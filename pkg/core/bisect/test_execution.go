package bisect

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// TestPlan is the service-facing representation of an upcoming bisection test.
type TestPlan struct {
	ModIDsToTest       sets.Set
	IsVerificationStep bool
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

// PlanAndApplyNextTest is the single entry point for the UI's "Step" action.
// It will fail if the system state is inconsistent.
func (s *Service) PlanAndApplyNextTest() error {
	if s.NeedsReconciliation() {
		return ErrNeedsReconciliation
	}

	plan, err := s.engine.PlanNextTest()
	if err != nil {
		return err
	}

	testSet := plan.ModIDsToTest()
	logging.Debugf("BisectService: Plan generated. Resolving effective set for test targets: %v", sets.FormatSet(testSet))

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
			finalSet[id] = struct{}{}
		} else if !status.IsActivatable() {
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

// CancelTest reverts file changes and invalidates the current test plan.
func (s *Service) CancelTest() {
	s.engine.InvalidateActivePlan()
}
