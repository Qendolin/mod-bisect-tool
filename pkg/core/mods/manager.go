package mods

import (
	"sort"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// StateManager provides a way to manage the state of mods.
type StateManager struct {
	// The canonical source of all static top-level mod data, keyed by mod ID.
	allMods map[string]*Mod

	// Stores the runtime status for each mod, mapping mod ID to its status.
	modStatuses map[string]*ModStatus

	// Internal dependency resolver.
	resolver *DependencyResolver

	// stateRevision increments whenever any mod status actually changes (no-op
	// setters do not increment it). It is used to detect that the mod state has
	// changed since some earlier point in time, e.g. the bisection service
	// compares it against the revision at the last reconciliation to decide
	// whether reconciliation is needed again.
	stateRevision int
}

// NewStateManager creates a new mod state manager.
// It initializes the mod statuses based on the initially loaded mod data.
func NewStateManager(allMods map[string]*Mod, resolver *DependencyResolver) *StateManager {
	modStatuses := make(map[string]*ModStatus, len(allMods))
	for id, mod := range allMods {
		modStatuses[id] = &ModStatus{
			ID:            id,
			Mod:           mod,
			ForceEnabled:  false,
			ForceDisabled: false,
		}
	}
	return &StateManager{
		allMods:     allMods,
		modStatuses: modStatuses,
		resolver:    resolver,
	}
}

// StateRevision returns the current state revision. It increments whenever any
// mod status actually changes.
func (sm *StateManager) StateRevision() int {
	return sm.stateRevision
}

// SetForceEnabled updates the force-enabled state of a mod.
func (sm *StateManager) SetForceEnabled(modID string, enabled bool) {
	if status, ok := sm.modStatuses[modID]; ok {
		// No change, no revision bump.
		if status.ForceEnabled == enabled {
			return
		}
		status.ForceEnabled = enabled
		// If force-enabled, it cannot also be force-disabled.
		if enabled {
			status.ForceDisabled = false
		}
		sm.stateRevision++
	}
}

// SetForceDisabled updates the force-disabled state of a mod.
func (sm *StateManager) SetForceDisabled(modID string, disabled bool) {
	if status, ok := sm.modStatuses[modID]; ok {
		if status.ForceDisabled == disabled {
			return
		}
		status.ForceDisabled = disabled
		// If force-disabled, it cannot also be force-enabled.
		if disabled {
			status.ForceEnabled = false
		}
		sm.stateRevision++
	}
}

// SetOmitted updates the "ignored in search" state of a mod.
func (sm *StateManager) SetOmitted(modID string, isOmitted bool) {
	if status, ok := sm.modStatuses[modID]; ok {
		if status.Omitted == isOmitted {
			return
		}
		status.Omitted = isOmitted
		sm.stateRevision++
	}
}

// SetMissing updates the missing state for a single mod.
func (sm *StateManager) SetMissing(modID string, missing bool) {
	if status, ok := sm.modStatuses[modID]; ok {
		if status.IsMissing != missing {
			status.IsMissing = missing
			sm.stateRevision++
		}
	}
}

// SetProblematicBatch updates the problematic state for multiple mods at once,
// bumping the revision only once if anything changed.
func (sm *StateManager) SetProblematicBatch(modIDs []string, problematic bool) {
	var changed bool
	for _, modID := range modIDs {
		if status, ok := sm.modStatuses[modID]; ok {
			if status.IsProblematic != problematic {
				status.IsProblematic = problematic
				changed = true
			}
		}
	}
	if changed {
		sm.stateRevision++
	}
}

// SetProblematic updates the problematic state for a single mod.
func (sm *StateManager) SetProblematic(modID string, problematic bool) {
	if status, ok := sm.modStatuses[modID]; ok {
		if status.IsProblematic != problematic {
			status.IsProblematic = problematic
			sm.stateRevision++
		}
	}
}

// SetUnresolvableBatch updates the unresolvable state for multiple mods at once,
// bumping the revision only once if anything changed.
func (sm *StateManager) SetUnresolvableBatch(modIDs []string, unresolvable bool) {
	var changed bool
	for _, modID := range modIDs {
		if status, ok := sm.modStatuses[modID]; ok {
			if status.IsUnresolvable != unresolvable {
				status.IsUnresolvable = unresolvable
				changed = true
			}
		}
	}
	if changed {
		sm.stateRevision++
	}
}

// SetUnresolvable updates the unresolvable state for a single mod.
func (sm *StateManager) SetUnresolvable(modID string, unresolvable bool) {
	if status, ok := sm.modStatuses[modID]; ok {
		if status.IsUnresolvable != unresolvable {
			status.IsUnresolvable = unresolvable
			sm.stateRevision++
		}
	}
}

// SetMissingBatch updates the missing state for multiple mods at once,
// bumping the revision only once if anything changed.
func (sm *StateManager) SetMissingBatch(modIDs []string, missing bool) {
	var changed bool
	for _, modID := range modIDs {
		if status, ok := sm.modStatuses[modID]; ok {
			if status.IsMissing != missing {
				status.IsMissing = missing
				changed = true
			}
		}
	}
	if changed {
		sm.stateRevision++
	}
}

// SetForceEnabledBatch updates the force-enabled state for multiple mods at once.
// It bumps the revision only once if anything changed.
func (sm *StateManager) SetForceEnabledBatch(modIDs []string, enabled bool) {
	var changed bool
	for _, modID := range modIDs {
		if status, ok := sm.modStatuses[modID]; ok {
			if status.ForceEnabled != enabled {
				status.ForceEnabled = enabled
				if enabled {
					status.ForceDisabled = false
				}
				changed = true
			}
		}
	}
	if changed {
		sm.stateRevision++
	}
}

// SetForceDisabledBatch updates the force-disabled state for multiple mods at once.
// It bumps the revision only once if anything changed.
func (sm *StateManager) SetForceDisabledBatch(modIDs []string, disabled bool) {
	var changed bool
	for _, modID := range modIDs {
		if status, ok := sm.modStatuses[modID]; ok {
			if status.ForceDisabled != disabled {
				status.ForceDisabled = disabled
				if disabled {
					status.ForceEnabled = false
				}
				changed = true
			}
		}
	}
	if changed {
		sm.stateRevision++
	}
}

// SetOmittedBatch updates the "ignored in search" state for multiple mods at once.
// It bumps the revision only once if anything changed.
func (sm *StateManager) SetOmittedBatch(modIDs []string, omitted bool) {
	var changed bool
	for _, modID := range modIDs {
		if status, ok := sm.modStatuses[modID]; ok {
			if status.Omitted != omitted {
				status.Omitted = omitted
				changed = true
			}
		}
	}
	if changed {
		sm.stateRevision++
	}
}

// RemoveDependencies removes the given dependency IDs from a mod's metadata.
// This is used when the user chooses to keep a mod active despite its
// unresolvable dependencies: once the failing requirements are gone, the mod
// becomes activatable and a search candidate again. It bumps the state revision
// so the next reconciliation picks up the change.
func (sm *StateManager) RemoveDependencies(modID string, depIDs []string) {
	if len(depIDs) == 0 {
		return
	}
	mod, ok := sm.allMods[modID]
	if !ok {
		return
	}
	changed := false
	for _, depID := range depIDs {
		if _, exists := mod.Metadata.Depends[depID]; exists {
			delete(mod.Metadata.Depends, depID)
			changed = true
		}
	}
	if changed {
		sm.stateRevision++
	}
}

// GetModStatus returns the current ModStatus for a given modID.
// Returns nil and false if the modID is not found.
func (sm *StateManager) GetModStatus(modID string) (*ModStatus, bool) {
	status, ok := sm.modStatuses[modID]
	return status, ok
}

// GetModStatusesSnapshot returns a consistent snapshot of the current mod statuses.
func (sm *StateManager) GetModStatusesSnapshot() map[string]ModStatus {
	snapshot := make(map[string]ModStatus, len(sm.modStatuses))
	for id, status := range sm.modStatuses {
		snapStatus := *status
		snapshot[id] = snapStatus
	}
	return snapshot
}

// GetAllModIDs returns sorted IDs for all known top-level mods.
func (sm *StateManager) GetAllModIDs() []string {
	ids := make([]string, 0, len(sm.allMods))
	for id := range sm.allMods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// GetAllMods returns the map of loaded top-level mods keyed by mod ID.
func (sm *StateManager) GetAllMods() map[string]*Mod {
	return sm.allMods
}

// ResolveEffectiveSet calculates the set of active top-level mods based on the
// given target set and the current mod statuses managed by the StateManager.
func (sm *StateManager) ResolveEffectiveSet(targetSet sets.Set) ResolutionResult {
	return sm.resolver.ResolveEffectiveSet(targetSet, sm.GetModStatusesSnapshot())
}

func (sm *StateManager) Resolver() *DependencyResolver {
	return sm.resolver
}
