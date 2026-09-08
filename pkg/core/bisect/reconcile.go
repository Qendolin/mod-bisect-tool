package bisect

import (
	"errors"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// ErrNeedsReconciliation is returned by service methods that require a consistent
// state to operate, but detect that the state has been dirtied by user actions.
var ErrNeedsReconciliation = errors.New("system state is inconsistent and needs reconciliation")

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

	details := s.state.Resolver().CalculateUnresolvableModsDetails(s.getUnresolvableEvaluationSet())
	expectedUnresolvable := make(sets.Set)
	for id := range details.DirectlyUnresolvable {
		expectedUnresolvable[id] = struct{}{}
	}
	for id := range details.TransitivelyUnresolvable {
		expectedUnresolvable[id] = struct{}{}
	}

	currentlyUnresolvable := make(sets.Set)
	for id, status := range s.state.GetModStatusesSnapshot() {
		if status.IsUnresolvable {
			currentlyUnresolvable[id] = struct{}{}
		}
	}

	newlyUnresolvable := sets.Subtract(expectedUnresolvable, currentlyUnresolvable)
	newlyResolvable := sets.Subtract(currentlyUnresolvable, expectedUnresolvable)

	modStateChanged := false
	if len(newlyUnresolvable) > 0 {
		s.state.SetUnresolvableBatch(sets.MakeSlice(newlyUnresolvable), true)
		modStateChanged = true
	}
	if len(newlyResolvable) > 0 {
		s.state.SetUnresolvableBatch(sets.MakeSlice(newlyResolvable), false)
		modStateChanged = true
	}

	engineStateChanged := s.engine.Reconcile(s.getSearchCandidates())

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
