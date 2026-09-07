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

// TestIndeterminateBothHalvesRequestsPotentialDependencies verifies that a
// double-INDETERMINATE result (both halves of a split crash independently)
// does not halt immediately: the search requests potential dependency
// injection from the service layer. When nothing can be injected (simulated
// here via HaltSearch, as the service does), the search halts and the current
// candidate set is preserved so the UI can reconstruct the two conflicting
// groups.
func TestIndeterminateBothHalvesRequestsPotentialDependencies(t *testing.T) {
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

	engine := NewEngine(initialStateFor(mods...))
	for steps := 0; !engine.GetCurrentState().NeedsPotentialDependencies; steps++ {
		if steps > 100 {
			t.Fatal("search never requested potential dependency injection")
		}
		plan, err := engine.PlanNextTest()
		if err != nil {
			t.Fatalf("PlanNextTest failed: %v", err)
		}
		if err := engine.SubmitTestResult(oracle(plan.ModIDsToTest())); err != nil {
			t.Fatalf("SubmitTestResult failed: %v", err)
		}
	}

	state := engine.GetCurrentState()
	if state.IsHalted {
		t.Fatal("expected the search to not halt before the service reacts")
	}
	if state.IsComplete {
		t.Fatal("expected the search to not be complete")
	}
	if state.IsHandlingIndeterminate {
		t.Fatal("expected the indeterminate handling to be cleared for the re-plan")
	}
	if len(state.ConflictSet) != 0 {
		t.Fatalf("expected empty conflict set, got %v", sets.MakeSlice(state.ConflictSet))
	}

	// Simulate the service finding no injectable dependencies: the search halts.
	engine.HaltSearch()

	state = engine.GetCurrentState()
	if !state.IsHalted {
		t.Fatalf("expected the search to be halted, but IsHalted=%t", state.IsHalted)
	}
	if state.IsComplete {
		t.Fatal("expected the halted search to not be marked complete")
	}
	if state.NeedsPotentialDependencies {
		t.Fatal("expected the potential dependency request to be cleared on halt")
	}

	// The two groups are reconstructable from the preserved candidate set.
	candidateSlice := sets.MakeSlice(state.GetCandidateSet())
	c1, c2 := sets.Split(candidateSlice)
	if !sets.Equal(sets.MakeSet(c1), sets.MakeSet([]string{"a", "b"})) ||
		!sets.Equal(sets.MakeSet(c2), sets.MakeSet([]string{"c", "d"})) {
		t.Fatalf("expected groups a,b and c,d, got %v and %v", c1, c2)
	}

	if _, err := engine.PlanNextTest(); err == nil {
		t.Fatal("expected PlanNextTest to fail on a halted search")
	}
}

// TestDoubleIndeterminateReplansSameSplitAfterInjection verifies that after
// the service has injected potential dependencies, the next planned test
// re-runs the same split (same stable set and candidates), so the split is
// retried with the referenced mods activated. With observability restored
// (simulated in the oracle), the search completes normally.
func TestDoubleIndeterminateReplansSameSplitAfterInjection(t *testing.T) {
	mods := []string{"a", "b", "c", "d"}

	injected := false
	oracle := func(s sets.Set) TestResult {
		_, hasA := s["a"]
		_, hasC := s["c"]
		if hasA != hasC && !injected {
			return TestResultIndeterminate
		}
		// After the injection, a's partner c is activated alongside, so the
		// test becomes observable. The primary conflict is c.
		if hasC {
			return TestResultFail
		}
		return TestResultGood
	}

	engine := NewEngine(initialStateFor(mods...))

	var firstSplitTest sets.Set
	for steps := 0; !engine.GetCurrentState().NeedsPotentialDependencies; steps++ {
		if steps > 100 {
			t.Fatal("search never requested potential dependency injection")
		}
		plan, err := engine.PlanNextTest()
		if err != nil {
			t.Fatalf("PlanNextTest failed: %v", err)
		}
		if plan.Kind == TestPlanNewBisection {
			// The initial test of the split; the injection retries the split
			// from its start, so this is the plan to compare against.
			firstSplitTest = plan.ModIDsToTest()
		}
		if err := engine.SubmitTestResult(oracle(plan.ModIDsToTest())); err != nil {
			t.Fatalf("SubmitTestResult failed: %v", err)
		}
	}

	// Simulate the service injecting the dependencies.
	injected = true
	engine.ClearNeedsPotentialDependencies()

	plan, err := engine.GetCurrentTestPlan()
	if err != nil {
		t.Fatalf("GetCurrentTestPlan failed after injection: %v", err)
	}
	if firstSplitTest == nil || !sets.Equal(plan.ModIDsToTest(), firstSplitTest) {
		t.Fatalf("expected the split's first test to be re-planned after injection (%v), got %v",
			sets.MakeSlice(firstSplitTest), sets.MakeSlice(plan.ModIDsToTest()))
	}

	// Drive the rest of the search to completion.
	for steps := 0; !engine.GetCurrentState().IsComplete; steps++ {
		if steps > 100 {
			t.Fatal("search did not complete after injection")
		}
		if engine.GetCurrentState().NeedsPotentialDependencies {
			t.Fatal("expected no further potential dependency requests")
		}
		next, err := engine.PlanNextTest()
		if err != nil {
			t.Fatalf("PlanNextTest failed: %v", err)
		}
		if err := engine.SubmitTestResult(oracle(next.ModIDsToTest())); err != nil {
			t.Fatalf("SubmitTestResult failed: %v", err)
		}
	}

	state := engine.GetCurrentState()
	if !sets.Equal(state.ConflictSet, sets.MakeSet([]string{"c"})) {
		t.Fatalf("expected conflict set {c}, got %v", sets.MakeSlice(state.ConflictSet))
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
