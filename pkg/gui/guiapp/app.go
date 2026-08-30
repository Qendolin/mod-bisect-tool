package guiapp

import (
	"sync"
	"sync/atomic"

	gioapp "gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	guii18n "github.com/Qendolin/mod-bisect-tool/pkg/gui/i18n"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/screens"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/ncruces/zenity"
)

type Screen interface {
	Layout(gtx layout.Context, th *material.Theme) layout.Dimensions
}

// App is the GUI implementation of ui.View using Gio.
type App struct {
	ui.AppController

	window     *gioapp.Window
	theme      *material.Theme
	logger     *logging.Logger
	translator *guii18n.Translator

	// Screen pages
	setupScreen        *screens.SetupScreen
	loadingScreen      *screens.LoadingScreen
	modSelectionScreen *screens.SetupRequiredModsScreen
	mainScreen         *screens.MainScreen
	resultScreen       *screens.ResultScreen

	activeScreen Screen

	// Custom window decorations. The native title bar is disabled
	// (Decorated(false)) so we can intercept the close action and show a quit
	// confirmation; Gio's DestroyEvent cannot be cancelled.
	decorations widget.Decorations

	// Thread-safe callbacks
	mu        sync.Mutex
	callbacks []func()

	shouldQuit bool

	// quitDialogOpen guards against spawning multiple quit confirmation
	// dialogs (e.g. from the decorations close button and an in-frame button).
	quitDialogOpen atomic.Bool

	// attachID is the platform window handle used to attach native zenity
	// dialogs to the main window, making it inert while a dialog is open. It is
	// captured from the platform ViewEvent (see view_handle_*.go) during the
	// first frames, before any dialog can be triggered.
	attachID any
}

func NewApp(controller ui.AppController, logger *logging.Logger, locale string) *App {
	translator := guii18n.New(locale)
	window := new(gioapp.Window)
	window.Option(gioapp.Title(translator.Text("app_name", "Mod Bisect Tool", nil)), gioapp.Size(unit.Dp(800), unit.Dp(600)), gioapp.Decorated(false))

	a := &App{
		AppController: controller,
		window:        window,
		theme:         theme.NewTheme(),
		logger:        logger,
		translator:    translator,
	}

	a.setupScreen = screens.NewSetupScreen(a)
	a.loadingScreen = screens.NewLoadingScreen(a)
	a.modSelectionScreen = screens.NewSetupRequiredModsScreen(a)
	a.mainScreen = screens.NewMainScreen(a)
	a.resultScreen = screens.NewResultScreen(a)

	a.activeScreen = a.setupScreen

	return a
}

func (a *App) Translator() *guii18n.Translator { return a.translator }

func (a *App) Text(id, fallback string, data map[string]any) string {
	return a.translator.Text(id, fallback, data)
}

func (a *App) SetLocale(locale string) {
	a.translator.SetLocale(locale)
	a.Update()
}

func (a *App) Stop() {
	a.shouldQuit = true
	a.window.Invalidate()
}

func (a *App) Start() error {
	var ops op.Ops
	for {
		if a.shouldQuit {
			return nil
		}
		switch e := a.window.Event().(type) {
		case gioapp.DestroyEvent:
			return e.Err
		case gioapp.ViewEvent:
			a.setViewHandle(e)
		case gioapp.ConfigEvent:
			a.decorations.Maximized = e.Config.Mode == gioapp.Maximized
		case gioapp.FrameEvent:
			gtx := gioapp.NewContext(&ops, e)
			a.processCallbacks()
			if a.shouldQuit {
				return nil
			}
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) Run(f func()) {
	a.mu.Lock()
	a.callbacks = append(a.callbacks, f)
	a.mu.Unlock()
	a.window.Invalidate()
}

func (a *App) ShowQuitDialog() {
	if !a.quitDialogOpen.CompareAndSwap(false, true) {
		return
	}
	defer a.quitDialogOpen.Store(false)
	opts := append(a.dialogOptions(), zenity.Title(a.translator.Text("quit", "Quit", nil)))
	err := zenity.Question(a.translator.Text("quit_message", "Are you sure you want to quit?\nThe current search progress will be lost.", nil), opts...)
	if err == nil {
		a.Stop()
	}
}

// dialogOptions returns the shared zenity options: the dialog is modal and
// attached to the main window, so the OS disables the window while the dialog
// is open, making it inert. On platforms without a usable window handle
// (Wayland), the dialog is still modal but unattached.
func (a *App) dialogOptions() []zenity.Option {
	opts := []zenity.Option{zenity.Modal()}
	if a.attachID != nil {
		opts = append(opts, zenity.Attach(a.attachID))
	}
	return opts
}

// WindowAttachID exposes the platform window handle to the screens package so
// its own native dialogs (e.g. the setup folder browser) can attach to the
// main window as well.
func (a *App) WindowAttachID() any {
	return a.attachID
}

func (a *App) SetActiveScreen(screen Screen) {
	a.activeScreen = screen
	a.Update()
}

func (a *App) SwitchToMainScreen() {
	a.SetActiveScreen(a.mainScreen)
}

func (a *App) processCallbacks() {
	a.mu.Lock()
	cbs := a.callbacks
	a.callbacks = nil
	a.mu.Unlock()

	for _, cb := range cbs {
		cb()
	}
}

func (a *App) layout(gtx layout.Context) {
	// Draw deep slate background
	paint.Fill(gtx.Ops, theme.BgColor)

	// Custom title bar. The close action is intercepted and routed through
	// ShowQuitDialog instead of closing the window directly. ShowQuitDialog
	// blocks on a native dialog, so it must not run on the frame goroutine.
	actions := a.decorations.Update(gtx)
	if actions&system.ActionClose != 0 {
		actions &^= system.ActionClose
		go func() {
			defer logging.HandlePanic()
			a.ShowQuitDialog()
		}()
	}
	if actions != 0 {
		a.window.Perform(actions)
	}

	decoStyle := material.Decorations(a.theme, &a.decorations,
		system.ActionMinimize|system.ActionMaximize|system.ActionClose,
		a.translator.Text("app_name", "Mod Bisect Tool", nil))
	decoStyle.Background = theme.BorderColor
	decoStyle.Foreground = theme.FgColor
	decoStyle.Title.Color = theme.TextMutedColor

	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(decoStyle.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if a.activeScreen != nil {
				return a.activeScreen.Layout(gtx, a.theme)
			}
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
	)
}
