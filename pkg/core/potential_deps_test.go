package app_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/bisect"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// --- Minimal class file fixtures (binary) ---

func fixtureClassFile(thisClassIndex, cpCount uint16, cpEntries ...[]byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xCA\xFE\xBA\xBE") // magic
	buf.WriteString("\x00\x00")         // minor_version
	buf.WriteString("\x00\x34")         // major_version (52)
	buf.WriteByte(byte(cpCount >> 8))   // constant_pool_count
	buf.WriteByte(byte(cpCount))
	for _, entry := range cpEntries {
		buf.Write(entry)
	}
	buf.WriteString("\x00\x21")              // access_flags
	buf.WriteByte(byte(thisClassIndex >> 8)) // this_class
	buf.WriteByte(byte(thisClassIndex))
	buf.WriteString("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00") // super, counts
	return []byte(buf.String())
}

func fixtureUtf8(s string) []byte {
	out := []byte{1, byte(len(s) >> 8), byte(len(s))}
	return append(out, s...)
}

func fixtureClass(nameIndex uint16) []byte {
	return []byte{7, byte(nameIndex >> 8), byte(nameIndex)}
}

// fixtureClassDeclaring returns class file bytes for a class that only declares itself.
func fixtureClassDeclaring(internalName string) string {
	return string(fixtureClassFile(2, 3, fixtureUtf8(internalName), fixtureClass(1)))
}

// fixtureClassReferencing returns class file bytes for a class that declares
// itself and references other internal class names via CONSTANT_Class entries.
func fixtureClassReferencing(internalName string, refs ...string) string {
	utf8Count := 1 + len(refs)
	thisClass := uint16(2 * utf8Count)
	entries := [][]byte{fixtureUtf8(internalName)}
	for _, ref := range refs {
		entries = append(entries, fixtureUtf8(ref))
	}
	for i := 2; i <= utf8Count; i++ {
		entries = append(entries, fixtureClass(uint16(i)))
	}
	entries = append(entries, fixtureClass(1))
	return string(fixtureClassFile(thisClass, uint16(len(entries)+1), entries...))
}

// --- Harness ---

// undeclaredPair models a mod that silently relies on another mod: a test
// whose effective set contains source but not target crashes before the
// primary issue can be observed, so the result is INDETERMINATE.
type undeclaredPair struct {
	source string
	target string
}

// runBisectionWithUndeclaredDeps drives the full bisection through the
// service layer. When double-INDETERMINATE halts the search, it attempts to
// infer undeclared dependencies, applies them if found, and retries. It returns
// the final conflict set and every injection performed.
func runBisectionWithUndeclaredDeps(t *testing.T, svc *bisect.Service, allMods map[string]*mods.Mod, problematicSets []sets.Set, undeclared []undeclaredPair) (sets.Set, [][]mods.InferredDependency) {
	t.Helper()
	var injections [][]mods.InferredDependency

	for testCount := 0; !svc.Engine().GetCurrentState().IsComplete; testCount++ {
		if testCount > 100 {
			t.Fatalf("Exceeded test count limit (100)")
		}
		if svc.Engine().GetCurrentState().IsHalted {
			t.Logf("Search halted before completion.")
			break
		}

		plan, err := svc.Engine().PlanNextTest()
		if err != nil {
			t.Logf("Planning finished: %v", err)
			break
		}

		effectiveSet := svc.StateManager().ResolveEffectiveSet(plan.ModIDsToTest()).EffectiveSet

		provided := sets.Copy(effectiveSet)
		for modID := range effectiveSet {
			if mod, ok := allMods[modID]; ok {
				for providedID := range mod.EffectiveProvides {
					provided[providedID] = struct{}{}
				}
			}
		}

		result := imcs.TestResultGood
		for _, pSet := range problematicSets {
			if len(pSet) > 0 && len(sets.Subtract(pSet, provided)) == 0 {
				result = imcs.TestResultFail
				break
			}
		}
		// Undeclared dependencies mask the primary issue.
		for _, pair := range undeclared {
			if _, hasSource := provided[pair.source]; hasSource {
				if _, hasTarget := provided[pair.target]; !hasTarget {
					result = imcs.TestResultIndeterminate
					break
				}
			}
		}

		t.Logf("Step %d: Testing %v -> Effective %v -> Result: %s", testCount+1,
			sets.MakeSlice(plan.ModIDsToTest()), sets.MakeSlice(effectiveSet), result)
		svc.SubmitTestResult(result)

		if svc.Engine().GetCurrentState().IsHalted && svc.CanInferDependencies() {
			inferred := svc.InferDependencies()
			if len(inferred) > 0 {
				injected := svc.ApplyInferredDependencies(inferred)
				t.Logf("Step %d injected %d inferred dependencies", testCount+1, len(injected))
				injections = append(injections, injected)
			} else {
				svc.DismissInferredDependencies()
			}
		}
	}

	return svc.Engine().GetCurrentState().ConflictSet, injections
}

// newDepsTestService builds an 8-mod environment (mod_a..mod_h) with the given
// per-mod decorations applied on top of plain fabric mods.
func newDepsTestService(t *testing.T, decorate func(filename string, spec modSpec) modSpec) (*bisect.Service, map[string]*mods.Mod) {
	t.Helper()

	specs := make(map[string]modSpec)
	for i := 0; i < 8; i++ {
		char := string(rune('a' + i))
		modID := "mod_" + char
		filename := "mod-" + char + "-1.0.jar"
		spec := modSpec{JSONContent: `{"id": "` + modID + `", "version": "1.0"}`}
		if decorate != nil {
			spec = decorate(filename, spec)
		}
		specs[filename] = spec
	}

	modsDir := filepath.Join(testDir, strings.ReplaceAll(t.Name(), " ", "_"))
	if err := os.RemoveAll(modsDir); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to clean mods dir: %v", err)
	}
	setupDummyMods(t, modsDir, specs)
	t.Cleanup(func() { os.RemoveAll(modsDir) })

	adapter := mods.FileAdapter{BaseDirectory: modsDir}
	loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderFabric}, Adapter: &adapter}
	allMods, providers, err := loader.LoadMods(modsDir, nil, nil)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}

	resolver := mods.NewDependencyResolver(allMods, providers, loader.RunLoader)
	stateMgr := mods.NewStateManager(allMods, resolver)
	activator := mods.NewModActivator(&adapter, allMods)
	svc, err := bisect.NewService(stateMgr, activator)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	svc.ResetSearch()
	return svc, allMods
}

// --- Tests ---

// TestDoubleIndeterminateResolvedByAssumedDependencies verifies that a
// double-INDETERMINATE caused by two undeclared, class-discoverable
// dependencies (mod_a -> mod_e, mod_f -> mod_d) is resolved by injecting the
// inferred dependencies: the search continues and finds the real conflict.
func TestDoubleIndeterminateResolvedByAssumedDependencies(t *testing.T) {
	logFile := setupLogger(t)
	defer logFile.Close()

	svc, allMods := newDepsTestService(t, func(filename string, spec modSpec) modSpec {
		switch filename {
		case "mod-a-1.0.jar":
			spec.RawFiles = map[string]string{"com/a/Foo.class": fixtureClassReferencing("com/a/Foo", "com/e/Baz")}
		case "mod-e-1.0.jar":
			spec.RawFiles = map[string]string{"com/e/Baz.class": fixtureClassDeclaring("com/e/Baz")}
		case "mod-f-1.0.jar":
			spec.RawFiles = map[string]string{"com/f/Qux.class": fixtureClassReferencing("com/f/Qux", "com/d/Waldo")}
		case "mod-d-1.0.jar":
			spec.RawFiles = map[string]string{"com/d/Waldo.class": fixtureClassDeclaring("com/d/Waldo")}
		}
		return spec
	})

	problematic := sets.MakeSet([]string{"mod_c", "mod_g"})
	undeclared := []undeclaredPair{
		{source: "mod_a", target: "mod_e"},
		{source: "mod_f", target: "mod_d"},
	}

	conflict, injections := runBisectionWithUndeclaredDeps(t, svc, allMods, []sets.Set{problematic}, undeclared)

	if svc.Engine().GetCurrentState().IsHalted {
		t.Fatalf("expected the search to resolve the double-INDETERMINATE, but it halted")
	}
	if !reflect.DeepEqual(conflict, problematic) {
		t.Fatalf("expected conflict set %v, got %v", sets.MakeSlice(problematic), sets.MakeSlice(conflict))
	}
	if len(injections) != 1 || len(injections[0]) != 2 {
		t.Fatalf("expected exactly one injection of two dependencies, got %+v", injections)
	}
	if svc.CanInferDependencies() {
		t.Fatal("expected CanInferDependencies to be false after inferred dependencies applied")
	}

	if allMods["mod_a"].Metadata.Depends["mod_e"] == nil {
		t.Error("expected mod_a to depend on mod_e after injection")
	}
	if allMods["mod_f"].Metadata.Depends["mod_d"] == nil {
		t.Error("expected mod_f to depend on mod_d after injection")
	}
}

// TestDoubleIndeterminateWithoutInferableDepsHalts verifies that a
// double-INDETERMINATE whose undeclared dependencies are invisible to the
// bytecode analysis (no class files) halts the search.
func TestDoubleIndeterminateWithoutInferableDepsHalts(t *testing.T) {
	logFile := setupLogger(t)
	defer logFile.Close()

	svc, allMods := newDepsTestService(t, nil)

	problematic := sets.MakeSet([]string{"mod_c", "mod_g"})
	undeclared := []undeclaredPair{
		{source: "mod_b", target: "mod_h"},
		{source: "mod_g", target: "mod_c"},
	}

	conflict, injections := runBisectionWithUndeclaredDeps(t, svc, allMods, []sets.Set{problematic}, undeclared)

	if !svc.Engine().GetCurrentState().IsHalted {
		t.Fatalf("expected the search to halt, got conflict set %v", sets.MakeSlice(conflict))
	}
	if len(conflict) != 0 {
		t.Fatalf("expected empty conflict set on halt, got %v", sets.MakeSlice(conflict))
	}
	if len(injections) != 0 {
		t.Fatalf("expected no injections, got %+v", injections)
	}
	if allMods["mod_b"].Metadata.Depends["mod_h"] != nil {
		t.Error("expected no dependency to be injected for mod_b")
	}
}

// TestSecondDoubleIndeterminateHaltsAfterInjection verifies the one-shot
// nature of the potential dependency injection: the first double-INDETERMINATE
// is resolved by injecting the discoverable dependencies (mod_a -> mod_e,
// mod_f -> mod_d), but the second double-INDETERMINATE caused by remaining
// undiscoverable dependencies (mod_b -> mod_h, mod_g -> mod_c) halts the search.
func TestSecondDoubleIndeterminateHaltsAfterInjection(t *testing.T) {
	logFile := setupLogger(t)
	defer logFile.Close()

	svc, allMods := newDepsTestService(t, func(filename string, spec modSpec) modSpec {
		switch filename {
		case "mod-a-1.0.jar":
			spec.RawFiles = map[string]string{"com/a/Foo.class": fixtureClassReferencing("com/a/Foo", "com/e/Baz")}
		case "mod-e-1.0.jar":
			spec.RawFiles = map[string]string{"com/e/Baz.class": fixtureClassDeclaring("com/e/Baz")}
		case "mod-f-1.0.jar":
			spec.RawFiles = map[string]string{"com/f/Qux.class": fixtureClassReferencing("com/f/Qux", "com/d/Waldo")}
		case "mod-d-1.0.jar":
			spec.RawFiles = map[string]string{"com/d/Waldo.class": fixtureClassDeclaring("com/d/Waldo")}
		case "mod-b-1.0.jar":
			// Declares its class but its undeclared dependency on mod_h is
			// not visible via CONSTANT_Class entries.
			spec.RawFiles = map[string]string{"com/b/Bar.class": fixtureClassDeclaring("com/b/Bar")}
		case "mod-g-1.0.jar":
			spec.RawFiles = map[string]string{"com/g/Gee.class": fixtureClassDeclaring("com/g/Gee")}
		}
		return spec
	})

	problematic := sets.MakeSet([]string{"mod_c", "mod_g"})
	undeclared := []undeclaredPair{
		{source: "mod_a", target: "mod_e"},
		{source: "mod_f", target: "mod_d"},
		{source: "mod_b", target: "mod_h"},
		{source: "mod_g", target: "mod_c"},
	}

	conflict, injections := runBisectionWithUndeclaredDeps(t, svc, allMods, []sets.Set{problematic}, undeclared)

	if !svc.Engine().GetCurrentState().IsHalted {
		t.Fatalf("expected the search to halt on the second double-INDETERMINATE, got conflict set %v", sets.MakeSlice(conflict))
	}
	if len(injections) != 1 || len(injections[0]) != 2 {
		t.Fatalf("expected exactly one injection of two dependencies, got %+v", injections)
	}
	if allMods["mod_a"].Metadata.Depends["mod_e"] == nil {
		t.Error("expected mod_a to depend on mod_e after injection")
	}
	if allMods["mod_b"].Metadata.Depends["mod_h"] != nil {
		t.Error("expected no dependency to be injected for the undiscoverable mod_b -> mod_h")
	}
}
