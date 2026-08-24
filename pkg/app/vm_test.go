package app

import (
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

func TestMakeExecutionLogVMCopiesDistinctStateSets(t *testing.T) {
	state := imcs.NewInitialState()
	state.Step = 3
	state.Round = 2
	state.Iteration = 4
	state.ConflictSet["conflict"] = struct{}{}
	state.StableSet["conflict"] = struct{}{}
	state.StableSet["stable"] = struct{}{}
	state.Candidates = []string{"candidate"}
	state.SearchStack = []imcs.SearchStep{{
		StableSet:  state.StableSet,
		Candidates: state.Candidates,
	}}

	entries := []imcs.CompletedTest{{
		Plan: imcs.TestPlan{
			Kind:        imcs.TestPlanNewBisection,
			StableSet:   state.ConflictSet,
			C1:          sets.OrderedSet{"candidate"},
			C2:          sets.OrderedSet{},
			ConflictSet: state.ConflictSet,
		},
		Result:          imcs.TestResultGood,
		StateBeforeTest: state,
	}}

	vm := makeExecutionLogVM(entries)
	if len(vm.Entries) != 1 {
		t.Fatalf("expected one history entry, got %d", len(vm.Entries))
	}
	entry := vm.Entries[0]
	if !sets.Equal(entry.StableSet, sets.MakeSet([]string{"conflict", "stable"})) {
		t.Fatalf("unexpected stable set: %v", sets.MakeSlice(entry.StableSet))
	}
	if !sets.Equal(entry.ClearedSet, sets.MakeSet([]string{"stable"})) {
		t.Fatalf("unexpected cleared set: %v", sets.MakeSlice(entry.ClearedSet))
	}
	if entry.Kind != imcs.TestPlanNewBisection {
		t.Fatalf("unexpected plan kind: %q", entry.Kind)
	}

	delete(state.StableSet, "stable")
	delete(state.ConflictSet, "conflict")
	if _, ok := entry.StableSet["stable"]; !ok {
		t.Fatal("stable set aliases the source state")
	}
	if _, ok := entry.ConflictSet["conflict"]; !ok {
		t.Fatal("conflict set aliases the source state")
	}
}
