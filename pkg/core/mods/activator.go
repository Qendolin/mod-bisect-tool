package mods

import (
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

type FileAdapter struct {
	// Has to be a clean path
	BaseDirectory string
}

func (a FileAdapter) apply(path string, enable bool) error {
	var newPath string
	if enable {
		newPath = a.EnabledPath(path)
	} else {
		newPath = a.DisabledPath(path)
	}

	if err := os.Rename(path, newPath); err != nil {
		return err
	}

	return nil
}

// path can be a full path or just a filename
func (a FileAdapter) BasePath(path string) string {
	dir, file := filepath.Split(path)
	file = a.BaseFilename(file)
	if dir == "" {
		dir = a.BaseDirectory
	}
	return filepath.Join(dir, file)
}

func (a FileAdapter) BaseFilename(filename string) string {
	filename = strings.TrimSuffix(filename, ".disabled")
	return strings.TrimSuffix(filename, ".jar")
}

func (a FileAdapter) EnabledPath(path string) string {
	base := a.BasePath(path)
	return base + ".jar"
}

func (a FileAdapter) DisabledPath(path string) string {
	base := a.BasePath(path)
	return base + ".jar.disabled"
}

func (a FileAdapter) Disable(path string) error {
	return a.apply(path, false)
}

func (a FileAdapter) Enable(path string) error {
	return a.apply(path, true)
}

func (a FileAdapter) IsEnabledPath(path string) bool {
	return strings.HasSuffix(path, ".jar")
}

// path can be a full path or just a filename
func (a FileAdapter) IsValidPath(path string) bool {
	dir, file := filepath.Split(path)
	if dir != "" && filepath.Clean(dir) != a.BaseDirectory {
		return false
	}
	return strings.HasSuffix(file, ".jar") || strings.HasSuffix(file, ".jar.disabled")
}

func (a FileAdapter) ResolvePath(filename string) (path string, err error) {
	base := a.BasePath(filepath.Join(a.BaseDirectory, filename))
	enabledPath := base + ".jar"
	disabledPath := base + ".jar.disabled"
	if _, err := os.Stat(enabledPath); err == nil {
		return enabledPath, err
	}

	if _, err := os.Stat(disabledPath); err == nil {
		return disabledPath, err
	} else {
		return base, err
	}
}

// Activator manages the physical file state of mods (enabled/disabled).
type Activator struct {
	allMods map[string]*Mod
	adapter *FileAdapter
	snap    ActivationSnapshot // Represents the current logical state
	initial ActivationSnapshot
}

// NewModActivator creates a new activator.
func NewModActivator(adapter *FileAdapter, allMods map[string]*Mod) *Activator {
	return &Activator{
		allMods: allMods,
		adapter: adapter,
	}
}

type SnapshotStateEntry struct {
	Active  bool
	Missing bool
}

type ActivationSnapshot struct {
	States map[string]SnapshotStateEntry
}

func (a *Activator) Snapshot() ActivationSnapshot {
	if a.snap.States == nil {
		logging.Errorf("Activator: Accessed snapshot before initialization")
	}
	return a.copySnapshot(a.snap)
}

func (a *Activator) InitiallyDisabledModIDs() []string {
	ids := make([]string, 0, len(a.initial.States))
	for id, state := range a.initial.States {
		if !state.Missing && !state.Active {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// copySnapshot returns a deep copy of the given snapshot so that later
// Activator mutations cannot corrupt a previously captured snapshot.
func (a *Activator) copySnapshot(snap ActivationSnapshot) ActivationSnapshot {
	states := make(map[string]SnapshotStateEntry, len(snap.States))
	maps.Copy(states, snap.States)
	return ActivationSnapshot{States: states}
}

func (a *Activator) createSnapshot() (snap ActivationSnapshot, err error) {
	snap = ActivationSnapshot{
		States: make(map[string]SnapshotStateEntry, len(a.allMods)),
	}
	for id, mod := range a.allMods {
		path, err := a.adapter.ResolvePath(mod.BaseFilename)
		if os.IsNotExist(err) {
			logging.Warnf("Activator: JAR file at '%s' for mod %s is missing", path, id)
			snap.States[id] = SnapshotStateEntry{
				Missing: true,
			}
			continue
		} else if err != nil {
			return snap, err
		}
		enabled := a.adapter.IsEnabledPath(path)
		snap.States[id] = SnapshotStateEntry{
			Active: enabled,
		}
	}

	return snap, nil
}

// Activate calculates and executes the necessary file renames to achieve the effectiveSet state.
func (a *Activator) Activate(effectiveSet sets.Set) error {
	var toEnable, toDisable, toUpdate []string
	var missingFileErrors []*FileMissingError

	for id := range a.allMods {
		state := a.snap.States[id]
		if state.Missing {
			// Already known to be missing; handled separately.
			continue
		}

		_, wantActive := effectiveSet[id]
		if wantActive && !state.Active {
			toEnable = append(toEnable, id)
		} else if !wantActive && state.Active {
			toDisable = append(toDisable, id)
		} else {
			toUpdate = append(toUpdate, id)
		}
	}

	sort.Strings(toEnable)
	sort.Strings(toDisable)
	sort.Strings(toUpdate)

	logging.Infof("Activator: Applying changes for %d mod files\n  Enabling: %v\n  Disabling: %v", len(toEnable)+len(toDisable), toEnable, toDisable)

	// Mods being disabled may already be gone from disk; that is fine (they are
	// effectively disabled). Only mods that should be active are checked for
	// presence, matching the original behavior.
	for _, id := range toDisable {
		if err := a.set(id, false); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	for _, id := range toEnable {
		if err := a.set(id, true); os.IsNotExist(err) {
			missingFileErrors = append(missingFileErrors, &FileMissingError{ModID: id, FileBasePath: a.adapter.BasePath(a.allMods[id].BaseFilename)})
		} else if err != nil {
			return err
		}
	}

	logging.Infof("Activator: Checking state of %d unchanged mods: %v", len(toUpdate), toUpdate)
	for _, id := range toUpdate {
		state := a.snap.States[id]
		mod := a.allMods[id]
		path, err := a.adapter.ResolvePath(mod.BaseFilename)
		if os.IsNotExist(err) {
			// The file disappeared since the last activation.
			a.snap.States[id] = SnapshotStateEntry{Missing: true}
			if state.Active {
				missingFileErrors = append(missingFileErrors, &FileMissingError{ModID: id, FileBasePath: a.adapter.BasePath(mod.BaseFilename)})
			}
			continue
		} else if err != nil {
			return err
		}
		// Correct any external drift (e.g. a file renamed behind our back)
		// without a redundant self-rename when the state already matches.
		if a.adapter.IsEnabledPath(path) != state.Active {
			if err := a.set(id, state.Active); err != nil {
				return err
			}
		}
	}

	if len(missingFileErrors) > 0 {
		return &MissingFilesError{Errors: missingFileErrors}
	}

	return nil
}

// Restore applies a snapshot in order to restore a previous state.
// This is used for cleanup or undo operations.
func (a *Activator) Restore(oldSnap ActivationSnapshot) error {
	return a.restore(oldSnap, false)
}

// RestoreInitialState restores the on-disk mod files to the state they were in
// when the activator was initialized (i.e. before the bisection started). It is
// best-effort: any mod that cannot be restored (for example because its file is
// missing or locked) is logged and skipped rather than aborting the restore.
func (a *Activator) RestoreInitialState() {
	if a.initial.States == nil {
		logging.Error("Activator: Cannot restore initial state: activator was never initialized.")
		return
	}
	if err := a.restore(a.initial, true); err != nil {
		logging.Errorf("Activator: Initial state restore failed: %v", err)
	}
}

func (a *Activator) restore(oldSnap ActivationSnapshot, bestEffort bool) error {
	var toEnable, toDisable, toUpdate []string
	skippedAlreadyMissingMods := 0
	newSnap := a.snap

	for id := range a.allMods {
		oldState := oldSnap.States[id]
		newState := newSnap.States[id]

		if oldState.Missing {
			skippedAlreadyMissingMods++
			continue
		}

		if oldState.Active && !newState.Active {
			toEnable = append(toEnable, id)
		} else if !oldState.Active && newState.Active {
			toDisable = append(toDisable, id)
		} else {
			toUpdate = append(toUpdate, id)
		}
	}

	if skippedAlreadyMissingMods > 0 {
		logging.Debugf("Activator: Revert skipping %d already known missing mods.", skippedAlreadyMissingMods)
	}

	sort.Strings(toEnable)
	sort.Strings(toDisable)
	sort.Strings(toUpdate)

	logging.Infof("Activator: Reverting changes for %d mod files\n  Re-enabling: %v\n  Re-disabling: %v", len(toEnable)+len(toDisable), toEnable, toDisable)

	// In best-effort mode every failure is logged (by set) and skipped so the
	// remaining mods are still processed. Otherwise a hard error aborts the
	// restore, while missing files are always tolerated.
	shouldAbort := func(err error) bool {
		return !bestEffort && err != nil && !os.IsNotExist(err)
	}

	for _, id := range toDisable {
		if err := a.set(id, false); shouldAbort(err) {
			return err
		}
	}

	for _, id := range toEnable {
		if err := a.set(id, true); shouldAbort(err) {
			return err
		}
	}

	logging.Infof("Activator: Checking state of %d unchanged mods: %v", len(toUpdate), toUpdate)
	for _, id := range toUpdate {
		state := a.snap.States[id]
		mod := a.allMods[id]
		path, err := a.adapter.ResolvePath(mod.BaseFilename)
		if os.IsNotExist(err) {
			// File gone since the snapshot; record it and carry on (best-effort).
			a.snap.States[id] = SnapshotStateEntry{Missing: true}
			continue
		} else if shouldAbort(err) {
			return err
		}
		// Correct any external drift; otherwise nothing to do.
		if a.adapter.IsEnabledPath(path) != state.Active {
			if err := a.set(id, state.Active); shouldAbort(err) {
				return err
			}
		}
	}

	return nil
}

func (a *Activator) set(id string, active bool) error {
	mod := a.allMods[id]
	path, err := a.adapter.ResolvePath(mod.BaseFilename)
	if err != nil {
		if os.IsNotExist(err) {
			a.snap.States[id] = SnapshotStateEntry{Missing: true}
		}
		logging.Errorf("Activator: Failed to resolve mod path for %v at '%v': %v", id, path, err)
		return err
	}
	if active {
		if err := a.adapter.Enable(path); err != nil {
			if os.IsNotExist(err) {
				a.snap.States[id] = SnapshotStateEntry{Missing: true}
			}
			logging.Errorf("Activator: Failed to enable mod %v at '%v': %v", id, path, err)
			return err
		}
	} else {
		if err := a.adapter.Disable(path); err != nil {
			if os.IsNotExist(err) {
				a.snap.States[id] = SnapshotStateEntry{Missing: true}
			}
			logging.Errorf("Activator: Failed to disable mod %v at '%v': %v", id, path, err)
			return err
		}
	}

	a.snap.States[id] = SnapshotStateEntry{Active: active}

	return nil
}

// Initialize enables all non-missing mods and initializes the snapshot state
func (a *Activator) Initialize(statuses map[string]ModStatus) error {
	snap, err := a.createSnapshot()
	if err != nil {
		return err
	}
	a.snap = snap
	// Keep the pre-activation state as an independent copy
	a.initial = a.copySnapshot(snap)

	return nil
}
