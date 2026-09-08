package bisect

import (
	"errors"
	"fmt"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

var ErrUndoStackEmpty = errors.New("cannot undo: undo stack is empty")

// Service encapsulates the entire bisection business logic.
type Service struct {
	state     *mods.StateManager
	activator *mods.Activator
	engine    *imcs.Engine

	enumState *Enumeration

	inferredDepsAttempted bool

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

// GetCurrentState returns a read-only snapshot of the engine's state.
func (s *Service) GetCurrentState() imcs.SearchState {
	return s.engine.GetCurrentState()
}

// ContinueSearch transitions the system to the next search round. It archives
// the last result, reconciles the candidate list, creates a new engine,
// and returns a report of the changes.
func (s *Service) ContinueSearch() (ActionReport, error) {
	if !s.engine.GetCurrentState().IsComplete {
		return ActionReport{}, errors.New("cannot continue search: the current search is not yet complete")
	}

	lastEngine := s.engine
	lastState := lastEngine.GetCurrentState()
	lastConflictSet := lastState.ConflictSet

	logging.Infof("BisectService: Starting 'Continue Search' for Round %d.", lastState.Round+1)

	s.state.SetProblematicBatch(sets.MakeSlice(lastConflictSet), true)

	s.enumState.AddFoundConflictSet(lastConflictSet)
	s.enumState.AppendLog(lastEngine.GetExecutionLog())

	report := s.ReconcileState()
	report.ModsSetProblematic = lastConflictSet
	report.HasChanges = report.HasChanges || len(lastConflictSet) > 0

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

	s.enumState = NewEnumeration()

	s.state.SetProblematicBatch(allModIDs, false)
	s.state.SetUnresolvableBatch(allModIDs, false)

	s.inferredDepsAttempted = false
	s.lastReconcileRevision = s.state.StateRevision()

	initialState := imcs.NewInitialState()
	initialState.AllModIDs = allModIDs
	initialState.Candidates = allModIDs
	s.engine = imcs.NewEngine(initialState)
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

	return nil
}

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
