package imcs

import (
	"math"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// oracleFunc decides the outcome of a test on a set of mod ids.
type oracleFunc func(sets.Set) TestResult

// runSearchToCompletion drives the engine until the search completes or halts,
// using the given oracle for every planned test.
func runSearchToCompletion(t *testing.T, initialState SearchState, oracle oracleFunc) SearchState {
	t.Helper()
	engine := NewEngine(initialState)
	for steps := 0; !engine.GetCurrentState().IsComplete && !engine.GetCurrentState().IsHalted; steps++ {
		if steps > 500 {
			t.Fatalf("search did not terminate within 500 steps")
		}
		plan, err := engine.PlanNextTest()
		if err != nil {
			t.Fatalf("PlanNextTest failed: %v", err)
		}
		result := oracle(plan.ModIDsToTest())
		if err := engine.SubmitTestResult(result); err != nil {
			t.Fatalf("SubmitTestResult failed: %v", err)
		}
	}
	return engine.GetCurrentState()
}

func initialStateFor(mods ...string) SearchState {
	state := NewInitialState()
	state.AllModIDs = mods
	state.Candidates = mods
	return state
}

func TestTestPlanKindsDeriveExpectedTestSets(t *testing.T) {
	stable := sets.MakeSet([]string{"stable"})
	c1 := sets.OrderedSet{"a", "b"}
	c2 := sets.OrderedSet{"c", "d"}
	conflict := sets.MakeSet([]string{"conflict"})

	tests := []struct {
		name string
		plan TestPlan
		want sets.Set
	}{
		{
			name: "complement",
			plan: TestPlan{Kind: TestPlanComplement, StableSet: stable, C1: c1, C2: c2, ConflictSet: conflict},
			want: sets.MakeSet([]string{"stable", "c", "d"}),
		},
		{
			name: "continuation",
			plan: TestPlan{Kind: TestPlanContinuation, StableSet: stable, C1: c1, C2: c2, ConflictSet: conflict},
			want: sets.MakeSet([]string{"stable", "a", "b"}),
		},
		{
			name: "new bisection",
			plan: TestPlan{Kind: TestPlanNewBisection, StableSet: stable, C1: c1, C2: c2, ConflictSet: conflict},
			want: sets.MakeSet([]string{"stable", "a", "b"}),
		},
		{
			name: "verification",
			plan: TestPlan{Kind: TestPlanVerification, StableSet: stable, C1: c1, C2: c2, ConflictSet: conflict},
			want: sets.MakeSet([]string{"conflict"}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.ModIDsToTest(); !sets.Equal(got, tc.want) {
				t.Fatalf("expected test set %v, got %v", sets.MakeSlice(tc.want), sets.MakeSlice(got))
			}
			if got := tc.plan.IsVerificationStep(); got != (tc.plan.Kind == TestPlanVerification) {
				t.Fatalf("unexpected verification classification: %t", got)
			}
		})
	}
}

// TestIndeterminateMaskingModExcluded verifies that a masking mod which always
// yields INDETERMINATE does not enter the conflict set, while the primary
// conflict is still found. This exercises the complement GOOD and FAIL branches.
func TestIndeterminateMaskingModExcluded(t *testing.T) {
	mods := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	primary := sets.MakeSet([]string{"c", "e"})
	mask := "a"

	oracle := func(s sets.Set) TestResult {
		if _, ok := s[mask]; ok {
			return TestResultIndeterminate
		}
		if len(sets.Subtract(primary, s)) == 0 {
			return TestResultFail
		}
		return TestResultGood
	}

	state := runSearchToCompletion(t, initialStateFor(mods...), oracle)

	if !sets.Equal(state.ConflictSet, primary) {
		t.Fatalf("expected conflict set %v, got %v", sets.MakeSlice(primary), sets.MakeSlice(state.ConflictSet))
	}
	if _, ok := state.ConflictSet[mask]; ok {
		t.Fatalf("masking mod %q must not be part of the conflict set", mask)
	}
}

// TestComplementDescentReplacesStackFrame verifies that a complement descent is
// a tail call: the frame that produced the INDETERMINATE result is replaced on
// the stack instead of remaining beneath the new frame.
func TestComplementDescentReplacesStackFrame(t *testing.T) {
	mods := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	primary := sets.MakeSet([]string{"c", "e"})
	mask := "a"

	oracle := func(s sets.Set) TestResult {
		if _, ok := s[mask]; ok {
			return TestResultIndeterminate
		}
		if len(sets.Subtract(primary, s)) == 0 {
			return TestResultFail
		}
		return TestResultGood
	}

	engine := NewEngine(initialStateFor(mods...))

	step := func() {
		t.Helper()
		plan, err := engine.PlanNextTest()
		if err != nil {
			t.Fatalf("PlanNextTest failed: %v", err)
		}
		if err := engine.SubmitTestResult(oracle(plan.ModIDsToTest())); err != nil {
			t.Fatalf("SubmitTestResult failed: %v", err)
		}
	}

	// First split is INDETERMINATE; the complement is GOOD, so the search
	// descends into the first half on a single stack frame.
	step() // INDETERMINATE
	step() // complement GOOD -> descend into first half
	if depth := len(engine.GetCurrentState().SearchStack); depth != 1 {
		t.Fatalf("expected stack depth 1 after complement GOOD descent, got %d", depth)
	}

	// The descent is INDETERMINATE again; the complement is FAIL, so the search
	// descends into the second half. The stack must not grow: the frame that
	// produced the second INDETERMINATE is replaced, not kept.
	step() // INDETERMINATE
	step() // complement FAIL -> descend into second half
	if depth := len(engine.GetCurrentState().SearchStack); depth != 1 {
		t.Fatalf("expected stack depth 1 after complement FAIL descent, got %d", depth)
	}
}

// TestIndeterminateComplementFail verifies the branch where the complement test
// on the second half returns FAIL, descending into the second half.
func TestIndeterminateComplementFail(t *testing.T) {
	mods := []string{"a", "b", "c", "d"}
	primary := sets.MakeSet([]string{"c"})

	oracle := func(s sets.Set) TestResult {
		_, hasA := s["a"]
		_, hasC := s["c"]
		if hasA && !hasC {
			// a crashes when c (its partner) is absent.
			return TestResultIndeterminate
		}
		if len(sets.Subtract(primary, s)) == 0 {
			return TestResultFail
		}
		return TestResultGood
	}

	state := runSearchToCompletion(t, initialStateFor(mods...), oracle)

	if !sets.Equal(state.ConflictSet, primary) {
		t.Fatalf("expected conflict set %v, got %v", sets.MakeSlice(primary), sets.MakeSlice(state.ConflictSet))
	}
}

// TestIndeterminateBothHalves verifies that a double-INDETERMINATE result (both
// halves of a split crash independently) halts the search instead of continuing.
// The current candidate set is preserved so the UI can reconstruct the two
// conflicting groups.
func TestIndeterminateBothHalvesHalts(t *testing.T) {
	mods := []string{"a", "b", "c", "d"}

	oracle := func(s sets.Set) TestResult {
		_, hasA := s["a"]
		_, hasC := s["c"]
		if hasA != hasC {
			// Exactly one of a and c is present, so the missing partner causes a crash.
			return TestResultIndeterminate
		}
		return TestResultGood
	}

	state := runSearchToCompletion(t, initialStateFor(mods...), oracle)

	if !state.IsHalted {
		t.Fatalf("expected the search to be halted, but IsHalted=%t", state.IsHalted)
	}
	if state.IsComplete {
		t.Fatal("expected the halted search to not be marked complete")
	}
	if len(state.ConflictSet) != 0 {
		t.Fatalf("expected empty conflict set on halt, got %v", sets.MakeSlice(state.ConflictSet))
	}

	// The two groups are reconstructable from the preserved candidate set.
	candidateSlice := sets.MakeSlice(state.GetCandidateSet())
	c1, c2 := sets.Split(candidateSlice)
	if !sets.Equal(sets.MakeSet(c1), sets.MakeSet([]string{"a", "b"})) ||
		!sets.Equal(sets.MakeSet(c2), sets.MakeSet([]string{"c", "d"})) {
		t.Fatalf("expected groups a,b and c,d, got %v and %v", c1, c2)
	}
}

// TestIndeterminateSingleElementIsNonElement verifies that a single candidate
// whose test is still INDETERMINATE is treated as a non-element, exhausting the
// search with an empty conflict set.
func TestIndeterminateSingleElementIsNonElement(t *testing.T) {
	mods := []string{"a", "b"}
	mask := "a"

	oracle := func(s sets.Set) TestResult {
		if _, ok := s[mask]; ok {
			return TestResultIndeterminate
		}
		return TestResultGood
	}

	state := runSearchToCompletion(t, initialStateFor(mods...), oracle)

	if len(state.ConflictSet) != 0 {
		t.Fatalf("expected empty conflict set, got %v", sets.MakeSlice(state.ConflictSet))
	}
}

// TestIndeterminateNoConflictTerminates verifies that a system with no primary
// conflict and no indeterminate results terminates with an empty conflict set.
func TestIndeterminateNoConflictTerminates(t *testing.T) {
	mods := []string{"a", "b", "c", "d"}

	oracle := func(sets.Set) TestResult {
		return TestResultGood
	}

	state := runSearchToCompletion(t, initialStateFor(mods...), oracle)

	if len(state.ConflictSet) != 0 {
		t.Fatalf("expected empty conflict set, got %v", sets.MakeSlice(state.ConflictSet))
	}
}

// TestIndeterminateCountAndEstimate verifies that each INDETERMINATE result is
// counted and that GetEstimatedMaxTests accounts for the extra complement tests.
func TestIndeterminateCountAndEstimate(t *testing.T) {
	mods := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	primary := sets.MakeSet([]string{"c", "e"})
	mask := "a"

	oracle := func(s sets.Set) TestResult {
		if _, ok := s[mask]; ok {
			return TestResultIndeterminate
		}
		if len(sets.Subtract(primary, s)) == 0 {
			return TestResultFail
		}
		return TestResultGood
	}

	engine := NewEngine(initialStateFor(mods...))

	indeterminateSeen := 0
	maxEstimate := 0
	for !engine.GetCurrentState().IsComplete {
		plan, err := engine.PlanNextTest()
		if err != nil {
			t.Fatalf("PlanNextTest failed: %v", err)
		}
		result := oracle(plan.ModIDsToTest())
		if result == TestResultIndeterminate {
			indeterminateSeen++
		}
		if err := engine.SubmitTestResult(result); err != nil {
			t.Fatalf("SubmitTestResult failed: %v", err)
		}
		if est := engine.GetEstimatedMaxTests(); est > maxEstimate {
			maxEstimate = est
		}
	}

	state := engine.GetCurrentState()
	if indeterminateSeen == 0 {
		t.Fatal("expected at least one INDETERMINATE result in this scenario")
	}
	if state.IndeterminateCount != indeterminateSeen {
		t.Fatalf("expected IndeterminateCount %d, got %d", indeterminateSeen, state.IndeterminateCount)
	}

	// The base estimate is problemsFound * (ceil(log2(n)) + 1). Without any
	// indeterminate results the estimate would be exactly that, so the observed
	// estimate must exceed it by at least the number of indeterminates.
	problemsFound := len(state.ConflictSet)
	if !state.IsComplete {
		problemsFound++
	}
	if state.IndeterminateCount > 0 {
		base := problemsFound * (int(math.Ceil(math.Log2(float64(len(mods))))) + 1)
		if maxEstimate < base+state.IndeterminateCount {
			t.Fatalf("expected estimate >= %d, got %d", base+state.IndeterminateCount, maxEstimate)
		}
	}
}
