package app

import (
	"sync"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// modStatusController implements ui.ModStatusController. It inspects and changes
// per-mod status, staging changes via SetOverride and applying them atomically
// with Commit. The shared App remains the source of truth for the state manager
// and reconciliation.
type modStatusController struct {
	app *App

	// Staged, not yet committed, per-mod overrides for the manage mods page.
	// stagedMu guards stagedOverrides: staging is done on the UI event loop,
	// but Commit may run on a worker goroutine.
	stagedOverrides map[string]ui.ModStatusOverride
	stagedMu        sync.Mutex
}

// GetModStatuses returns a serializable snapshot of every mod's status, merged
// with any staged (not yet committed) overrides.
func (m *modStatusController) GetModStatuses() map[string]ui.ModStatusViewModel {
	if !m.app.IsBisectionReady() {
		return map[string]ui.ModStatusViewModel{}
	}

	allMods := m.app.bisectSvc.StateManager().GetAllMods()
	result := make(map[string]ui.ModStatusViewModel, len(allMods))

	m.stagedMu.Lock()
	staged := m.stagedOverrides
	m.stagedMu.Unlock()

	for id, status := range m.app.bisectSvc.StateManager().GetModStatusesSnapshot() {
		vm := ui.ModStatusViewModel{
			ModViewModel:   makeModVM(id, allMods),
			IsMissing:      status.IsMissing,
			IsProblematic:  status.IsProblematic,
			IsUnresolvable: status.IsUnresolvable,
			IsUserEditable: !status.IsMissing,
		}
		if override, ok := staged[id]; ok {
			vm.Override = override
		} else {
			vm.Override = overrideFromStatus(status)
		}
		result[id] = vm
	}
	return result
}

// SetOverride stages a new override for a single mod. It does not touch the
// underlying state until Commit is called.
func (m *modStatusController) SetOverride(id string, override ui.ModStatusOverride) {
	if !m.app.IsBisectionReady() {
		return
	}
	m.stagedMu.Lock()
	defer m.stagedMu.Unlock()
	if m.stagedOverrides == nil {
		m.stagedOverrides = make(map[string]ui.ModStatusOverride)
	}
	m.stagedOverrides[id] = override
}

// Commit applies all staged overrides to the state manager and triggers a
// reconciliation. Pending additions (mods that will re-enter the search pool)
// are available via GetViewModel().PendingAdditions afterwards.
func (m *modStatusController) Commit() {
	if !m.app.IsBisectionReady() {
		m.Discard()
		return
	}

	m.stagedMu.Lock()
	overrides := m.stagedOverrides
	m.stagedOverrides = nil
	m.stagedMu.Unlock()

	for id, override := range overrides {
		switch override {
		case ui.ModOverrideNone:
			m.app.bisectSvc.StateManager().SetForceEnabled(id, false)
			m.app.bisectSvc.StateManager().SetForceDisabled(id, false)
			m.app.bisectSvc.StateManager().SetOmitted(id, false)
		case ui.ModOverrideForceEnabled:
			// Unresolvable mods cannot be force-enabled; they are dealt with on
			// the unresolvable mods screen instead.
			if status, ok := m.app.bisectSvc.StateManager().GetModStatus(id); ok && status.IsUnresolvable {
				continue
			}
			m.app.bisectSvc.StateManager().SetForceEnabled(id, true)
			m.app.bisectSvc.StateManager().SetOmitted(id, false)
		case ui.ModOverrideForceDisabled:
			m.app.bisectSvc.StateManager().SetForceDisabled(id, true)
			m.app.bisectSvc.StateManager().SetOmitted(id, false)
		case ui.ModOverrideOmitted:
			m.app.bisectSvc.StateManager().SetOmitted(id, true)
			m.app.bisectSvc.StateManager().SetForceEnabled(id, false)
			m.app.bisectSvc.StateManager().SetForceDisabled(id, false)
		}
	}
	m.app.Reconcile()
}

// Discard drops all staged overrides without applying them.
func (m *modStatusController) Discard() {
	m.stagedMu.Lock()
	m.stagedOverrides = nil
	m.stagedMu.Unlock()
}

// ResolveEffectiveSet calculates the full set of mods that would be active for
// a test of the given target mods, including any dependencies pulled in
// transitively.
func (m *modStatusController) ResolveEffectiveSet(targetSet sets.Set) sets.Set {
	if !m.app.IsBisectionReady() {
		return sets.Set{}
	}
	return m.app.bisectSvc.StateManager().ResolveEffectiveSet(targetSet).EffectiveSet
}

// ResolveUnresolvableMods applies the user's decisions from the unresolvable
// mods screen. Mods marked UnresolvableModActionIgnore have their failing
// dependencies dropped and stay active; everything else stays disabled. The
// state is reconciled afterwards.
func (m *modStatusController) ResolveUnresolvableMods(decisions map[string]ui.UnresolvableModAction) {
	if !m.app.IsBisectionReady() {
		return
	}
	details := m.app.bisectSvc.DirectlyUnresolvableMods()
	for modID, action := range decisions {
		if action == ui.UnresolvableModActionIgnore {
			m.app.bisectSvc.StateManager().RemoveDependencies(modID, details[modID])
		}
	}
	m.app.bisectSvc.ReconcileState()
}

// overrideFromStatus maps a committed mod status to its override enum.
func overrideFromStatus(status mods.ModStatus) ui.ModStatusOverride {
	switch {
	case status.ForceEnabled:
		return ui.ModOverrideForceEnabled
	case status.ForceDisabled:
		return ui.ModOverrideForceDisabled
	case status.Omitted:
		return ui.ModOverrideOmitted
	default:
		return ui.ModOverrideNone
	}
}
