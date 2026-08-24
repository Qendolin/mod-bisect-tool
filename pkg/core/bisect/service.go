package bisect

import (
	"errors"
	"fmt"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// ErrNeedsReconciliation is returned by service methods that require a consistent
// state to operate, but detect that the state has been dirtied by user actions.
var ErrNeedsReconciliation = errors.New("system state is inconsistent and needs reconciliation")
var ErrUndoStackEmpty = errors.New("cannot undo: undo stack is empty")

// ActionReport describes the outcome of a state-changing operation like
// reconciliation or advancing to the next search round.
type ActionReport struct {
	ModsSetProblematic sets.Set
	// ModsUnresolvable maps each newly-flagged, directly unresolvable mod to
	// the list of dependencies that failed to resolve. Transitively broken
	// mods are marked unresolvable but are not listed here; they resolve once
	// their root cause is dealt with.
	ModsUnresolvable map[string][]string
	HasChanges       bool
}

// TestPlan is the service-facing representation of an upcoming bisection test.
type TestPlan struct {
	ModIDsToTest       sets.Set
	IsVerificationStep bool
}

// Service encapsulates the entire bisection business logic.
type Service struct {
	state     *mods.StateManager
	activator *mods.Activator
	engine    *imcs.Engine

	enumState *Enumeration

	// lastReconcileRevision is the StateManager revision at the last time the
	// state was reconciled. NeedsReconciliation reports whether the revision
	// has since advanced, i.e. whether mod statuses changed.
	lastReconcileRevision int
}

// NewService creates a new bisect service from pre-loaded components.
func NewService(stateMgr *mods.StateManager, activator *mods.Activator) (*Service, error) {
	if err := activator.Initialize(stateMgr.GetModStatusesSnapshot()); err != nil {
		return nil, fmt.Errorf("failed to enable all mods on startup: %w", err)
	}

	initialState := imcs.NewInitialState()
	initialState.AllModIDs = stateMgr.GetAllModIDs()
	initialState.Candidates = stateMgr.GetAllModIDs()
	engine := imcs.NewEngine(initialState)

	return &Service{
		state:     stateMgr,
		activator: activator,
		engine:    engine,
		enumState: NewEnumeration(),
	}, nil
}

// --- Direct Component Access ---
func (s *Service) StateManager() *mods.StateManager { return s.state }
func (s *Service) Activator() *mods.Activator       { return s.activator }
func (s *Service) Engine() *imcs.Engine             { return s.engine }
func (s *Service) EnumerationState() *Enumeration   { return s.enumState }

// --- High-Level Workflow Methods ---

// GetCurrentState returns a read-only snapshot of the engine's state.
func (s *Service) GetCurrentState() imcs.SearchState {
	return s.engine.GetCurrentState()
}

// GetCurrentTestPlan returns a copied, service-facing preview of the next test.
func (s *Service) GetCurrentTestPlan() (*TestPlan, error) {
	plan, err := s.engine.GetCurrentTestPlan()
	if err != nil {
		return nil, err
	}
	return toServiceTestPlan(plan), nil
}

// GetActiveTestPlan returns a copied, service-facing representation of the
// test currently being executed.
func (s *Service) GetActiveTestPlan() *TestPlan {
	return toServiceTestPlan(s.engine.GetActiveTestPlan())
}

func toServiceTestPlan(plan *imcs.TestPlan) *TestPlan {
	if plan == nil {
		return nil
	}
	return &TestPlan{
		ModIDsToTest:       sets.Copy(plan.ModIDsToTest()),
		IsVerificationStep: plan.IsVerificationStep(),
	}
}

// DirectlyUnresolvableMods returns each directly-unresolvable mod mapped to the
// list of dependencies that failed to resolve.
func (s *Service) DirectlyUnresolvableMods() map[string][]string {
	return s.state.Resolver().CalculateDirectlyUnresolvableModsWithDetails(s.getUnresolvableEvaluationSet())
}

// NeedsReconciliation returns true if the mod statuses have changed since the
// last reconciliation, meaning the service state may be inconsistent and must
// be reconciled before the next step is planned.
func (s *Service) NeedsReconciliation() bool {
	return s.state.StateRevision() != s.lastReconcileRevision
}

// ReconcileState checks for and resolves dependency inconsistencies. It is safe
// to call multiple times. It returns a report of any mods whose state was changed.
func (s *Service) ReconcileState() (report ActionReport) {
	logging.Debugf("BisectService: Reconciling system state.")

	// Calculate the set of unresolvable mods with per-mod dependency details.
	// The evaluation pool includes mods that are already flagged unresolvable,
	// so their status is stable across reconciles (they are only cleared once
	// their dependencies actually become resolvable).
	details := s.state.Resolver().CalculateUnresolvableModsDetails(s.getUnresolvableEvaluationSet())
	expectedUnresolvable := make(sets.Set)
	for id := range details.DirectlyUnresolvable {
		expectedUnresolvable[id] = struct{}{}
	}
	for id := range details.TransitivelyUnresolvable {
		expectedUnresolvable[id] = struct{}{}
	}

	// Get the set of mods currently marked as unresolvable.
	currentlyUnresolvable := make(sets.Set)
	for id, status := range s.state.GetModStatusesSnapshot() {
		if status.IsUnresolvable {
			currentlyUnresolvable[id] = struct{}{}
		}
	}

	// Determine which mods need their state updated.
	newlyUnresolvable := sets.Subtract(expectedUnresolvable, currentlyUnresolvable)
	newlyResolvable := sets.Subtract(currentlyUnresolvable, expectedUnresolvable)

	// Commit the state changes.
	modStateChanged := false
	if len(newlyUnresolvable) > 0 {
		s.state.SetUnresolvableBatch(sets.MakeSlice(newlyUnresolvable), true)
		modStateChanged = true
	}
	if len(newlyResolvable) > 0 {
		s.state.SetUnresolvableBatch(sets.MakeSlice(newlyResolvable), false)
		modStateChanged = true
	}

	// After reconciliation, the bisection engine's view of candidates might be stale.
	engineStateChanged := s.engine.Reconcile(s.getSearchCandidates())

	// Record the current state revision so NeedsReconciliation is false until
	// the next actual status change. This must happen after the mutations above.
	s.lastReconcileRevision = s.state.StateRevision()

	report.HasChanges = modStateChanged || engineStateChanged
	report.ModsUnresolvable = make(map[string][]string)
	for id, deps := range details.DirectlyUnresolvable {
		if _, isNew := newlyUnresolvable[id]; isNew {
			report.ModsUnresolvable[id] = deps
		}
	}

	return
}

// PlanAndApplyNextTest is the single entry point for the UI's "Step" action.
// It will fail if the system state is inconsistent.
func (s *Service) PlanAndApplyNextTest() error {
	if s.NeedsReconciliation() {
		return ErrNeedsReconciliation
	}

	// Plan the very next logical test.
	plan, err := s.engine.PlanNextTest()
	if err != nil {
		return err // Search is complete, test in progress or cannot proceed.
	}

	testSet := plan.ModIDsToTest()
	logging.Debugf("BisectService: Plan generated. Resolving effective set for test targets: %v", sets.FormatSet(testSet))

	// 3. Resolve and activate the mod set for the test.
	logging.Info("BisectService: Resolving effective set for test targets.")
	result := s.state.ResolveEffectiveSet(testSet)
	logging.Infof("BisectService: %v", result.Path)
	for _, dep := range result.UnresolvableDeps {
		logging.Error("BisectService: " + dep.String())
	}

	statuses := s.state.GetModStatusesSnapshot()
	finalEffectiveSet := s.finalizeEffectiveSet(result.EffectiveSet, statuses)

	logging.Debugf("BisectService: Final effective set contains %d mods: %v", len(finalEffectiveSet), sets.FormatSet(finalEffectiveSet))

	restoreSnap := s.activator.Snapshot()
	if err = s.activator.Activate(finalEffectiveSet); err != nil {
		// Try restore, if that fails return original error
		if ignored := s.activator.Restore(restoreSnap); ignored != nil {
			logging.Debugf("BisectService: Activator.Apply failed and Restore too.")
			return err
		}
		logging.Debugf("BisectService: Activator.Apply failed but Restore successful")
		return err
	}

	return nil
}

// finalizeEffectiveSet takes the resolver's proposed set and applies manual overrides.
// It ensures that ForceEnabled mods are included and non-activatable mods are excluded.
func (s *Service) finalizeEffectiveSet(proposedSet sets.Set, statuses map[string]mods.ModStatus) sets.Set {
	finalSet := sets.Copy(proposedSet)

	for id, status := range statuses {
		if status.ForceEnabled {
			// A user override to force-enable a mod takes precedence.
			finalSet[id] = struct{}{}
		} else if !status.IsActivatable() {
			// Any mod that is not activatable must be excluded.
			delete(finalSet, id)
		}
	}
	return finalSet
}

// SubmitTestResult processes the outcome of a test.
func (s *Service) SubmitTestResult(result imcs.TestResult) {
	plan := s.engine.GetActiveTestPlan()
	if plan == nil {
		logging.Error("BisectService: Attempted to submit result without an active plan.")
		return
	}

	if err := s.engine.SubmitTestResult(result); err != nil {
		logging.Errorf("BisectService: Failed to submit test result to engine: %v", err)
	}
}

// UndoLastStep orchestrates a complete undo operation. It reverts the bisection engine to its previous state.
func (s *Service) UndoLastStep() error {
	if s.engine == nil {
		return errors.New("cannot undo: engine is not initialized")
	}

	undoneFrame, ok := s.engine.Undo()
	if !ok {
		return ErrUndoStackEmpty
	}
	logging.Infof("BisectService: Undone frame: Round %d, Iteration %d, Step %d.", undoneFrame.State.Round, undoneFrame.State.Iteration, undoneFrame.State.Step)

	// Undoing a step can change what's considered unresolvable; the caller is
	// expected to reconcile afterwards (see App.Undo).

	return nil
}

// CancelTest reverts file changes and invalidates the current test plan.
func (s *Service) CancelTest() {
	s.engine.InvalidateActivePlan()
}

// ContinueSearch transitions the system to the next search round. It archives
// the last result, reconciles the candidate list, creates a new engine,
// and returns a report of the changes.
func (s *Service) ContinueSearch() (ActionReport, error) {
	// This action can only be performed if the current search is complete.
	if !s.engine.GetCurrentState().IsComplete {
		return ActionReport{}, errors.New("cannot continue search: the current search is not yet complete")
	}

	lastEngine := s.engine
	lastState := lastEngine.GetCurrentState()
	lastConflictSet := lastState.ConflictSet

	// --- Phase 1: Enact Primary State Changes ---
	logging.Infof("BisectService: Starting 'Continue Search' for Round %d.", lastState.Round+1)

	// Mark the found conflict set as problematic. This is a primary state change
	// that advances the StateManager revision; the reconcile below brings the
	// service back in sync.
	s.state.SetProblematicBatch(sets.MakeSlice(lastConflictSet), true)

	// Archive the enumeration results for historical records.
	s.enumState.AddFoundConflictSet(lastConflictSet)
	s.enumState.AppendLog(lastEngine.GetExecutionLog())

	// --- Phase 2: Perform Reconciliation ---
	// Because the state is now dirty, we must reconcile it to determine any
	// newly unresolvable mods and bring the system to a consistent state.
	report := s.ReconcileState()
	report.ModsSetProblematic = lastConflictSet // Add problematic mods to the final report.
	report.HasChanges = report.HasChanges || len(lastConflictSet) > 0

	// --- Phase 3: Create New Engine ---
	// Now that the state is consistent, we can get the final list of candidates.
	finalCandidates := s.getSearchCandidates()

	if logging.IsDebugEnabled() {
		logging.Debugf("BisectService: === Continue Search Round %d Summary ===", lastState.Round+1)
		logging.Debugf("  - Last Conflict Found %d: %v", len(lastConflictSet), sets.FormatSet(lastConflictSet))
		logging.Debugf("  - All Found Conflict Sets %d: %v", len(s.enumState.FoundConflictSets), s.enumState.FoundConflictSets)
		logging.Debugf("  - Mods Marked Problematic This Round %d: %v", len(report.ModsSetProblematic), sets.FormatSet(report.ModsSetProblematic))
		logging.Debugf("  - Mods Newly Unresolvable (Auto-Disabled) This Round %d: %v", len(report.ModsUnresolvable), report.ModsUnresolvable)
		logging.Debugf("  - Final Candidate List for New Engine %d: %v", len(finalCandidates), sets.FormatSet(finalCandidates))
		logging.Debugf("BisectService: ===========================================")
	}

	nextState := imcs.NewInitialState()
	nextState.AllModIDs = s.state.GetAllModIDs()
	nextState.Candidates = sets.MakeSlice(finalCandidates)
	nextState.Round = lastState.Round + 1
	s.engine = imcs.NewEngine(nextState)
	logging.Infof("BisectService: Initialized new engine for Round %d.", s.engine.GetCurrentState().Round)

	return report, nil
}

// ResetSearch performs a hard reset of the entire bisection process.
func (s *Service) ResetSearch() {
	allModIDs := s.state.GetAllModIDs()

	// Re-initialize the entire enumeration state from scratch.
	s.enumState = NewEnumeration()

	// Reset system-set statuses for all mods.
	s.state.SetProblematicBatch(allModIDs, false)
	s.state.SetUnresolvableBatch(allModIDs, false)

	// Create a new engine with the original full set of candidates.
	initialState := imcs.NewInitialState()
	initialState.AllModIDs = allModIDs
	initialState.Candidates = allModIDs
	s.engine = imcs.NewEngine(initialState)
}

// --- Helper Methods ---

// GetCurrentExecutionLog returns the log of completed tests from the active engine.
func (s *Service) GetCurrentExecutionLog() *imcs.ExecutionLog {
	if s.engine == nil {
		return nil
	}
	return s.engine.GetExecutionLog()
}

// GetCombinedExecutionLog returns a complete history of all test steps taken
// during the entire session, combining archived logs from previous enumeration
// runs with the log from the currently active bisection.
func (s *Service) GetCombinedExecutionLog() []imcs.CompletedTest {
	if s.enumState == nil || s.enumState.ArchivedExecutionLog == nil {
		return nil
	}

	combinedEntries := s.enumState.ArchivedExecutionLog.GetEntries()

	if s.engine != nil {
		if currentLog := s.engine.GetExecutionLog(); currentLog != nil {
			combinedEntries = append(combinedEntries, currentLog.GetEntries()...)
		}
	}

	return combinedEntries
}

// getSearchCandidates identifies and returns the set of mods that are currently
// considered active participants (candidates) in the bisection search.
func (s *Service) getSearchCandidates() sets.Set {
	searchCandidates := make(sets.Set)
	for id, status := range s.state.GetModStatusesSnapshot() {
		if status.IsSearchCandidate() {
			searchCandidates[id] = struct{}{}
		}
	}
	return searchCandidates
}

// getActivatableMods identifies and returns the set of all mods that can be
// enabled, including Omitted mods which may be required as dependencies.
func (s *Service) getActivatableMods() sets.Set {
	activatableMods := make(sets.Set)
	for id, status := range s.state.GetModStatusesSnapshot() {
		if status.IsActivatable() {
			activatableMods[id] = struct{}{}
		}
	}
	return activatableMods
}

// getUnresolvableEvaluationSet returns the mods that should be evaluated for
// unresolvability during reconciliation. Unlike getActivatableMods it does not
// exclude mods that are already flagged unresolvable, so those mods stay
// flagged across reconciles until their dependencies actually become
// resolvable (e.g. after the user chooses to ignore them).
func (s *Service) getUnresolvableEvaluationSet() sets.Set {
	evaluationSet := make(sets.Set)
	for id, status := range s.state.GetModStatusesSnapshot() {
		if !status.ForceDisabled && !status.IsMissing && !status.IsProblematic {
			evaluationSet[id] = struct{}{}
		}
	}
	return evaluationSet
}
