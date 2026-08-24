package tuiapp

import (
	"context"
	"fmt"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/pages"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// App is the TUI implementation of ui.View.
type App struct {
	ui.AppController
	tviewApp      *tview.Application
	layoutManager *tui.LayoutManager
	navManager    *tui.NavigationManager
	dialogManager *tui.DialogManager
	logger        *logging.Logger
	focusManager  *tui.FocusManager

	// Pages
	setupPage      *pages.SetupPage
	mainPage       *pages.MainPage
	logPage        *pages.LogPage
	loadingPage    *pages.LoadingPage
	manageModsPage *pages.ManageModsPage
	historyPage    *pages.HistoryPage

	appCtx    context.Context
	cancelApp context.CancelFunc
}

// NewApp creates and initializes the TUI application.
func NewApp(controller ui.AppController, logger *logging.Logger) *App {
	appCtx, cancelApp := context.WithCancel(context.Background())

	a := &App{
		AppController: controller,
		tviewApp:      tview.NewApplication(),
		appCtx:        appCtx,
		cancelApp:     cancelApp,
		logger:        logger,
	}

	a.layoutManager = tui.NewLayoutManager(a, a.appCtx)
	a.navManager = tui.NewNavigationManager(a, a.layoutManager.Pages())
	a.dialogManager = tui.NewDialogManager(a)
	a.focusManager = tui.NewFocusManager(a)
	a.tviewApp.SetRoot(a.layoutManager.RootPrimitive(), true).EnableMouse(true).EnablePaste(true)

	a.setupPage = pages.NewSetupPage(a)
	a.mainPage = pages.NewMainPage(a)
	a.logPage = pages.NewLogPage(a)
	a.loadingPage = pages.NewLoadingPage(a)
	a.manageModsPage = pages.NewManageModsPage(a)
	a.historyPage = pages.NewHistoryPage(a)

	a.navManager.Register(tui.PageSetupID, a.setupPage)
	a.navManager.Register(tui.PageMainID, a.mainPage)
	a.navManager.Register(tui.PageLoadingID, a.loadingPage)
	a.navManager.Register(tui.PageManageModsID, a.manageModsPage)

	a.setupGlobalInputCapture()

	return a
}

// --- TUIApp Interface implementation ---
// ExecuteAndDraw runs f on the tview event loop. QueueUpdateDraw must not be
// called from the event loop itself, so it is always invoked from a fresh
// goroutine here.
func (a *App) ExecuteAndDraw(f func()) {
	go a.tviewApp.QueueUpdateDraw(f)
}

func (a *App) Navigation() *tui.NavigationManager { return a.navManager }
func (a *App) Dialogs() *tui.DialogManager        { return a.dialogManager }
func (a *App) Layout() *tui.LayoutManager         { return a.layoutManager }
func (a *App) GetLogger() *logging.Logger         { return a.logger }
func (a *App) GetFocus() tview.Primitive          { return a.tviewApp.GetFocus() }
func (a *App) SetFocus(p tview.Primitive)         { a.tviewApp.SetFocus(p) }

// --- ui.View Interface implementation ---

func (a *App) Start() error {
	a.navManager.SwitchTo(tui.PageSetupID)
	return a.tviewApp.Run()
}

func (a *App) Stop() {
	a.cancelApp()
	a.tviewApp.Stop()
}

// Update dispatches to the current page so it can repaint itself with the
// latest state. Everything, including reading the current page, runs on the
// tview event loop, so navigation state is never touched from other goroutines.
func (a *App) Update() {
	a.ExecuteAndDraw(func() {
		if page := a.navManager.GetCurrentPage(true); page != nil {
			page.Update()
		}
	})
}

// showDialog displays a modal via the DialogManager and blocks until dismissed.
func (a *App) showDialog(show func(onDismiss func())) {
	showDialogValue(a, func(onDismiss func(struct{})) {
		show(func() { onDismiss(struct{}{}) })
	})
}

// showDialogValue displays a modal via the DialogManager and blocks until it is
// dismissed, returning the value that was passed to onDismiss.
func showDialogValue[T any](a *App, show func(onDismiss func(T))) (result T) {
	done := make(chan T)
	a.ExecuteAndDraw(func() { show(func(v T) { done <- v }) })
	return <-done
}

func (a *App) ShowDialogErrorModLoadingGeneric(path string, err error) {
	a.showDialog(func(onDismiss func()) {
		a.dialogManager.ShowErrorDialog(
			"Mod Loading Error",
			fmt.Sprintf("Failed to load mods from '%s'", path),
			err,
			func() {
				a.navManager.SwitchTo(tui.PageSetupID)
				onDismiss()
			},
		)
	})
}

func (a *App) ShowDialogErrorModLoadingNoMods(path string) {
	a.showDialog(func(onDismiss func()) {
		a.dialogManager.ShowErrorDialog(
			"Mod Loading Error",
			fmt.Sprintf("No mods were found at '%s'.\nPlease ensure that you've entered the path correctly.", path),
			nil,
			func() {
				a.navManager.SwitchTo(tui.PageSetupID)
				onDismiss()
			},
		)
	})
}

func (a *App) ShowDialogErrorBisectionInitialization(err error) {
	a.showDialog(func(onDismiss func()) {
		a.dialogManager.ShowErrorDialog(
			"Initialization Error",
			"Failed to initialize the bisection!",
			err,
			func() {
				a.navManager.SwitchTo(tui.PageSetupID)
				onDismiss()
			},
		)
	})
}

func (a *App) ShowDialogErrorBisectionCannotContinue(err error) {
	a.showDialog(func(onDismiss func()) {
		a.dialogManager.ShowErrorDialog(
			"Bisection Error",
			"Cannot continue the search!",
			err,
			onDismiss,
		)
	})
}

func (a *App) ShowDialogErrorBisectionPrepare(err error) {
	a.showDialog(func(onDismiss func()) {
		a.dialogManager.ShowErrorDialog(
			"Bisection Error",
			"An error occurred and the next step could not be prepared.\nIf another program, like Minecraft, is currently accessing your mods, please close it.\n\nPlease check the application log for details.",
			err,
			onDismiss,
		)
	})
}

func (a *App) OnBisectionHalted(groupA, groupB sets.Set) {
	a.ExecuteAndDraw(func() {
		a.navManager.ShowModal(tui.PageHaltID, pages.NewHaltPage(a, groupA, groupB, func() {
			a.navManager.CloseModal()
		}))
	})
}

func (a *App) ShowDialogInfoBisectionModsMissingExpected(missingMods sets.Set) {
	a.showDialog(func(onDismiss func()) {
		a.dialogManager.ShowInfoDialog(
			"Known Problematic Mod(s) Removed",
			"The following mod(s), which were part of a known conflict set, have been detected as missing. This is expected. The search will now proceed with the updated mod list.",
			sets.FormatSet(missingMods).String(),
			onDismiss,
		)
	})
}

func (a *App) ShowDialogInfoBisectionUnresolvableModsDisabled(disabledMods sets.Set) {
	a.showDialog(func(onDismiss func()) {
		a.dialogManager.ShowInfoDialog(
			"Disabled Mods",
			"The following mods were automatically disabled due to unmet dependencies:",
			sets.FormatSet(disabledMods).String(),
			onDismiss,
		)
	})
}

func (a *App) ShowDialogQuestionBisectionContinueWithMissingMods(missingMods sets.Set) bool {
	return showDialogValue(a, func(onDismiss func(bool)) {
		a.dialogManager.ShowQuestionDialog(
			"Missing Mod Files Detected",
			"The following mod files were unexpectedly missing. Do you want to continue the search without them?",
			sets.FormatSet(missingMods).String(),
			true,
			func() { onDismiss(true) },
			func() { onDismiss(false) },
		)
	})
}

func (a *App) OnLoadingStarted() {
	a.ExecuteAndDraw(func() { a.navManager.SwitchTo(tui.PageLoadingID) })
}

func (a *App) OnLoadingProgress(fileName string, i, count int) {
	a.ExecuteAndDraw(func() { a.loadingPage.UpdateProgress(fileName, i, count) })
}

func (a *App) OnUnresolvableMods(mods []ui.UnresolvableModInfo) {
	a.ExecuteAndDraw(func() {
		a.navManager.ShowModal(tui.PageUnresolvableID, pages.NewUnresolvablePage(a, mods))
	})
}

func (a *App) OnInitialModStateSelection(initiallyDisabled []string) {
	a.ExecuteAndDraw(func() {
		page := pages.NewInitialModStatePage(a, initiallyDisabled)
		a.navManager.ShowModal("initial_mod_state", page)
		if _, present := a.GetViewModel().Mods.Infos["crash_assistant"]; present {
			a.dialogManager.ShowQuestionDialog("Crash Assistant Detected", "Crash Assistant can slow down the search. Do you want to disable it?", "", true, func() { page.KeepDisabled("crash_assistant") }, nil)
		}
	})
}

func (a *App) OnBisectionReady() {
	a.ExecuteAndDraw(func() { a.navManager.SwitchTo(tui.PageMainID) })
}

func (a *App) OnTestReady() {
	vm := a.GetViewModel()
	isVerification := vm.Progress.IsVerificationStep
	a.ExecuteAndDraw(func() {
		testPage := pages.NewTestPage(
			a,
			isVerification,
			func() {
				a.navManager.CloseModal()
				a.GetBisectionController().SubmitTestResult(imcs.TestResultGood)
			},
			func() {
				a.navManager.CloseModal()
				a.GetBisectionController().SubmitTestResult(imcs.TestResultFail)
			},
			func() {
				a.navManager.CloseModal()
				a.GetBisectionController().SubmitTestResult(imcs.TestResultIndeterminate)
			},
			func() {
				a.navManager.CloseModal()
				a.GetBisectionController().CancelTest()
			},
		)
		a.navManager.ShowModal(tui.PageTestID, testPage)
	})
}

func (a *App) OnIterationComplete() {
	a.ExecuteAndDraw(func() {
		resultPage := pages.NewResultPage(a)
		a.navManager.ShowModal(tui.PageResultID, resultPage)
	})
}

// setupGlobalInputCapture defines application-wide keybindings.
func (a *App) setupGlobalInputCapture() {
	a.tviewApp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			if a.focusManager.Cycle(a.navManager.GetCurrentPage(true), true) {
				return nil
			}
		}
		if event.Key() == tcell.KeyBacktab {
			if a.focusManager.Cycle(a.navManager.GetCurrentPage(true), false) {
				return nil
			}
		}

		if event.Modifiers()&tcell.ModCtrl != 0 {
			switch event.Key() {
			case tcell.KeyCtrlL:
				if a.navManager.GetCurrentPageID(true) != tui.PageLogID {
					a.navManager.ShowModal(tui.PageLogID, a.logPage)
					return nil
				}
			case tcell.KeyCtrlC:
				a.ExecuteAndDraw(a.dialogManager.ShowQuitDialog)
				return nil
			case tcell.KeyCtrlH, tcell.KeyDEL: // For some fucked up reason Ctrl+H is sent as DEL in some terminals
				if a.navManager.GetCurrentPageID(true) != tui.PageHistoryID {
					a.navManager.ShowModal(tui.PageHistoryID, a.historyPage)
					return nil
				}
			}
		}
		return event
	})
}
