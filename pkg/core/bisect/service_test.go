package bisect

import (
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

func TestResetSearchResetsSessionState(t *testing.T) {
	allMods := map[string]*mods.Mod{"a": {Metadata: mods.ModMetadata{ID: "a"}}}
	stateMgr := mods.NewStateManager(allMods, nil)
	service := &Service{
		state:  stateMgr,
		engine: imcs.NewEngine(imcs.NewInitialState()),
	}
	service.inferredDepsAttempted = true
	service.lastReconcileRevision = 99
	service.state.SetProblematicBatch([]string{"a"}, true)
	service.state.SetUnresolvableBatch([]string{"a"}, true)
	service.ResetSearch()

	if service.inferredDepsAttempted {
		t.Fatal("ResetSearch should clear the per-session inferred dependency flag")
	}
	if service.NeedsReconciliation() {
		t.Fatal("ResetSearch should reset lastReconcileRevision so reconciliation is not stale")
	}
	if service.lastReconcileRevision != service.state.StateRevision() {
		t.Fatalf("lastReconcileRevision = %d, want %d", service.lastReconcileRevision, service.state.StateRevision())
	}
}

func TestServiceInferredDependenciesHaltAndDismissalFlow(t *testing.T) {
	allMods := map[string]*mods.Mod{
		"a": {Metadata: mods.ModMetadata{ID: "a"}},
		"b": {Metadata: mods.ModMetadata{ID: "b"}},
	}
	stateMgr := mods.NewStateManager(allMods, nil)
	engine := imcs.NewEngine(imcs.NewInitialState())
	engine.AddCandidates(sets.NewSet("a", "b"))

	service := &Service{
		state:  stateMgr,
		engine: engine,
	}

	// 1. First half of the split (mod 'a')
	_, err := engine.PlanNextTest()
	if err != nil {
		t.Fatalf("PlanNextTest failed: %v", err)
	}
	service.SubmitTestResult(imcs.TestResultIndeterminate)

	if service.CanInferDependencies() {
		t.Fatal("expected CanInferDependencies to be false after only one half is indeterminate")
	}

	// 2. Second half of the split (complement: mod 'b')
	_, err = engine.PlanNextTest()
	if err != nil {
		t.Fatalf("PlanNextTest complement failed: %v", err)
	}
	// Submitting the second indeterminate halts the engine
	service.SubmitTestResult(imcs.TestResultIndeterminate)

	if !service.Engine().GetCurrentState().IsHalted {
		t.Fatal("expected engine to be halted after double-indeterminate")
	}
	if !service.CanInferDependencies() {
		t.Fatal("expected CanInferDependencies to be true after double-indeterminate")
	}

	// 3. User cancels / dismisses the inferred dependencies
	service.DismissInferredDependencies()

	if service.CanInferDependencies() {
		t.Fatal("expected CanInferDependencies to be false after dismissal")
	}
	if !service.Engine().GetCurrentState().IsHalted {
		t.Fatal("expected engine to remain halted after dismissal")
	}
}

func TestServiceApplyInferredDependenciesUnчнойHaltsSplit(t *testing.T) {
	allMods := map[string]*mods.Mod{
		"a": {Metadata: mods.ModMetadata{ID: "a"}},
		"b": {Metadata: mods.ModMetadata{ID: "b"}},
	}
	stateMgr := mods.NewStateManager(allMods, nil)
	engine := imcs.NewEngine(imcs.NewInitialState())
	engine.AddCandidates(sets.NewSet("a", "b"))

	service := &Service{
		state:  stateMgr,
		engine: engine,
	}

	// Drive to double-indeterminate halt
	_, _ = engine.PlanNextTest()
	service.SubmitTestResult(imcs.TestResultIndeterminate)
	_, _ = engine.PlanNextTest()
	service.SubmitTestResult(imcs.TestResultIndeterminate)

	deps := []mods.InferredDependency{
		{SourceID: "a", TargetID: "b", Classes: []string{"com/b/B"}},
	}
	injected := service.ApplyInferredDependencies(deps)

	if len(injected) != 1 {
		t.Fatalf("expected 1 injected dependency, got %d", len(injected))
	}
	if service.Engine().GetCurrentState().IsHalted {
		t.Fatal("expected engine to be un-halted after ApplyInferredDependencies")
	}
	if service.CanInferDependencies() {
		t.Fatal("expected CanInferDependencies to be false after applying")
	}
	if allMods["a"].Metadata.Depends["b"] == nil {
		t.Fatal("expected mod a to have dependency on mod b injected into metadata")
	}
}

func TestInferDependenciesScopedToHaltedSplit(t *testing.T) {
	allMods := map[string]*mods.Mod{
		"a": {
			Metadata: mods.ModMetadata{ID: "a"},
			ClassIndex: &mods.JarClassIndex{
				Declared:   map[string]struct{}{"com/a/A": {}},
				Referenced: map[string]struct{}{"com/b/B": {}}, // A -> B (crosses the split)
			},
		},
		"b": {
			Metadata: mods.ModMetadata{ID: "b"},
			ClassIndex: &mods.JarClassIndex{
				Declared:   map[string]struct{}{"com/b/B": {}},
				Referenced: map[string]struct{}{},
			},
		},
		"x": {
			Metadata: mods.ModMetadata{ID: "x"},
			ClassIndex: &mods.JarClassIndex{
				Declared:   map[string]struct{}{"com/x/X": {}},
				Referenced: map[string]struct{}{"com/y/Y": {}}, // X -> Y (unrelated background mod)
			},
		},
		"y": {
			Metadata: mods.ModMetadata{ID: "y"},
			ClassIndex: &mods.JarClassIndex{
				Declared:   map[string]struct{}{"com/y/Y": {}},
				Referenced: map[string]struct{}{},
			},
		},
	}
	stateMgr := mods.NewStateManager(allMods, nil)
	engine := imcs.NewEngine(imcs.NewInitialState())
	engine.AddCandidates(sets.NewSet("a", "b"))

	service := &Service{
		state:  stateMgr,
		engine: engine,
	}

	// 1. Unhalted (startup flag mode): should find both A -> B and X -> Y
	unhaltedDeps := service.InferDependencies()
	if len(unhaltedDeps) != 2 {
		t.Fatalf("expected 2 unhalted dependencies, got %d: %+v", len(unhaltedDeps), unhaltedDeps)
	}

	// 2. Drive engine to double-indeterminate halt on candidates {a, b}
	_, _ = engine.PlanNextTest()
	service.SubmitTestResult(imcs.TestResultIndeterminate)
	_, _ = engine.PlanNextTest()
	service.SubmitTestResult(imcs.TestResultIndeterminate)

	if !service.Engine().GetCurrentState().IsHalted {
		t.Fatal("expected engine to be halted")
	}

	// 3. Halted: must strictly scope to dependencies crossing split {a} <-> {b}
	haltedDeps := service.InferDependencies()
	if len(haltedDeps) != 1 {
		t.Fatalf("expected exactly 1 split-scoped dependency, got %d: %+v", len(haltedDeps), haltedDeps)
	}
	if haltedDeps[0].SourceID != "a" || haltedDeps[0].TargetID != "b" {
		t.Fatalf("expected dependency a -> b, got %+v", haltedDeps[0])
	}
}
