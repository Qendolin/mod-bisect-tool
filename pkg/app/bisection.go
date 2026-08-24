package app

import (
	"errors"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/bisect"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// bisectionController implements ui.BisectionController. It drives the bisection
// search. The shared App remains the source of truth for the bisection service,
// the view, and cross-cutting operations like reconciliation.
type bisectionController struct {
	app *App
}

func (b *bisectionController) IsBisectionReady() bool { return b.app.IsBisectionReady() }

// Reconcile triggers a reconciliation of the current mod state. It is part of
// ui.BisectionController but is shared with the mod status controller, so it is
// implemented by the App.
func (b *bisectionController) Reconcile() { b.app.Reconcile() }

// Step orchestrates the next bisection test.
func (b *bisectionController) Step() {
	if !b.app.IsBisectionReady() {
		return
	}
	err := b.app.bisectSvc.PlanAndApplyNextTest()
	if err != nil {
		b.app.bisectSvc.Engine().InvalidateActivePlan()
		b.handleStepError(err)
		b.app.view.Update()
		return
	}

	b.app.view.OnTestReady()
	b.app.view.Update()
}

func (b *bisectionController) SubmitTestResult(result imcs.TestResult) {
	b.app.bisectSvc.SubmitTestResult(result)
	state := b.app.bisectSvc.GetCurrentState()
	if state.IsHalted {
		b.showHaltedPage()
	} else {
		b.displayResults()
	}
	b.app.view.Update()
}

// showHaltedPage shows the halt page with the two candidate halves that
// were being tested when the search halted, mirroring the split the bisection
// algorithm performs on the current candidate set.
func (b *bisectionController) showHaltedPage() {
	candidateSlice := sets.MakeSlice(b.app.bisectSvc.GetCurrentState().GetCandidateSet())
	groupA, groupB := sets.Split(candidateSlice)
	b.app.view.OnBisectionHalted(sets.MakeSet(groupA), sets.MakeSet(groupB))
}

func (b *bisectionController) CancelTest() {
	b.app.bisectSvc.CancelTest()
	b.app.view.Update()
}

func (b *bisectionController) ContinueSearch() {
	if !b.app.IsBisectionReady() {
		return
	}
	logging.Debugf("App: ContinueSearch action triggered.")

	report, err := b.app.bisectSvc.ContinueSearch()
	if err != nil {
		b.app.view.ShowDialogErrorBisectionCannotContinue(err)
		b.app.view.Update()
		return
	}

	if len(report.ModsUnresolvable) > 0 {
		disabled := make(sets.Set, len(report.ModsUnresolvable))
		for id := range report.ModsUnresolvable {
			disabled[id] = struct{}{}
		}
		b.app.view.ShowDialogInfoBisectionUnresolvableModsDisabled(disabled)
	}
	b.app.view.Update()
}

func (b *bisectionController) Undo() error {
	err := b.app.bisectSvc.UndoLastStep()
	if err != nil {
		logging.Errorf("App: Undo failed: %v", err)
	} else {
		logging.Debugf("App: Undo successful.")
		b.app.Reconcile()
	}
	return err
}

func (b *bisectionController) ResetSearch() {
	logging.Debugf("App: ResetSearch faction triggered.")
	b.app.bisectSvc.ResetSearch()
	b.app.Reconcile()
}

func (b *bisectionController) displayResults() {
	if !b.app.IsBisectionReady() {
		return
	}
	state := b.app.bisectSvc.GetCurrentState()
	if state.IsComplete || b.app.bisectSvc.Engine().WasLastTestVerification() {
		b.app.view.OnIterationComplete()
	}
}

func (b *bisectionController) handleStepError(err error) {
	if errors.Is(err, imcs.ErrSearchComplete) {
		logging.Infof("App: Step error, bisection complete: %s", err)
		b.displayResults()
		return
	}

	if errors.Is(err, imcs.ErrSearchHalted) {
		logging.Warnf("App: Step error, bisection halted: %s", err)
		b.showHaltedPage()
		return
	}

	if missingErr, ok := err.(*mods.MissingFilesError); ok {
		logging.Warnf("App: Step error, missing files: %v", missingErr)

		vm := b.app.GetViewModel()

		allKnownConflicts := sets.Copy(vm.Sets.CurrentConflict)
		for _, s := range vm.Sets.AllConflicts {
			allKnownConflicts = sets.Union(allKnownConflicts, s)
		}

		unexpectedDeletions := make(sets.Set)
		expectedDeletions := make(sets.Set)
		var missingIDs []string

		for _, e := range missingErr.Errors {
			missingIDs = append(missingIDs, e.ModID)
			if _, isProblem := allKnownConflicts[e.ModID]; isProblem {
				expectedDeletions[e.ModID] = struct{}{}
			} else {
				unexpectedDeletions[e.ModID] = struct{}{}
			}
		}

		if len(unexpectedDeletions) > 0 {
			ok := b.app.view.ShowDialogQuestionBisectionContinueWithMissingMods(unexpectedDeletions)
			if ok {
				logging.Infof("App: Disabling %d mods that are unexpectedly missing: %v", len(missingIDs), missingIDs)
				b.app.bisectSvc.StateManager().SetMissingBatch(missingIDs, true)
				b.app.Reconcile()
				b.Step()
			}
		} else {
			b.app.view.ShowDialogInfoBisectionModsMissingExpected(expectedDeletions)
			logging.Infof("App: Disabling %d mods that are expectedly missing: %v", len(missingIDs), missingIDs)
			b.app.bisectSvc.StateManager().SetMissingBatch(missingIDs, true)
			b.app.Reconcile()
			b.Step()
		}
		return
	}

	if errors.Is(err, bisect.ErrNeedsReconciliation) {
		report := b.app.bisectSvc.ReconcileState()
		if report.HasChanges {
			b.app.showReconciliationReport(&report)
			b.Step()
		} else {
			logging.Error("App: Reconciliation triggered by ErrNeedsReconciliation but reconciliation yielded no changes.")
			b.Step()
		}
		return
	}

	logging.Errorf("App: Step error: %v", err)

	b.app.view.ShowDialogErrorBisectionPrepare(err)
}
