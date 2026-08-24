// imcs_algorithm.go
package imcs

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// IMCSAlgorithm contains the pure, stateless logic for the bisection search.
type IMCSAlgorithm struct{}

// NewIMCSAlgorithm creates a new algorithm instance.
func NewIMCSAlgorithm() *IMCSAlgorithm {
	return &IMCSAlgorithm{}
}

// indeterminateContext returns the stable set and candidates of the step that
// produced the complement test currently being handled. Candidates are sorted
// deterministically so planning and applying agree on the split.
func indeterminateContext(state SearchState) (sets.Set, []string) {
	if len(state.SearchStack) > 0 {
		step := state.SearchStack[len(state.SearchStack)-1]
		return step.StableSet, step.Candidates
	}
	reconstructed := newSearchStep(state.ConflictSet, state.Candidates)
	return reconstructed.StableSet, reconstructed.Candidates
}

// exhaustSearch handles a FindNextConflictElement branch that terminated with no
// element found. A null result propagates out of every FAIL-descend ancestor, so
// the entire search stack is cleared and the search is marked complete.
func exhaustSearch(newState *SearchState) {
	newState.SearchStack = make([]SearchStep, 0)
	logging.Info("IMCSAlgorithm: Bisection finished, no conflict found. Search is complete.")
	newState.IsComplete = true
}

// recordFoundElement commits a found conflict element to the global state and
// clears all local bisection state so the verification step can run next.
func recordFoundElement(newState *SearchState, foundMod string) {
	logging.Infof("IMCSAlgorithm: Bisection found a conflict element: %s", foundMod)
	newState.ConflictSet[foundMod] = struct{}{}
	newState.StableSet = newState.ConflictSet
	newState.Candidates = sets.SubtractSlices(newState.Candidates, []string{foundMod})
	newState.LastFoundElement = foundMod
	newState.SearchStack = make([]SearchStep, 0)
	newState.IsHandlingIndeterminate = false
	newState.IsVerifyingConflictSet = true
}

// PlanNextTest determines the next test to run based on the current state.
// This logic now directly mirrors the decision points in the formal IMCS algorithm.
func (a *IMCSAlgorithm) PlanNextTest(state SearchState) (*TestPlan, error) {
	if state.IsComplete {
		return nil, ErrSearchComplete
	}
	if state.IsHalted {
		return nil, ErrSearchHalted
	}

	// Priority 0: An INDETERMINATE result on the first half requires a complement
	// test on the second half to determine where the primary conflict lies.
	if state.IsHandlingIndeterminate {
		stable, candidates := indeterminateContext(state)
		c1, c2 := sets.Split(candidates)
		return &TestPlan{Kind: TestPlanComplement, StableSet: stable, C1: c1, C2: c2, ConflictSet: state.ConflictSet}, nil
	}

	// Priority 1: A bisection search for an element is in progress.
	if len(state.SearchStack) > 0 {
		currentStep := state.SearchStack[len(state.SearchStack)-1]
		c1, c2 := sets.Split(currentStep.Candidates)
		return &TestPlan{Kind: TestPlanContinuation, StableSet: currentStep.StableSet, C1: c1, C2: c2, ConflictSet: state.ConflictSet}, nil
	}

	// Priority 2: No bisection, but we need to run the `test(ConflictSet)` optimization.
	if state.IsVerifyingConflictSet {
		// This test only includes the conflict set itself, no other context.
		return &TestPlan{Kind: TestPlanVerification, StableSet: sets.Set{}, C1: sets.OrderedSet{}, C2: sets.OrderedSet{}, ConflictSet: state.ConflictSet}, nil
	}

	// Priority 3: No bisection, no verification. Time to start a new search for the next element.
	if len(state.Candidates) == 0 {
		// All candidates have been processed. The search is over.
		return nil, ErrSearchComplete
	}

	// This is the start of a "FindNextConflictElement" call. Plan the first test.
	// The StableSet for this new bisection is the globally confirmed ConflictSet.
	stableSet := state.ConflictSet
	c1, c2 := sets.Split(state.Candidates)

	return &TestPlan{Kind: TestPlanNewBisection, StableSet: stableSet, C1: c1, C2: c2, ConflictSet: state.ConflictSet}, nil
}

// applyComplementResult processes the result of the complement test that was
// planned in response to an INDETERMINATE result on the first half of a split.
// stable and candidates describe the step that produced the INDETERMINATE; they
// are passed in so the function needs no reference to the pre-apply state.
func applyComplementResult(newState SearchState, stable sets.Set, candidates []string, result TestResult) SearchState {
	c1, c2 := sets.Split(candidates)

	newState.IsHandlingIndeterminate = false

	switch result {
	case TestResultFail:
		// The primary conflict element is in the second half. Proceed normally.
		if len(c2) == 1 {
			recordFoundElement(&newState, c2[0])
		} else {
			// Descend into the second half with the original stable set. This is a
			// tail call, so it replaces the frame that produced the INDETERMINATE.
			replaceStackTop(&newState, newSearchStep(stable, c2))
		}
	case TestResultGood:
		// The second half is clean. The primary conflict element (if any) is in the
		// first half. Recurse into it with the second half permanently added to the
		// stable set, which suppresses the secondary conflict for the entire descent.
		newStable := sets.Union(stable, sets.MakeSet(c2))
		replaceStackTop(&newState, newSearchStep(newStable, c1))
	case TestResultIndeterminate:
		// Both halves have independent secondary conflicts and neither has a
		// verified reading, so the search cannot proceed. Halt and leave the
		// search stack intact so the UI can reconstruct the two groups from the
		// current candidate set.
		logging.Infof("IMCSAlgorithm: Both halves indeterminate (%v / %v). Halting search.", c1, c2)
		newState.IsHalted = true
	}

	logging.Debugf("IMCSAlgorithm.applyComplementResult: Applied complement result '%s'. New state: IsComplete=%t, IsHalted=%t, ConflictSet=%v, StackDepth=%d", result, newState.IsComplete, newState.IsHalted, sets.FormatSet(newState.ConflictSet), len(newState.SearchStack))

	return newState
}

// replaceStackTop replaces the current top-of-stack frame with step. A
// complement descent models a tail call, so the frame that produced the
// INDETERMINATE result must not remain on the stack beneath the new frame.
func replaceStackTop(newState *SearchState, step SearchStep) {
	if len(newState.SearchStack) > 0 {
		newState.SearchStack = newState.SearchStack[:len(newState.SearchStack)-1]
	}
	newState.SearchStack = append(newState.SearchStack, step)
}

// ApplyResult takes a state, a completed test plan, and its result,
// and returns the new, updated state. This is a pure function.
func (a *IMCSAlgorithm) ApplyResult(state SearchState, plan TestPlan, result TestResult) SearchState {
	newState := deepCopyState(state)
	newState.LastTestResult = result
	newState.IsVerifyingConflictSet = false // Flag is consumed after one use.
	newState.Step++

	// --- Handle Verification Step Result ---
	if plan.IsVerificationStep() {
		newState.Step = 0
		if result == TestResultFail {
			// The current ConflictSet is sufficient to cause a crash. We are done.
			logging.Info("IMCSAlgorithm: Verification PASSED. ConflictSet is minimal.")
			newState.IsComplete = true
		} else { // GOOD or INDETERMINATE
			// The current ConflictSet is not sufficient (or unobservable). Continue
			// the search for more elements.
			logging.Info("IMCSAlgorithm: Verification FAILED. ConflictSet not sufficient, continuing search.")
			newState.Iteration++
		}
		newState.IsHandlingIndeterminate = false
		return newState
	}

	if result == TestResultIndeterminate {
		// Each indeterminate result triggers a complement test, so it counts
		// towards the estimated maximum number of tests. A verification-step
		// INDETERMINATE does not trigger a complement test and is not counted.
		newState.IndeterminateCount++
	}

	// --- Handle Complement Test Result (INDETERMINATE handling) ---
	if state.IsHandlingIndeterminate {
		stable, candidates := indeterminateContext(state)
		return applyComplementResult(newState, stable, candidates, result)
	}

	// --- Handle Bisection Step Result ---
	var stepToProcess SearchStep
	if len(state.SearchStack) == 0 {
		// If the stack was empty, this test was the start of a new bisection.
		// The context for this step is the global ConflictSet and Candidates.
		stepToProcess = newSearchStep(state.ConflictSet, state.Candidates)
	} else {
		// A bisection was in progress. The context is the top of the stack.
		stepToProcess = state.SearchStack[len(state.SearchStack)-1]
	}

	c1, c2 := sets.Split(stepToProcess.Candidates)

	switch result {
	case TestResultFail:
		// The conflict is in the first half (c1).
		if len(c1) == 1 { // Base Case: Test of a single element + context failed.
			recordFoundElement(&newState, c1[0])
		} else { // Recursive Case: Continue bisection within c1.
			// The StableSet does not change as we descend into a failing partition.
			newState.SearchStack = append(newState.SearchStack, newSearchStep(stepToProcess.StableSet, c1))
		}
	case TestResultGood:
		// The test on c1 + context passed. The conflict must be in c2.

		// Pop the stack
		if len(newState.SearchStack) > 0 {
			newState.SearchStack = newState.SearchStack[:len(newState.SearchStack)-1]
		}

		if len(c2) > 0 {
			// The next bisection step for c2 uses an expanded StableSet, including the "good" chunk c1.
			newStableSetForNextStep := sets.Union(stepToProcess.StableSet, sets.MakeSet(c1))
			logging.Debugf("IMCSAlgorithm.ApplyResult: Test was GOOD. Adding %v to stable set for next step.", c1)
			newState.SearchStack = append(newState.SearchStack, newSearchStep(newStableSetForNextStep, c2))
		} else {
			// c2 is empty, meaning c1 was the last candidate(s) in this branch.
			// Since it was GOOD, this bisection branch found no element.
			exhaustSearch(&newState)
		}
	case TestResultIndeterminate:
		// A secondary issue prevented observing whether the primary conflict is in c1.
		if len(c2) == 0 {
			// The candidate set was a single element: that element is itself the
			// source of a secondary conflict and is treated as a non-element.
			exhaustSearch(&newState)
		} else {
			// Keep the current step context so the complement test on c2 can be
			// planned next and its result used to branch the search.
			logging.Debugf("IMCSAlgorithm.ApplyResult: Test was INDETERMINATE. Planning complement test on second half: %v", c2)
			newState.IsHandlingIndeterminate = true
		}
	}

	logging.Debugf("IMCSAlgorithm.ApplyResult: Applied result '%s'. New state: IsComplete=%t, ConflictSet=%v, StackDepth=%d", result, newState.IsComplete, sets.FormatSet(newState.ConflictSet), len(newState.SearchStack))

	return newState
}
