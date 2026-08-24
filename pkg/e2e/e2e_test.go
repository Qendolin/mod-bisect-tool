package e2e

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Qendolin/mod-bisect-tool/pkg/app"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/bisect"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// modSpec defines the structure for creating a dummy mod.
type modSpec struct {
	JSONContent string
	NestedJars  map[string]modSpec
	RawFiles    map[string]string
}

// setupDummyMods creates a temporary mods directory and files.
func setupDummyMods(t *testing.T, modsDir string, specs map[string]modSpec) {
	t.Helper()
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		t.Fatalf("failed to create mods dir '%s': %v", modsDir, err)
	}
	for filename, spec := range specs {
		jarPath := filepath.Join(modsDir, filename)
		jarBytes, err := createJarFromSpec(t, spec)
		if err != nil {
			t.Fatalf("failed to create JAR data for %s: %v", filename, err)
		}
		if err := os.WriteFile(jarPath, jarBytes, 0644); err != nil {
			t.Fatalf("failed to write dummy mod file %s: %v", jarPath, err)
		}
	}
}

// createJarFromSpec is a recursive helper to build a JAR file from a spec.
func createJarFromSpec(t *testing.T, spec modSpec) ([]byte, error) {
	t.Helper()
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	if spec.JSONContent != "" {
		modJsonFile, err := zipWriter.Create("fabric.mod.json")
		if err != nil {
			return nil, err
		}
		if _, err = modJsonFile.Write([]byte(spec.JSONContent)); err != nil {
			return nil, err
		}
	}
	for path, content := range spec.RawFiles {
		rawFile, err := zipWriter.Create(path)
		if err != nil {
			return nil, err
		}
		if _, err = rawFile.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	for nestedFilename, nestedSpec := range spec.NestedJars {
		nestedJarBytes, err := createJarFromSpec(t, nestedSpec)
		if err != nil {
			return nil, err
		}
		nestedJarFile, err := zipWriter.Create(nestedFilename)
		if err != nil {
			return nil, err
		}
		if _, err := nestedJarFile.Write(nestedJarBytes); err != nil {
			return nil, err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return zipBuf.Bytes(), nil
}

const timeout = 30 * time.Second

// newTestApp creates an app with a fresh MockView and starts loading mods from a
// temp directory populated with the given specs. It returns the app, the mock,
// and the temp mods directory.
func newTestApp(t *testing.T, specs map[string]modSpec) (*app.App, *MockView, string) {
	return newTestAppWithLoader(t, specs, mods.RunLoaderFabric)
}

// newTestAppWithLoader is newTestApp with an explicit mod loader.
func newTestAppWithLoader(t *testing.T, specs map[string]modSpec, loader mods.RunLoader) (*app.App, *MockView, string) {
	t.Helper()
	modsDir := t.TempDir()
	setupDummyMods(t, modsDir, specs)

	mainLogger := logging.NewLogger()
	cliArgs := &app.CLIArgs{NoEmbeddedOverrides: true}
	a := app.NewApp(mainLogger, cliArgs)
	mock := NewMockView()
	a.SetView(mock)
	a.StartLoadingProcess(modsDir, loader)
	return a, mock, modsDir
}

// newLoadedApp creates an app, starts loading, and waits until loading has
// completed successfully (OnBisectionReady fired).
func newLoadedApp(t *testing.T, specs map[string]modSpec) (*app.App, *MockView, string) {
	return newLoadedAppWithLoader(t, specs, mods.RunLoaderFabric)
}

// newLoadedAppWithLoader is newLoadedApp with an explicit mod loader.
func newLoadedAppWithLoader(t *testing.T, specs map[string]modSpec, loader mods.RunLoader) (*app.App, *MockView, string) {
	t.Helper()
	a, mock, modsDir := newTestAppWithLoader(t, specs, loader)
	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	a.CompleteInitialModState(sets.MakeSet(initiallyDisabled), sets.Set{})
	mock.WaitReady(t, timeout)
	return a, mock, modsDir
}

// TestLoadAndBisectionReady asserts the loading lifecycle: OnLoadingStarted,
// progress callbacks, OnBisectionReady, and a populated view model.
func TestLoadAndBisectionReady(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	calls := mock.Calls()
	if len(calls) == 0 || calls[0] != "OnLoadingStarted" {
		t.Errorf("expected OnLoadingStarted as the first call, got: %v", calls)
	}
	if !mock.HasCall("OnLoadingProgress") {
		t.Error("expected at least one OnLoadingProgress call")
	}
	if !mock.HasCall("OnBisectionReady") {
		t.Error("expected OnBisectionReady")
	}

	vm := a.GetViewModel()
	if !vm.IsReady {
		t.Error("expected IsReady to be true")
	}
	if len(vm.Mods.All) != 3 {
		t.Errorf("expected 3 mods, got %v", vm.Mods.All)
	}
	for _, id := range []string{"mod_a", "mod_b", "mod_c"} {
		if _, ok := vm.Mods.Infos[id]; !ok {
			t.Errorf("missing mod info for %s", id)
		}
	}
	if vm.Loader.Preferred != "" {
		t.Error("expected no preferred loader (no -loader flag given)")
	}
}

// TestBridgeModForceEnabled asserts that under the Connector and Kilt loaders
// the bridge mod is force-enabled right after loading, so it is never a search
// candidate and is pinned into every test's effective set.
func TestBridgeModForceEnabled(t *testing.T) {
	connectorToml := `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "connector"
version = "1.0"
displayName = "Connector"`

	tests := []struct {
		name      string
		loader    mods.RunLoader
		bridgeMod string
		specs     map[string]modSpec
	}{
		{
			name:      "Connector",
			loader:    mods.RunLoaderNeoForgeWithFabric,
			bridgeMod: "connector",
			specs: map[string]modSpec{
				"connector-1.0.jar": {RawFiles: map[string]string{"META-INF/neoforge.mods.toml": connectorToml}},
			},
		},
		{
			name:      "Kilt",
			loader:    mods.RunLoaderFabricWithNeoForge,
			bridgeMod: "kilt",
			specs: map[string]modSpec{
				"kilt-1.0.jar": {JSONContent: `{"id": "kilt", "version": "1.0", "name": "Kilt"}`},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := newLoadedAppWithLoader(t, tc.specs, tc.loader)

			statuses := a.GetModStatusController().GetModStatuses()
			st, ok := statuses[tc.bridgeMod]
			if !ok {
				t.Fatalf("expected bridge mod %s to be loaded", tc.bridgeMod)
			}
			if st.Override != ui.ModOverrideForceEnabled {
				t.Errorf("expected bridge mod %s to be force-enabled, got override %q", tc.bridgeMod, st.Override)
			}
		})
	}
}

// TestUnresolvableModsAtLoad asserts that mods with unresolvable dependencies
// are surfaced on the unresolvable mods screen right after loading (not on the
// first step), and that choosing to ignore the failing dependencies keeps the
// mod active and in the search pool.
func TestUnresolvableModsAtLoad(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		// mod_c has an unresolvable dependency, so reconciliation must flag it.
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0", "depends": {"nonexistent": ">=1.0"}}`},
	}
	a, mock, _ := newTestApp(t, specs)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	a.CompleteInitialModState(sets.MakeSet(initiallyDisabled), sets.Set{})

	// The unresolvable mods must be reported without any Step having run.
	mods := mock.WaitUnresolvable(t, timeout)
	if len(mods) != 1 || mods[0].Mod.ID != "mod_c" {
		t.Fatalf("expected only mod_c as unresolvable, got %+v", mods)
	}
	if len(mods[0].DepsDisplay) != 1 ||
		!strings.Contains(mods[0].DepsDisplay[0], "nonexistent") ||
		!strings.Contains(mods[0].DepsDisplay[0], ">=1.0") {
		t.Fatalf("expected the dependency display to include the version predicate, got %q", mods[0].DepsDisplay)
	}
	if mock.HasCall("OnTestReady") {
		t.Error("OnUnresolvableMods should have fired before any Step (no OnTestReady yet)")
	}

	// Ignore the failing dependency: mod_c is kept active and becomes a candidate.
	a.GetModStatusController().ResolveUnresolvableMods(map[string]ui.UnresolvableModAction{
		"mod_c": ui.UnresolvableModActionIgnore,
	})
	a.CompleteLoading()
	mock.WaitReady(t, timeout)

	statuses := a.GetModStatusController().GetModStatuses()
	if statuses["mod_c"].IsUnresolvable {
		t.Error("expected mod_c to no longer be unresolvable after ignoring its deps")
	}
	vm := a.GetViewModel()
	if _, inCandidates := vm.Sets.Candidate["mod_c"]; !inCandidates {
		t.Error("expected mod_c to be a candidate after ignoring its deps")
	}
}

// TestUnresolvableModsLoggedAtLoad asserts that the unresolvable mods found at
// load are logged multiline together with their failing dependencies.
func TestUnresolvableModsLoggedAtLoad(t *testing.T) {
	mainLogger := logging.NewLogger()
	logging.SetDefault(mainLogger)

	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0", "depends": {"nonexistent": ">=1.0"}}`},
	}
	modsDir := t.TempDir()
	setupDummyMods(t, modsDir, specs)

	a := app.NewApp(mainLogger, &app.CLIArgs{NoEmbeddedOverrides: true})
	mock := NewMockView()
	a.SetView(mock)
	a.StartLoadingProcess(modsDir, mods.RunLoaderFabric)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	a.CompleteInitialModState(sets.MakeSet(initiallyDisabled), sets.Set{})

	mock.WaitUnresolvable(t, timeout)

	var found bool
	for _, e := range mainLogger.Store().GetAll() {
		if strings.Contains(e.Message, "mod(s) are unresolvable") &&
			strings.Contains(e.Message, "mod_c") &&
			strings.Contains(e.Message, "nonexistent (>=1.0)") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a multiline unresolvable log mentioning mod_c and its failing dependency 'nonexistent (>=1.0)'")
	}
}

// TestUnresolvableModsDisableAtLoad asserts that choosing to disable an
// unresolvable mod at load keeps it excluded from the search.
func TestUnresolvableModsDisableAtLoad(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0", "depends": {"nonexistent": ">=1.0"}}`},
	}
	a, mock, _ := newTestApp(t, specs)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	a.CompleteInitialModState(sets.MakeSet(initiallyDisabled), sets.Set{})

	mock.WaitUnresolvable(t, timeout)
	// Empty decisions map == every mod stays disabled (the default).
	a.GetModStatusController().ResolveUnresolvableMods(nil)
	a.CompleteLoading()
	mock.WaitReady(t, timeout)

	statuses := a.GetModStatusController().GetModStatuses()
	if !statuses["mod_c"].IsUnresolvable {
		t.Error("expected mod_c to remain marked unresolvable")
	}
	vm := a.GetViewModel()
	if _, inCandidates := vm.Sets.Candidate["mod_c"]; inCandidates {
		t.Error("expected mod_c to stay excluded from candidates")
	}
}

// TestForceEnableUnresolvableBlocked asserts that an unresolvable mod cannot be
// force-enabled through the mod status controller.
func TestForceEnableUnresolvableBlocked(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0", "depends": {"nonexistent": ">=1.0"}}`},
	}
	a, mock, _ := newTestApp(t, specs)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	a.CompleteInitialModState(sets.MakeSet(initiallyDisabled), sets.Set{})

	mock.WaitUnresolvable(t, timeout)
	a.GetModStatusController().ResolveUnresolvableMods(nil) // keep mod_c disabled
	a.CompleteLoading()
	mock.WaitReady(t, timeout)

	ctrl := a.GetModStatusController()
	ctrl.SetOverride("mod_c", ui.ModOverrideForceEnabled)
	ctrl.Commit()

	statuses := a.GetModStatusController().GetModStatuses()
	if statuses["mod_c"].Override == ui.ModOverrideForceEnabled {
		t.Error("expected force-enabling an unresolvable mod to be blocked")
	}
	if !statuses["mod_c"].IsUnresolvable {
		t.Error("expected mod_c to still be unresolvable after the blocked commit")
	}
}

// TestUnresolvableModsMidSessionDisabledNoChoice asserts that an unresolvable
// mod that appears mid-session (not at load) is simply disabled and reported
// via the info dialog, without offering a per-mod choice.
func TestUnresolvableModsMidSessionDisabledNoChoice(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0", "depends": {"mod_b": ">=1.0"}}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	// Force-disable mod_b: mod_a's only provider disappears mid-session.
	// Commit blocks on the report dialog, so run it in a goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.GetModStatusController().SetOverride("mod_b", ui.ModOverrideForceDisabled)
		a.GetModStatusController().Commit()
	}()

	// The app reports it via the plain "Disabled Mods" info dialog (no choice).
	inv := mock.WaitDialog(t, timeout)
	if inv.Kind != DialogInfoBisectionUnresolvableModsDisabled {
		t.Fatalf("expected Disabled Mods info dialog, got %s", inv.Kind)
	}
	if _, ok := inv.DisabledMods["mod_a"]; !ok {
		t.Errorf("expected mod_a in disabled set, got %v", sets.MakeSlice(inv.DisabledMods))
	}
	inv.Respond(true)
	<-done

	statuses := a.GetModStatusController().GetModStatuses()
	if !statuses["mod_a"].IsUnresolvable {
		t.Error("expected mod_a to be marked unresolvable")
	}
}

// TestStepSubmitUndoResetLifecycle drives the step/test/undo/reset lifecycle
// and asserts the always-update invariant (Update is called after each op).
func TestStepSubmitUndoResetLifecycle(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	// Step
	before := mock.UpdateCount()
	a.GetBisectionController().Step()
	if !mock.HasCall("OnTestReady") {
		t.Fatal("expected OnTestReady after Step")
	}
	if mock.UpdateCount() <= before {
		t.Error("Step must end with view.Update()")
	}
	if plan := a.GetViewModel().CurrentTestPlan; !plan.IsPlanned() {
		t.Error("expected an active test plan after Step")
	}

	// Submit a test result (search proceeds)
	before = mock.UpdateCount()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultGood)
	if mock.UpdateCount() <= before {
		t.Error("SubmitTestResult must end with view.Update()")
	}

	// Undo
	before = mock.UpdateCount()
	if err := a.GetBisectionController().Undo(); err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	if mock.UpdateCount() <= before {
		t.Error("Undo must end with view.Update()")
	}

	// Undo again: stack empty, error returned but no crash
	if err := a.GetBisectionController().Undo(); !errors.Is(err, bisect.ErrUndoStackEmpty) {
		t.Errorf("expected ErrUndoStackEmpty, got %v", err)
	}

	// Reset
	before = mock.UpdateCount()
	a.GetBisectionController().ResetSearch()
	if mock.UpdateCount() <= before {
		t.Error("ResetSearch must end with view.Update()")
	}
	vm := a.GetViewModel()
	if vm.Progress.IsComplete {
		t.Error("expected search not complete after reset")
	}
}

// TestMissingFilesReconcileAndRestep asserts behavior (4): when a mod file goes
// missing during a step, the question dialog is shown; accepting it marks the
// mod missing, reconciles, and re-steps.
func TestMissingFilesReconcileAndRestep(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, modsDir := newLoadedApp(t, specs)

	// Remove a mod file that is expected to be active during the next step.
	if err := os.Remove(filepath.Join(modsDir, "mod-a-1.0.jar")); err != nil {
		t.Fatalf("failed to remove mod file: %v", err)
	}

	// Step is blocking from the caller's side: run it on a goroutine and answer
	// the dialog from the test goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer logging.HandlePanic()
		a.GetBisectionController().Step()
	}()

	inv := mock.WaitDialog(t, timeout)
	if inv.Kind != DialogQuestionBisectionContinueWithMissingMods {
		t.Fatalf("expected question dialog about missing mods, got %s", inv.Kind)
	}
	if _, ok := inv.MissingMods["mod_a"]; !ok {
		t.Errorf("expected mod_a in missing set, got %v", sets.MakeSlice(inv.MissingMods))
	}
	inv.Respond(true)

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("Step did not complete after responding to the dialog")
	}

	statuses := a.GetModStatusController().GetModStatuses()
	if !statuses["mod_a"].IsMissing {
		t.Error("expected mod_a to be marked missing after accepting the dialog")
	}
	if !mock.HasCall("OnTestReady") {
		t.Error("expected a re-step (OnTestReady) after reconciliation")
	}
}

// TestContinueSearchAfterComplete verifies that ContinueSearch reconciles
// explicitly and advances to a new round after the search completes.
func TestContinueSearchAfterComplete(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	// Run the bisection to completion (all results good => no conflict).
	for i := 0; i < 100 && !a.GetViewModel().Progress.IsComplete; i++ {
		a.GetBisectionController().Step()
		if a.GetViewModel().Progress.IsComplete {
			break
		}
		a.GetBisectionController().SubmitTestResult(imcs.TestResultGood)
	}
	if !a.GetViewModel().Progress.IsComplete {
		t.Fatal("search did not complete")
	}
	if rvm := a.GetResultViewModel(); rvm.State != ui.StateComplete {
		t.Errorf("expected StateComplete, got %s", rvm.State)
	}

	before := mock.UpdateCount()
	a.GetBisectionController().ContinueSearch()
	if mock.UpdateCount() <= before {
		t.Error("ContinueSearch must end with view.Update()")
	}
	if round := a.GetViewModel().Progress.Round; round != 2 {
		t.Errorf("expected Round 2 after ContinueSearch, got %d", round)
	}
}

// TestContinueSearchNotCompleteErrors verifies the error dialog when
// ContinueSearch is invoked before the search is complete.
func TestContinueSearchNotCompleteErrors(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer logging.HandlePanic()
		a.GetBisectionController().ContinueSearch()
	}()

	inv := mock.WaitDialog(t, timeout)
	if inv.Kind != DialogErrorBisectionCannotContinue {
		t.Fatalf("expected 'cannot continue' error dialog, got %s", inv.Kind)
	}
	inv.Respond(true)

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("ContinueSearch did not complete after responding")
	}
}

// TestCommitAndDiscardOverrides verifies staged overrides are applied atomically
// on Commit, trigger a reconciliation, and can be discarded.
func TestCommitAndDiscardOverrides(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	ctrl := a.GetModStatusController()

	// Stage then discard: nothing applied.
	ctrl.SetOverride("mod_a", ui.ModOverrideForceDisabled)
	ctrl.Discard()
	if st := a.GetModStatusController().GetModStatuses()["mod_a"]; st.Override != ui.ModOverrideNone {
		t.Errorf("expected Override None after Discard, got %s", st.Override)
	}

	// Stage then commit.
	ctrl.SetOverride("mod_b", ui.ModOverrideForceDisabled)
	before := mock.UpdateCount()
	ctrl.Commit()
	if mock.UpdateCount() <= before {
		t.Error("Commit must end with view.Update()")
	}
	if st := a.GetModStatusController().GetModStatuses()["mod_b"]; st.Override != ui.ModOverrideForceDisabled {
		t.Errorf("expected ForceDisabled override after Commit, got %s", st.Override)
	}
	vm := a.GetViewModel()
	if _, inCandidates := vm.Sets.Candidate["mod_b"]; inCandidates {
		t.Error("expected mod_b to be removed from candidates after force-disabling")
	}
}

// TestLoadErrors covers the loading error dialogs.
func TestLoadErrors(t *testing.T) {
	t.Run("no mods", func(t *testing.T) {
		a, mock, _ := newTestApp(t, map[string]modSpec{})
		inv := mock.WaitDialog(t, timeout)
		if inv.Kind != DialogErrorModLoadingNoMods {
			t.Fatalf("expected no-mods dialog, got %s", inv.Kind)
		}
		inv.Respond(true)
		if a.IsBisectionReady() {
			t.Error("bisection should not be ready after a load error")
		}
	})

	t.Run("nonexistent path", func(t *testing.T) {
		modsDir := filepath.Join(t.TempDir(), "does-not-exist")
		mainLogger := logging.NewLogger()
		cliArgs := &app.CLIArgs{NoEmbeddedOverrides: true}
		a := app.NewApp(mainLogger, cliArgs)
		mock := NewMockView()
		a.SetView(mock)
		a.StartLoadingProcess(modsDir, mods.RunLoaderFabric)

		inv := mock.WaitDialog(t, timeout)
		if inv.Kind != DialogErrorModLoadingGeneric {
			t.Fatalf("expected generic load error dialog, got %s", inv.Kind)
		}
		inv.Respond(true)
		if a.IsBisectionReady() {
			t.Error("bisection should not be ready after a load error")
		}
	})
}

// TestGetResultViewModelNotReady asserts the view model reports not-ready before
// any mods are loaded.
func TestGetResultViewModelNotReady(t *testing.T) {
	mainLogger := logging.NewLogger()
	cliArgs := &app.CLIArgs{NoEmbeddedOverrides: true}
	a := app.NewApp(mainLogger, cliArgs)
	mock := NewMockView()
	a.SetView(mock)

	if rvm := a.GetResultViewModel(); rvm.State != ui.StateNotReady {
		t.Errorf("expected StateNotReady before loading, got %s", rvm.State)
	}
	if vm := a.GetViewModel(); vm.IsReady {
		t.Error("expected IsReady false before loading")
	}
}

// TestFailDrivenBisectionVerification drives the search with failing results so
// a conflict element is isolated, a verification step is reached, and the search
// completes, asserting the result view model.
func TestFailDrivenBisectionVerification(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	// First bisection step: tests the first half of the candidates.
	a.GetBisectionController().Step()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultFail)

	// Second step narrows to a single candidate; this isolates mod_a.
	a.GetBisectionController().Step()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultFail)

	// Now the engine should be verifying the isolated conflict set.
	a.GetBisectionController().Step()
	if !a.GetViewModel().Progress.IsVerificationStep {
		t.Fatal("expected the third plan to be a verification step")
	}
	if cs := a.GetViewModel().Sets.CurrentConflict; len(cs) != 1 {
		t.Fatalf("expected a single conflict element, got %v", sets.MakeSlice(cs))
	}
	a.GetBisectionController().SubmitTestResult(imcs.TestResultFail)

	if !mock.HasCall("OnIterationComplete") {
		t.Error("expected OnIterationComplete after completion")
	}
	rvm := a.GetResultViewModel()
	if rvm.State != ui.StateComplete {
		t.Errorf("expected StateComplete, got %s", rvm.State)
	}
	if len(rvm.CurrentConflict.Mods) == 0 || rvm.CurrentConflict.Mods[0].Mod.ID != "mod_a" {
		t.Errorf("expected CurrentConflict to contain mod_a, got %+v", rvm.CurrentConflict)
	}
}

// TestCancelTest verifies that CancelTest invalidates the active plan (so the
// next Step can re-plan instead of failing with ErrTestInProgress) and updates
// the view.
func TestCancelTest(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	a.GetBisectionController().Step()
	if !a.GetViewModel().CurrentTestPlan.IsPlanned() {
		t.Fatal("expected an active plan before CancelTest")
	}

	before := mock.UpdateCount()
	a.GetBisectionController().CancelTest()
	if mock.UpdateCount() <= before {
		t.Error("CancelTest must end with view.Update()")
	}

	// If the plan were not invalidated, the next Step would hit
	// ErrTestInProgress and show a prepare-error dialog. It must re-plan.
	before = mock.UpdateCount()
	a.GetBisectionController().Step()
	if mock.UpdateCount() <= before {
		t.Error("Step after CancelTest must end with view.Update()")
	}
	if !a.GetViewModel().CurrentTestPlan.IsPlanned() {
		t.Error("expected a fresh plan after Step following CancelTest")
	}
}

// TestMissingFilesExpectedDeletions asserts the info dialog branch of the
// missing-files flow: a mod in a known conflict set that goes missing triggers
// ShowDialogInfoBisectionModsMissingExpected (not the question dialog).
func TestMissingFilesExpectedDeletions(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, modsDir := newLoadedApp(t, specs)

	// Isolate mod_a as a conflict element (same as the fail-driven test).
	a.GetBisectionController().Step()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultFail)
	a.GetBisectionController().Step()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultFail)
	if _, ok := a.GetViewModel().Sets.CurrentConflict["mod_a"]; !ok {
		t.Fatal("expected mod_a to be in the conflict set")
	}

	// Delete the enabled file of the known-problematic mod.
	if err := os.Remove(filepath.Join(modsDir, "mod-a-1.0.jar")); err != nil {
		t.Fatalf("failed to remove mod file: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer logging.HandlePanic()
		a.GetBisectionController().Step()
	}()

	inv := mock.WaitDialog(t, timeout)
	if inv.Kind != DialogInfoBisectionModsMissingExpected {
		t.Fatalf("expected 'expected missing mods' info dialog, got %s", inv.Kind)
	}
	if _, ok := inv.MissingMods["mod_a"]; !ok {
		t.Errorf("expected mod_a in missing set, got %v", sets.MakeSlice(inv.MissingMods))
	}
	inv.Respond(true)

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("Step did not complete after responding to the dialog")
	}

	if st := a.GetModStatusController().GetModStatuses()["mod_a"]; !st.IsMissing {
		t.Error("expected mod_a to be marked missing")
	}
}

// TestRestoreInitialModState asserts that RestoreInitialModState re-enables all
// mod files on disk after steps disabled some of them.
func TestRestoreInitialModState(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	a, mock, modsDir := newLoadedApp(t, specs)

	// A step activates a subset of mods, disabling the rest on disk.
	a.GetBisectionController().Step()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultGood)

	assertDisabledFiles := func(t *testing.T, want bool) {
		t.Helper()
		entries, err := os.ReadDir(modsDir)
		if err != nil {
			t.Fatalf("failed to read mods dir: %v", err)
		}
		hasDisabled := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jar.disabled") {
				hasDisabled = true
			}
		}
		if want && !hasDisabled {
			t.Error("expected at least one disabled mod file before restore")
		}
		if !want && hasDisabled {
			t.Error("expected no disabled mod files after restore")
		}
	}

	assertDisabledFiles(t, true)
	a.RestoreInitialModState()
	assertDisabledFiles(t, false)
	_ = mock
}

// TestResolveEffectiveSetWithDependencies asserts ResolveEffectiveSet pulls in
// transitive dependencies of the requested mods.
func TestResolveEffectiveSetWithDependencies(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0", "depends": {"mod_a": ">=1.0"}}`},
	}
	a, _, _ := newLoadedApp(t, specs)

	effective := a.GetModStatusController().ResolveEffectiveSet(sets.MakeSet([]string{"mod_b"}))
	for _, id := range []string{"mod_a", "mod_b"} {
		if _, ok := effective[id]; !ok {
			t.Errorf("expected %s in effective set, got %v", id, sets.MakeSlice(effective))
		}
	}
}

// TestBisectionHaltsOnDoubleIndeterminate verifies that two consecutive
// INDETERMINATE answers on the halves of the same split halt the search and
// surface a page with the two conflicting mod groups.
func TestBisectionHaltsOnDoubleIndeterminate(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
		"mod-d-1.0.jar": {JSONContent: `{"id": "mod_d", "version": "1.0"}`},
	}
	a, mock, _ := newLoadedApp(t, specs)

	// First test: first half of the initial split is indeterminate.
	a.GetBisectionController().Step()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultIndeterminate)

	// Second test: complement (second half) is also indeterminate -> halt.
	a.GetBisectionController().Step()
	a.GetBisectionController().SubmitTestResult(imcs.TestResultIndeterminate)

	inv := mock.WaitHalted(t, timeout)
	if !sets.Equal(inv.GroupA, sets.MakeSet([]string{"mod_a", "mod_b"})) ||
		!sets.Equal(inv.GroupB, sets.MakeSet([]string{"mod_c", "mod_d"})) {
		t.Fatalf("unexpected halt groups: %v / %v", sets.MakeSlice(inv.GroupA), sets.MakeSlice(inv.GroupB))
	}

	vm := a.GetViewModel()
	if !vm.Progress.IsHalted {
		t.Error("expected the view model to report IsHalted")
	}
	if vm.Progress.IsComplete {
		t.Error("expected the halted search to not be marked complete")
	}

	// Pressing Step again must not plan a new test; it re-reports the halt.
	a.GetBisectionController().Step()
	mock.WaitHalted(t, timeout)
}

// TestLoadFiresOnInitialModStateSelection asserts that the OnInitialModStateSelection callback is fired after loading, and that the initial disabled set is reported correctly.
func TestInitialModStateSelectionInitiallyDisabled(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar.disabled": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar":          {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar":          {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	_, mock, _ := newTestApp(t, specs)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	if !slices.Contains(initiallyDisabled, "mod_a") {
		t.Errorf("expected mod_a to be initially disabled, got %v", initiallyDisabled)
	}

	if len(initiallyDisabled) != 1 {
		t.Errorf("expected only mod_a to be initially disabled, got %v", initiallyDisabled)
	}
}

func TestLoadFiresOnInitialModStateSelection(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		"mod-c-1.0.jar": {JSONContent: `{"id": "mod_c", "version": "1.0"}`},
	}
	_, mock, _ := newTestApp(t, specs)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	if len(initiallyDisabled) != 0 {
		t.Errorf("expected no modules to be initially disabled, got %v", initiallyDisabled)
	}
}

func TestCompleteInitialModStateKeepDisabled(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar.disabled": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar":          {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
	}
	a, mock, _ := newTestApp(t, specs)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	if !slices.Contains(initiallyDisabled, "mod_a") {
		t.Fatalf("expected mod_a to be initially disabled, got %v", initiallyDisabled)
	}

	a.CompleteInitialModState(sets.Set{"mod_a": {}}, sets.Set{})
	mock.WaitReady(t, timeout)

	statuses := a.GetModStatusController().GetModStatuses()
	if statuses["mod_a"].Override != ui.ModOverrideForceDisabled {
		t.Fatalf("expected mod_a to stay force-disabled, got %q", statuses["mod_a"].Override)
	}
	if statuses["mod_b"].Override != ui.ModOverrideNone {
		t.Fatalf("expected mod_b to remain enabled, got %q", statuses["mod_b"].Override)
	}
}

func TestCompleteInitialModStateOmitMods(t *testing.T) {
	specs := map[string]modSpec{
		"mod-a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
		"mod-b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
	}
	a, mock, _ := newTestApp(t, specs)

	initiallyDisabled := mock.WaitInitialModStateSelection(t, timeout)
	if len(initiallyDisabled) != 0 {
		t.Fatalf("expected no mods to be initially disabled, got %v", initiallyDisabled)
	}

	a.CompleteInitialModState(sets.MakeSet(initiallyDisabled), sets.Set{"mod_b": {}})
	mock.WaitReady(t, timeout)

	statuses := a.GetModStatusController().GetModStatuses()
	if statuses["mod_b"].Override != ui.ModOverrideOmitted {
		t.Fatalf("expected mod_b to be omitted, got %q", statuses["mod_b"].Override)
	}
	if statuses["mod_a"].Override != ui.ModOverrideNone {
		t.Fatalf("expected mod_a to remain enabled, got %q", statuses["mod_a"].Override)
	}
}
