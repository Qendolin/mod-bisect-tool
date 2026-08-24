package mods

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// writeModFile creates an empty mod file (either active or disabled) on disk.
func writeModFile(t *testing.T, adapter *FileAdapter, baseFilename, suffix string) {
	t.Helper()
	path := filepath.Join(adapter.BaseDirectory, baseFilename+suffix)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
}

// assertModFileState verifies which physical file exists for a mod.
func assertModFileState(t *testing.T, adapter *FileAdapter, baseFilename string, wantActive bool) {
	t.Helper()
	base := filepath.Join(adapter.BaseDirectory, baseFilename)
	_, enabledErr := os.Stat(base + ".jar")
	_, disabledErr := os.Stat(base + ".jar.disabled")
	enabled := enabledErr == nil
	disabled := disabledErr == nil

	if wantActive {
		if !enabled || disabled {
			t.Errorf("expected %q to be active (.jar) but enabled=%v disabled=%v", baseFilename, enabled, disabled)
		}
	} else {
		if enabled || !disabled {
			t.Errorf("expected %q to be disabled (.jar.disabled) but enabled=%v disabled=%v", baseFilename, enabled, disabled)
		}
	}
}

// newActivatorTestFixture creates an activator over the given base filenames,
// with the files created on disk (as active jars) and the activator initialized.
func newActivatorTestFixture(t *testing.T, baseFilenames ...string) (*FileAdapter, *Activator) {
	t.Helper()
	adapter := &FileAdapter{BaseDirectory: t.TempDir()}
	allMods := make(map[string]*Mod, len(baseFilenames))
	statuses := make(map[string]ModStatus, len(baseFilenames))
	for _, base := range baseFilenames {
		writeModFile(t, adapter, base, ".jar")
		mod := &Mod{BaseFilename: base}
		allMods[base] = mod
		statuses[base] = ModStatus{ID: base, Mod: mod}
	}

	act := NewModActivator(adapter, allMods)
	if err := act.Initialize(statuses); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	return adapter, act
}

// TestActivatorRestoreReDisables verifies that Restore re-disables a mod which
// was disabled at snapshot time but is active at restore time. This branch was
// previously dead code (`!newState.Active && newState.Active` is always false),
// so Restore never re-disabled anything, leaving rollbacks incomplete.
func TestActivatorRestoreReDisables(t *testing.T) {
	adapter := &FileAdapter{BaseDirectory: t.TempDir()}
	writeModFile(t, adapter, "a", ".jar")
	writeModFile(t, adapter, "b", ".jar")
	writeModFile(t, adapter, "c", ".jar.disabled") // c starts disabled on disk

	allMods := map[string]*Mod{
		"a": {BaseFilename: "a"},
		"b": {BaseFilename: "b"},
		"c": {BaseFilename: "c"},
	}
	statuses := map[string]ModStatus{
		"a": {ID: "a", Mod: allMods["a"]},
		"b": {ID: "b", Mod: allMods["b"]},
		"c": {ID: "c", Mod: allMods["c"]},
	}
	act := NewModActivator(adapter, allMods)
	if err := act.Initialize(statuses); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	// Initialize records initial state without altering files on disk.
	assertModFileState(t, adapter, "a", true)
	assertModFileState(t, adapter, "b", true)
	assertModFileState(t, adapter, "c", false)

	// Disable c, then snapshot the state to roll back to.
	if err := act.Activate(sets.MakeSet([]string{"a", "b"})); err != nil {
		t.Fatalf("Activate (disable c) failed: %v", err)
	}
	assertModFileState(t, adapter, "c", false)
	rollbackTarget := act.Snapshot() // a + b active, c disabled

	// Enable c again, the state we want to roll back from.
	if err := act.Activate(sets.MakeSet([]string{"a", "b", "c"})); err != nil {
		t.Fatalf("Activate (re-enable c) failed: %v", err)
	}
	assertModFileState(t, adapter, "c", true)

	// Restore must disable c again.
	if err := act.Restore(rollbackTarget); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	assertModFileState(t, adapter, "a", true)
	assertModFileState(t, adapter, "b", true)
	assertModFileState(t, adapter, "c", false)
}

// TestActivatorRestoreReEnables verifies the re-enable rollback path: a mod that
// was active at snapshot time but got disabled must be enabled again.
func TestActivatorRestoreReEnables(t *testing.T) {
	adapter, act := newActivatorTestFixture(t, "a", "b")
	rollbackTarget := act.Snapshot() // a + b active

	if err := act.Activate(sets.MakeSet([]string{"a"})); err != nil {
		t.Fatalf("Activate (disable b) failed: %v", err)
	}
	assertModFileState(t, adapter, "b", false)

	if err := act.Restore(rollbackTarget); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	assertModFileState(t, adapter, "a", true)
	assertModFileState(t, adapter, "b", true)
}

// TestActivatorRestoreNoOp verifies that restoring to the current state changes
// nothing on disk.
func TestActivatorRestoreNoOp(t *testing.T) {
	adapter, act := newActivatorTestFixture(t, "a", "b", "c")

	snap := act.Snapshot()
	if err := act.Restore(snap); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	for _, base := range []string{"a", "b", "c"} {
		assertModFileState(t, adapter, base, true)
	}
}

// TestActivatorActivateIgnoresMissingDisabledMods verifies that Activate does
// not report an error when the file of a mod that should be disabled (or that
// is already inactive) is missing from disk.
func TestActivatorActivateIgnoresMissingDisabledMods(t *testing.T) {
	adapter, act := newActivatorTestFixture(t, "a", "b")

	// Simulate an external deletion of b's jar.
	if err := os.Remove(filepath.Join(adapter.BaseDirectory, "b.jar")); err != nil {
		t.Fatalf("failed to remove b.jar: %v", err)
	}

	// a should stay active; b should be disabled but its file is gone.
	if err := act.Activate(sets.MakeSet([]string{"a"})); err != nil {
		t.Fatalf("Activate should not fail for a missing disabled mod, got: %v", err)
	}
	assertModFileState(t, adapter, "a", true)
}

// TestActivatorActivateReportsMissingActiveMod verifies that Activate reports a
// MissingFilesError when a mod that should be active has no file on disk.
func TestActivatorActivateReportsMissingActiveMod(t *testing.T) {
	adapter, act := newActivatorTestFixture(t, "a")

	// Simulate an external deletion of a's jar.
	if err := os.Remove(filepath.Join(adapter.BaseDirectory, "a.jar")); err != nil {
		t.Fatalf("failed to remove a.jar: %v", err)
	}

	err := act.Activate(sets.MakeSet([]string{"a"}))
	if err == nil {
		t.Fatal("Activate should report a missing expected-active mod, got nil")
	}
	if _, ok := err.(*MissingFilesError); !ok {
		t.Fatalf("expected *MissingFilesError, got %T: %v", err, err)
	}
}

// TestActivatorRestoreInitialState verifies that RestoreInitialState returns the
// on-disk mod files to the state they had when the activator was initialized.
func TestActivatorRestoreInitialState(t *testing.T) {
	adapter := &FileAdapter{BaseDirectory: t.TempDir()}
	writeModFile(t, adapter, "a", ".jar")          // a starts active
	writeModFile(t, adapter, "b", ".jar.disabled") // b starts disabled

	allMods := map[string]*Mod{
		"a": {BaseFilename: "a"},
		"b": {BaseFilename: "b"},
	}
	statuses := map[string]ModStatus{
		"a": {ID: "a", Mod: allMods["a"]},
		"b": {ID: "b", Mod: allMods["b"]},
	}
	act := NewModActivator(adapter, allMods)
	if err := act.Initialize(statuses); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	// Initialize does not change the mod state, so b is still disabled.
	assertModFileState(t, adapter, "b", false)

	// Run a test that flips both mods.
	if err := act.Activate(sets.MakeSet([]string{"b"})); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}
	assertModFileState(t, adapter, "a", false)
	assertModFileState(t, adapter, "b", true)

	// Restore the initial state: a active, b disabled again.
	act.RestoreInitialState()
	assertModFileState(t, adapter, "a", true)
	assertModFileState(t, adapter, "b", false)
}

// TestActivatorRestoreInitialStateBestEffort verifies that a mod whose file has
// gone missing does not abort the initial-state restore (best-effort).
func TestActivatorRestoreInitialStateBestEffort(t *testing.T) {
	adapter := &FileAdapter{BaseDirectory: t.TempDir()}
	writeModFile(t, adapter, "a", ".jar")
	writeModFile(t, adapter, "b", ".jar")

	allMods := map[string]*Mod{
		"a": {BaseFilename: "a"},
		"b": {BaseFilename: "b"},
	}
	statuses := map[string]ModStatus{
		"a": {ID: "a", Mod: allMods["a"]},
		"b": {ID: "b", Mod: allMods["b"]},
	}
	act := NewModActivator(adapter, allMods)
	if err := act.Initialize(statuses); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Disable b so the restore has something to re-enable.
	if err := act.Activate(sets.MakeSet([]string{"a"})); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}
	assertModFileState(t, adapter, "b", false)

	// Delete a's jar: a is currently active but its file is now gone.
	if err := os.Remove(filepath.Join(adapter.BaseDirectory, "a.jar")); err != nil {
		t.Fatalf("failed to remove a.jar: %v", err)
	}

	// The restore must still re-enable b and tolerate the missing a.
	act.RestoreInitialState()
	assertModFileState(t, adapter, "b", true)
}
