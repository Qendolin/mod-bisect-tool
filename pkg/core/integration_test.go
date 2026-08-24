package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/bisect"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// runBisectionTest is a test harness that executes the full bisection process.
// maskingSet, when non-empty, models a secondary issue: a test whose effective
// set fully contains the masking set crashes before the primary issue can be
// observed, so the result is INDETERMINATE.
func runBisectionTest(t *testing.T, svc *bisect.Service, allMods map[string]*mods.Mod, problematicSets []sets.Set, maskingSet sets.Set) sets.Set {
	t.Helper()

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

		allProvidedIDsByEffectiveSet := sets.Copy(effectiveSet)
		for modID := range effectiveSet {
			if mod, ok := allMods[modID]; ok {
				for providedID := range mod.EffectiveProvides {
					allProvidedIDsByEffectiveSet[providedID] = struct{}{}
				}
			}
		}

		// A test fails if *any* of the problematic sets are fully satisfied.
		isFailure := false
		for _, pSet := range problematicSets {
			if len(pSet) > 0 && len(sets.Subtract(pSet, allProvidedIDsByEffectiveSet)) == 0 {
				isFailure = true
				break
			}
		}

		result := imcs.TestResultGood
		if isFailure {
			result = imcs.TestResultFail
		}
		// The masking set takes precedence: its presence hides the primary issue.
		if len(maskingSet) > 0 && len(sets.Subtract(maskingSet, allProvidedIDsByEffectiveSet)) == 0 {
			result = imcs.TestResultIndeterminate
		}

		t.Logf("Step %d: Testing %v -> Effective %v -> Result: %s", testCount+1, sets.MakeSlice(plan.ModIDsToTest()), sets.MakeSlice(effectiveSet), result)
		if err := svc.Engine().SubmitTestResult(result); err != nil {
			t.Fatalf("SubmitTestResult failed: %v", err)
		}
	}

	return svc.Engine().GetCurrentState().ConflictSet
}

// TestBisectService_Integration runs a full integration test of the bisect service.
func TestBisectService_Integration(t *testing.T) {
	logFile := setupLogger(t)
	defer logFile.Close()

	baseModSpecs := make(map[string]modSpec)
	for i := 0; i < 26; i++ {
		char := string(rune('a' + i))
		modID := fmt.Sprintf("mod_%s", char)
		filename := fmt.Sprintf("mod-%s-1.0.jar", char)
		baseModSpecs[filename] = modSpec{JSONContent: fmt.Sprintf(`{"id": "%s", "version": "1.0"}`, modID)}
	}

	testCases := []struct {
		name                string
		modSpecs            map[string]modSpec
		problematicSet      sets.Set
		maskingSet          sets.Set
		stateManagerSetup   func(state *mods.StateManager)
		expectedConflictSet sets.Set
	}{
		{
			name:                "1-Mod Independent Conflict",
			modSpecs:            baseModSpecs,
			problematicSet:      sets.MakeSet([]string{"mod_m"}),
			expectedConflictSet: sets.MakeSet([]string{"mod_m"}),
		},
		{
			name:                "2-Mod Independent Conflict",
			modSpecs:            baseModSpecs,
			problematicSet:      sets.MakeSet([]string{"mod_b", "mod_y"}),
			expectedConflictSet: sets.MakeSet([]string{"mod_b", "mod_y"}),
		},
		{
			name: "2-Mod Dependent Conflict",
			modSpecs: func() map[string]modSpec {
				specs := deepCopySpecs(baseModSpecs)
				specs["mod-c-1.0.jar"] = modSpec{JSONContent: `{"id": "mod_c", "version": "1.0", "depends": {"mod_j": ">=1.0"}}`}
				return specs
			}(),
			problematicSet:      sets.MakeSet([]string{"mod_j"}),
			expectedConflictSet: sets.MakeSet([]string{"mod_c"}),
		},
		{
			name: "Dependency Provider Conflict",
			modSpecs: func() map[string]modSpec {
				specs := deepCopySpecs(baseModSpecs)
				specs["mod-a-1.0.jar"] = modSpec{JSONContent: `{"id": "mod_a", "version": "1.0", "depends": {"api": "1.0"}}`}
				specs["mod-b-1.0.jar"] = modSpec{JSONContent: `{"id": "mod_b", "version": "1.0", "provides": ["api"]}`}
				return specs
			}(),
			problematicSet:      sets.MakeSet([]string{"mod_b"}),
			expectedConflictSet: sets.MakeSet([]string{"mod_a"}),
		},
		{
			name: "Nested JAR Conflict",
			modSpecs: func() map[string]modSpec {
				specs := deepCopySpecs(baseModSpecs)
				specs["mod-a-1.0.jar"] = modSpec{
					JSONContent: `{"id": "mod_a", "version": "1.0", "jars": [{"file": "libs/nested.jar"}]}`,
					NestedJars: map[string]modSpec{
						"libs/nested.jar": {JSONContent: `{"id": "nested_b", "version": "1.0"}`},
					},
				}
				return specs
			}(),
			problematicSet:      sets.MakeSet([]string{"nested_b"}),
			expectedConflictSet: sets.MakeSet([]string{"mod_a"}),
		},
		{
			name:                "No Conflict",
			modSpecs:            baseModSpecs,
			problematicSet:      sets.MakeSet([]string{}),
			expectedConflictSet: sets.MakeSet([]string{}),
		},
		{
			name: "Unresolvable Dependency Prevents Conflict",
			modSpecs: func() map[string]modSpec {
				specs := deepCopySpecs(baseModSpecs)
				specs["mod-x-1.0.jar"] = modSpec{JSONContent: `{"id": "mod_x", "version": "1.0", "depends": {"non_existent": "1.0"}}`}
				return specs
			}(),
			problematicSet:      sets.MakeSet([]string{"mod_x"}),
			expectedConflictSet: sets.MakeSet([]string{}),
		},
		{
			name:           "Forced Disabled Conflict Is Ignored",
			modSpecs:       baseModSpecs,
			problematicSet: sets.MakeSet([]string{"mod_c"}),
			stateManagerSetup: func(state *mods.StateManager) {
				state.SetForceDisabled("mod_c", true)
			},
			expectedConflictSet: sets.MakeSet([]string{}),
		},
		{
			name:                "Indeterminate Masking Mod Hides Primary Conflict",
			modSpecs:            baseModSpecs,
			problematicSet:      sets.MakeSet([]string{"mod_c", "mod_k"}),
			maskingSet:          sets.MakeSet([]string{"mod_m"}),
			expectedConflictSet: sets.MakeSet([]string{"mod_c", "mod_k"}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logging.Infof("Test: Running test case %q", t.Name())
			modsDir := filepath.Join(testDir, strings.ReplaceAll(tc.name, " ", "_"))
			if err := os.RemoveAll(modsDir); err != nil && !os.IsNotExist(err) {
				t.Fatalf("failed to clean mods dir: %v", err)
			}
			setupDummyMods(t, modsDir, tc.modSpecs)
			defer os.RemoveAll(modsDir)

			adapter := mods.FileAdapter{BaseDirectory: modsDir}
			loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderFabric}, Adapter: &adapter}
			allMods, providers, err := loader.LoadMods(modsDir, nil, nil)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}

			resolver := mods.NewDependencyResolver(allMods, providers, loader.RunLoader)
			stateMgr := mods.NewStateManager(allMods, resolver)
			activator := mods.NewModActivator(&adapter, allMods)

			if tc.stateManagerSetup != nil {
				tc.stateManagerSetup(stateMgr)
			}

			svc, err := bisect.NewService(stateMgr, activator)
			if err != nil {
				t.Fatalf("NewService failed: %v", err)
			}

			svc.ResetSearch()
			finalConflictSet := runBisectionTest(t, svc, allMods, []sets.Set{tc.problematicSet}, tc.maskingSet)

			if !reflect.DeepEqual(finalConflictSet, tc.expectedConflictSet) {
				t.Errorf("Test case '%s' failed.\nExpected: %v\nGot:      %v", tc.name, sets.MakeSlice(tc.expectedConflictSet), sets.MakeSlice(finalConflictSet))
			}
		})
	}
}

// TestBisectService_Enumeration verifies that the service can find multiple
// independent conflict sets by using the ContinueSearch workflow.
func TestBisectService_Enumeration(t *testing.T) {
	logFile := setupLogger(t)
	defer logFile.Close()

	// Setup: Create a base set of mods.
	baseModSpecs := make(map[string]modSpec)
	for i := 0; i < 10; i++ {
		char := string(rune('a' + i))
		modID := fmt.Sprintf("mod_%s", char)
		filename := fmt.Sprintf("mod-%s-1.0.jar", char)
		baseModSpecs[filename] = modSpec{JSONContent: fmt.Sprintf(`{"id": "%s", "version": "1.0"}`, modID)}
	}

	// Define two independent conflicts.
	problematicSets := []sets.Set{
		sets.MakeSet([]string{"mod_b", "mod_c"}),
		sets.MakeSet([]string{"mod_h"}),
	}

	// Initialize the environment.
	modsDir := filepath.Join(testDir, "enumeration_test")
	if err := os.RemoveAll(modsDir); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to clean mods dir: %v", err)
	}
	setupDummyMods(t, modsDir, baseModSpecs)
	defer os.RemoveAll(modsDir)

	adapter := mods.FileAdapter{BaseDirectory: modsDir}
	loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderFabric}, Adapter: &adapter}
	allMods, providers, err := loader.LoadMods(modsDir, nil, nil)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}

	// Initialize the service. NewService implicitly calls ResetSearch.
	resolver := mods.NewDependencyResolver(allMods, providers, loader.RunLoader)
	stateMgr := mods.NewStateManager(allMods, resolver)
	activator := mods.NewModActivator(&adapter, allMods)
	svc, err := bisect.NewService(stateMgr, activator)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// --- Round 1: Find the first conflict set ---
	t.Log("--- Starting Round 1 ---")
	foundSet1 := runBisectionTest(t, svc, allMods, problematicSets, nil)

	// Verify that one of the two conflict sets was found.
	if !reflect.DeepEqual(foundSet1, problematicSets[0]) && !reflect.DeepEqual(foundSet1, problematicSets[1]) {
		t.Fatalf("Round 1 found an incorrect conflict set. Expected one of %v, but got %v", problematicSets, sets.MakeSlice(foundSet1))
	}
	t.Logf("Round 1 successful. Found conflict set: %v", sets.MakeSlice(foundSet1))

	// --- Continue Search: Prepare for Round 2 ---
	if !svc.Engine().GetCurrentState().IsComplete {
		t.Fatal("Engine should be in a completed state before continuing.")
	}
	t.Log("--- Continuing Search for Round 2 ---")
	svc.ContinueSearch()

	// --- Round 2: Find the second conflict set ---
	t.Log("--- Starting Round 2 ---")
	foundSet2 := runBisectionTest(t, svc, allMods, problematicSets, nil)

	// Verify that the *other* conflict set was found.
	var expectedSet2 sets.Set
	if reflect.DeepEqual(foundSet1, problematicSets[0]) {
		expectedSet2 = problematicSets[1]
	} else {
		expectedSet2 = problematicSets[0]
	}

	if !reflect.DeepEqual(foundSet2, expectedSet2) {
		t.Fatalf("Round 2 found an incorrect conflict set. Expected %v, but got %v", sets.MakeSlice(expectedSet2), sets.MakeSlice(foundSet2))
	}
	t.Logf("Round 2 successful. Found conflict set: %v", sets.MakeSlice(foundSet2))

	// --- Final Verification ---
	// After finding the second set, the service should contain *both* archived sets.
	// Note: The second set is in the engine's current state, but not yet archived in enumState.
	// To get the full picture, we add the final found set to the archived list.
	allFound := copySets(svc.EnumerationState().FoundConflictSets)
	allFound = append(allFound, foundSet2)

	if len(allFound) != 2 {
		t.Fatalf("Expected to find 2 total conflict sets, but service reported %d", len(allFound))
	}

	// Sort slices for deterministic comparison.
	sort.Slice(allFound, func(i, j int) bool {
		return sets.MakeSlice(allFound[i])[0] < sets.MakeSlice(allFound[j])[0]
	})
	sort.Slice(problematicSets, func(i, j int) bool {
		return sets.MakeSlice(problematicSets[i])[0] < sets.MakeSlice(problematicSets[j])[0]
	})

	if !reflect.DeepEqual(allFound, problematicSets) {
		t.Errorf("Final enumeration result is incorrect.\nExpected: %v\nGot:      %v", problematicSets, allFound)
	}

	t.Log("Enumeration test successful.")
}
