package imcs

import (
	"math"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// Engine orchestrates the bisection search, owning the state and using
// the stateless algorithm to advance it.
type Engine struct {
	// Items that will be added to the search at the end of the current iteration
	pendingAdditions sets.Set

	algorithm    *IMCSAlgorithm
	undoStack    *UndoStack
	executionLog *ExecutionLog

	state      SearchState
	activePlan *TestPlan
}

// NewEngine creates a new search orchestrator.
func NewEngine(initialState SearchState) *Engine {
	logging.Infof("IMCSEngine: Created new engine with %d total mods.", len(initialState.AllModIDs))
	return &Engine{
		algorithm:        NewIMCSAlgorithm(),
		undoStack:        NewUndoStack(),
		executionLog:     NewExecutionLog(),
		pendingAdditions: make(sets.Set),
		state:            initialState,
	}
}

// GetCurrentTestPlan returns the active test plan if one is in progress.
// If no test is running, it returns a preview of the next plan to be executed.
// Returns nil if the search is complete.
func (e *Engine) GetCurrentTestPlan() (*TestPlan, error) {
	if e.activePlan != nil {
		return e.activePlan, nil
	}
	// Note: We call the algorithm's PlanNextTest, which is the pure function.
	return e.algorithm.PlanNextTest(e.state)
}

// PlanNextTest commits to the next test plan, changing the process state to "awaiting result".
// This is a non-idempotent action.
func (e *Engine) PlanNextTest() (*TestPlan, error) {
	if e.activePlan != nil {
		return nil, ErrTestInProgress
	}

	plan, err := e.algorithm.PlanNextTest(e.state)
	if err != nil {
		logging.Warnf("IMCSEngine: Could not plan next test: %v", err)
		return nil, err
	}

	e.activePlan = plan
	e.logPlan(plan)
	logging.Infof("IMCSEngine: Committed to test plan with %d mods.", len(plan.ModIDsToTest()))
	return plan, nil
}

func (e *Engine) logPlan(plan *TestPlan) {
	candidates := append(sets.OrderedSet{}, plan.C1...)
	candidates = append(candidates, plan.C2...)

	switch plan.Kind {
	case TestPlanComplement:
		logging.Debugf("IMCSEngine.PlanNextTest: Planning complement test after INDETERMINATE. StableSet: %v, Candidates: %v. Testing second half: %v", sets.FormatSet(plan.StableSet), candidates, plan.C2)
	case TestPlanContinuation:
		logging.Debugf("IMCSEngine.PlanNextTest: Continuing bisection (stack depth %d). StableSet: %v, Candidates: %v. Testing first half: %v", len(e.state.SearchStack), sets.FormatSet(plan.StableSet), candidates, plan.C1)
	case TestPlanVerification:
		logging.Debugf("IMCSEngine.PlanNextTest: Planning verification test for ConflictSet: %v", sets.FormatSet(plan.ConflictSet))
	case TestPlanNewBisection:
		logging.Debugf("IMCSEngine.PlanNextTest: Starting new bisection. StableSet: %v, All Candidates: %v. Testing first half: %v", sets.FormatSet(plan.StableSet), candidates, plan.C1)
	}
}

// InvalidateActivePlan cancels any in-progress test plan, usually due to an
// external state change that makes the test irrelevant.
func (e *Engine) InvalidateActivePlan() {
	if e.activePlan != nil {
		logging.Info("IMCSEngine: Invalidating active test plan due to external state change.")
		e.activePlan = nil
	}
}

// SubmitTestResult provides the result for the active test, advancing the search.
// It implicitly operates on the currently active plan.
func (e *Engine) SubmitTestResult(result TestResult) error {
	if e.activePlan == nil {
		logging.Warnf("IMCSEngine: no active test plan to submit a result for")
		return ErrNoActivePlan
	}

	logging.Infof("IMCSEngine: Submitting result '%s' for active test.", result)
	// The plan that was active before this step.
	committedPlan := *e.activePlan

	// Push the state *before* applying the result to the undo stack.
	e.undoStack.Push(UndoFrame{
		State: e.state,
		Plan:  *e.activePlan,
	})

	// Log the completed test for the UI history.
	completedTest := CompletedTest{
		Plan:            committedPlan,
		Result:          result,
		StateBeforeTest: e.state,
	}
	e.executionLog.Log(completedTest)

	// Calculate the next state.
	newState := e.algorithm.ApplyResult(e.state, committedPlan, result)

	e.state = newState
	e.activePlan = nil // Ready for the next test.

	if e.state.IsComplete {
		logging.Infof("IMCSEngine: Search is now complete. Final conflict set: %v", sets.FormatSet(e.state.ConflictSet))
	}

	// The merge logic for pending additions.
	if e.WasLastTestVerification() && len(e.pendingAdditions) > 0 {
		e.MergePendingAdditions()
	}

	return nil
}

// MergePendingAdditions merges any deferred items into the main candidate pool.
// This is called at safe-boundary points, like the end of an iteration or a manual reset.
func (e *Engine) MergePendingAdditions() {
	if len(e.pendingAdditions) == 0 {
		return
	}
	logging.Debugf("IMCSEngine: Merging pending items into candidate pool: %v", sets.FormatSet(e.pendingAdditions))
	e.AddCandidates(e.pendingAdditions)
	e.pendingAdditions = make(sets.Set) // Clear the pending list.
}

// Reconcile intelligently synchronizes the engine's internal state with a
// new set of valid candidates from an external source. It returns true if any
// internal state (active plan, pending additions, etc.) was modified.
func (e *Engine) Reconcile(validCandidates sets.Set) (changed bool) {
	logging.Debugf("IMCSEngine.Reconcile: Received %d valid candidates: %v", len(validCandidates), sets.FormatSet(validCandidates))

	// 1. Invalidate any active plan, as the underlying assumptions have changed.
	if e.activePlan != nil {
		changed = true
		e.InvalidateActivePlan()
	}

	// 2. Determine the full set of items the engine currently considers part of the search.
	currentEngineItems := sets.MakeSet(e.state.Candidates)
	currentAndPending := sets.Union(currentEngineItems, e.pendingAdditions)

	// 3. Calculate what needs to be removed and what needs to be added.
	removals := sets.Subtract(currentAndPending, validCandidates)
	additions := sets.Subtract(validCandidates, currentAndPending)

	// 4. Immediately apply all removals.
	if len(removals) > 0 {
		logging.Debugf("IMCSEngine.Reconcile: Pruning %d item(s): %v", len(removals), sets.FormatSet(removals))
		e.pendingAdditions = sets.Subtract(e.pendingAdditions, removals)
		e.RemoveCandidates(removals)
		changed = true
	}

	// 5. Defer all additions.
	if len(additions) > 0 {
		logging.Debugf("IMCSEngine.Reconcile: Deferring addition of %d item(s): %v", len(additions), sets.FormatSet(additions))
		e.pendingAdditions = sets.Union(e.pendingAdditions, additions)
		changed = true
	}
	return
}

// RemoveCandidates safely prunes a set of items from all aspects of the
// current search state, including the candidate list, conflict set, stable set,
// and any in-progress bisection steps on the search stack.
func (e *Engine) RemoveCandidates(removals sets.Set) {
	logging.Debugf("IMCSEngine.RemoveCandidates: Removing from internal state: %v", sets.FormatSet(removals))
	e.state.ConflictSet = sets.Subtract(e.state.ConflictSet, removals)
	e.state.StableSet = sets.Subtract(e.state.StableSet, removals)
	e.state.Candidates = sets.SubtractSlices(e.state.Candidates, sets.MakeSlice(removals))

	// Rebuild the search stack, pruning removed candidates from each step.
	newStack := make([]SearchStep, 0, len(e.state.SearchStack))
	for _, step := range e.state.SearchStack {
		step.Candidates = sets.SubtractSlices(step.Candidates, sets.MakeSlice(removals))
		// Only keep steps that still have candidates to test.
		if len(step.Candidates) > 0 {
			newStack = append(newStack, step)
		}
	}
	e.state.SearchStack = newStack
}

// AddCandidates safely adds a set of new items to the main candidate pool
// for future bisection iterations.
func (e *Engine) AddCandidates(additions sets.Set) {
	newCandidates := sets.MakeSet(e.state.Candidates)
	for item := range additions {
		newCandidates[item] = struct{}{}
	}
	e.state.Candidates = sets.MakeSlice(newCandidates)
}

// Returns the number of undos possible
func (e *Engine) UndoCount() int {
	return e.undoStack.Size()
}

// Undo reverts to the previous state in the search. It returns the state that
// was just popped from the undo stack, allowing the caller to inspect the
// change that was undone
func (e *Engine) Undo() (*UndoFrame, bool) {
	logging.Info("IMCSEngine: Attempting to undo last step.")
	undoneFrame, err := e.undoStack.Pop()
	if err != nil {
		logging.Warnf("IMCSEngine: Cannot undo: %v", err)
		return nil, false
	}

	// Revert the engine's state to the one from the frame.
	e.state = undoneFrame.State
	e.InvalidateActivePlan()

	logging.Infof("IMCSEngine: Successfully undid last step.")
	// Return the entire frame so the service layer can inspect the plan.
	return &undoneFrame, true
}

// GetCurrentState returns a read-only view of the current search state.
func (e *Engine) GetCurrentState() SearchState {
	return e.state
}

// GetExecutionLog provides access to the log of completed tests.
func (e *Engine) GetExecutionLog() *ExecutionLog {
	return e.executionLog
}

// GetStepCount returns the number of committed steps in the current search path.
// This is the correct value to display to the user as the current progress.
func (e *Engine) GetStepCount() int {
	return e.undoStack.Size()
}

// GetActiveTestPlan returns the plan currently being tested.
func (e *Engine) GetActiveTestPlan() *TestPlan {
	if e.activePlan == nil {
		return nil
	}
	// Return a copy to prevent mutation
	planCopy := *e.activePlan
	return &planCopy
}

// WasLastTestVerification checks if the most recently completed test was the final verification step.
func (e *Engine) WasLastTestVerification() bool {
	lastUndoFrame, ok := e.undoStack.Peek()
	return ok && lastUndoFrame.State.IsVerifyingConflictSet
}

// GetEstimatedMaxTests provides an estimated upper bound for the total tests.
// This estimate increases as more problems are found, which is intended.
func (e *Engine) GetEstimatedMaxTests() int {
	// The initial candidates are all mods participating in the search.
	// This value is also not exact, but an upper bound.
	numInitialCandidates := len(e.state.AllModIDs)

	// Number of problems found is the size of the ConflictSet.
	problemsFound := len(e.state.ConflictSet)

	if !e.state.IsComplete {
		problemsFound++
	}

	if numInitialCandidates == 0 || problemsFound == 0 {
		return 0
	}

	// The formula: problems * (ceil(log2(n)) + 1)
	// The +1 accounts for the verification step.
	// Each INDETERMINATE result adds one complement test on top of the base estimate.
	return problemsFound*(int(math.Ceil(math.Log2(float64(numInitialCandidates))))+1) + e.state.IndeterminateCount
}

// GetPendingAdditions returns the items that will be added to the search at the end of the current iteration
func (e *Engine) GetPendingAdditions() sets.Set {
	return e.pendingAdditions
}
