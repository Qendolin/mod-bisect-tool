package app

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/bisect"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// App orchestrates the bisection application, managing the lifecycle and core
// services. It is the top-level ui.AppController handed to the UI and exposes
// the narrower role controllers via GetBisectionController and
// GetModStatusController.
type App struct {
	view   ui.View
	logger *logging.Logger

	// Core Service (only initialized after successful loading)
	bisectSvc *bisect.Service
	adapter   *mods.FileAdapter
	// loader is the mod loader the search was actually started with (set once
	// loading begins). It may differ from the preferred loader requested via
	// the command line.
	loader mods.RunLoader

	// Role controllers exposed to the UI.
	bisection *bisectionController
	modStatus *modStatusController

	cliArgs CLIArgs
}

// NewApp creates and initializes the application logic.
func NewApp(logger *logging.Logger, cliArgs *CLIArgs) *App {
	a := &App{
		logger:  logger,
		cliArgs: *cliArgs,
	}
	a.bisection = &bisectionController{app: a}
	a.modStatus = &modStatusController{app: a}
	return a
}

func (a *App) SetView(view ui.View) {
	a.view = view
}

func (a *App) StartLoadingProcess(modsPath string, loader mods.RunLoader) {
	a.view.OnLoadingStarted()
	a.loader = loader

	a.adapter = &mods.FileAdapter{BaseDirectory: modsPath}

	go func() {
		defer logging.HandlePanic()
		overrides := loadAndMergeOverrides(modsPath, a.cliArgs)

		modLoader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: loader}, Adapter: a.adapter}
		logging.Infof("App: Loading mods from '%s', Loader: %s", modsPath, loader.String())
		allMods, providers, loadErr := modLoader.LoadMods(modsPath, overrides, a.view.OnLoadingProgress)

		a.onLoadingComplete(modsPath, allMods, providers, loadErr)
	}()
}

func (a *App) onLoadingComplete(modsPath string, allMods map[string]*mods.Mod, providers mods.PotentialProvidersMap, err error) {
	if err != nil {
		logging.Errorf("App: Failed to load mods: %v", err)
		a.view.ShowDialogErrorModLoadingGeneric(modsPath, err)
		return
	}
	if len(allMods) == 0 {
		logging.Errorf("App: No mods were found in '%s'.", modsPath)
		a.view.ShowDialogErrorModLoadingNoMods(modsPath)
		return
	}

	// Loading was successful, now create the runtime services.
	resolver := mods.NewDependencyResolver(allMods, providers, a.loader)
	stateMgr := mods.NewStateManager(allMods, resolver)

	// The loader's bridge mod must be active for every test: force-enable it so
	// it is never a search candidate and the search doesn't spend tests toggling
	// it (which would roughly double the number of tests).
	var bridgeID string
	switch a.loader {
	case mods.RunLoaderNeoForgeWithFabric:
		bridgeID = "connector"
	case mods.RunLoaderFabricWithNeoForge:
		bridgeID = "kilt"
	}
	if bridgeID != "" {
		if infos := providers[bridgeID]; len(infos) > 0 {
			bridgeMod := infos[0].TopLevelModID
			logging.Infof("App: Force-enabling bridge mod %s for loader %s.", bridgeMod, a.loader)
			stateMgr.SetForceEnabled(bridgeMod, true)
		}
	}

	activator := mods.NewModActivator(a.adapter, allMods)

	svc, err := bisect.NewService(stateMgr, activator)
	if err != nil {
		logging.Errorf("App: Failed to initialize the bisection service: %v", err)
		a.view.ShowDialogErrorBisectionInitialization(err)
		return
	}

	a.bisectSvc = svc
	a.bisectSvc.ResetSearch()

	initiallyDisabled := svc.Activator().InitiallyDisabledModIDs()
	a.view.OnInitialModStateSelection(initiallyDisabled)
}

func (a *App) finishLoading() {
	// Initial reconciliation scans the full mod set for unresolvable mods and
	// reports the directly-unresolvable roots so the UI can ask the user what
	// to do with each of them.
	report := a.bisectSvc.ReconcileState()
	if len(report.ModsUnresolvable) > 0 {
		modIDs := make([]string, 0, len(report.ModsUnresolvable))
		for modID := range report.ModsUnresolvable {
			modIDs = append(modIDs, modID)
		}
		sort.Strings(modIDs)

		var sb strings.Builder
		fmt.Fprintf(&sb, "App: %d mod(s) are unresolvable:", len(modIDs))
		allMods := a.bisectSvc.StateManager().GetAllMods()
		for _, modID := range modIDs {
			refs := formatDependencyRefs(allMods[modID], report.ModsUnresolvable[modID])
			fmt.Fprintf(&sb, "\n  - %s: missing dependencies %s", modID, strings.Join(refs, ", "))
		}
		logging.Infof("%s", sb.String())

		a.view.OnUnresolvableMods(a.buildUnresolvableModInfos(report.ModsUnresolvable))
		return
	}
	a.view.OnBisectionReady()
}

func (a *App) CompleteInitialModState(keepDisabled, omitted sets.Set) {
	if !a.IsBisectionReady() {
		return
	}
	initiallyDisabled := a.bisectSvc.Activator().InitiallyDisabledModIDs()
	initialSet := make(map[string]struct{}, len(initiallyDisabled))
	for _, id := range initiallyDisabled {
		initialSet[id] = struct{}{}
		_, keep := keepDisabled[id]
		a.bisectSvc.StateManager().SetForceDisabled(id, keep)
		a.bisectSvc.StateManager().SetOmitted(id, false)
	}
	for id := range keepDisabled {
		if _, ok := initialSet[id]; !ok {
			a.bisectSvc.StateManager().SetForceDisabled(id, true)
			a.bisectSvc.StateManager().SetOmitted(id, false)
		}
	}
	for id := range omitted {
		if slices.Contains(initiallyDisabled, id) {
			continue
		}
		a.bisectSvc.StateManager().SetForceDisabled(id, false)
		a.bisectSvc.StateManager().SetOmitted(id, true)
	}
	// TODO: Make app loading a state machine, this is horrible
	a.finishLoading()
}

// CompleteLoading finishes the loading phase. After the unresolvable mods
// screen's decisions have been applied, it merges any re-added mods so they
// participate in the search immediately, and signals the UI that loading is
// done.
func (a *App) CompleteLoading() {
	if !a.IsBisectionReady() {
		return
	}
	a.bisectSvc.Engine().MergePendingAdditions()
	a.view.OnBisectionReady()
}

func (a *App) IsBisectionReady() bool {
	return a.bisectSvc != nil
}

func (a *App) GetBisectionController() ui.BisectionController {
	return a.bisection
}

func (a *App) GetModStatusController() ui.ModStatusController {
	return a.modStatus
}

// RestoreInitialModState restores the on-disk mod files to the state they were
// in when the bisection first loaded them. It is best-effort: mods that cannot
// be restored (e.g. missing files) are logged and skipped. Safe to call even if
// no mods were loaded. It is intended to be called when the application exits,
// so user mod files are left as they were found.
func (a *App) RestoreInitialModState() {
	if !a.IsBisectionReady() {
		return
	}
	a.bisectSvc.Activator().RestoreInitialState()
}

// Reconcile triggers a reconciliation of the current mod state against the
// engine and reports any resulting changes to the UI. It is shared by the role
// controllers.
func (a *App) Reconcile() {
	logging.Debugf("App: Reconciliation triggered.")
	report := a.bisectSvc.ReconcileState()
	if report.HasChanges {
		a.showReconciliationReport(&report)
	}
	a.view.Update()
}

func (a *App) showReconciliationReport(report *bisect.ActionReport) {
	if len(report.ModsUnresolvable) > 0 {
		disabled := make(sets.Set, len(report.ModsUnresolvable))
		for id := range report.ModsUnresolvable {
			disabled[id] = struct{}{}
		}
		a.view.ShowDialogInfoBisectionUnresolvableModsDisabled(disabled)
		return
	}
	logging.Info("App: Reconciliation report has no 'Unresolvable Mods' changes. This is odd.")
}
