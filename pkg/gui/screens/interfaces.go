package screens

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/i18n"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// App defines the interface that screens use to communicate with the GUI App.
type App interface {
	ui.AppController

	Run(func())

	SwitchToMainScreen()

	ShowQuitDialog()
	ShowErrorDialog(title, message string, err error)
	ShowInfoDialog(title, message, details string)
	ShowQuestionDialog(title, message, details string, initial bool) (ok bool)
	ShowCrashAssistantDialog() bool

	// WindowAttachID returns the platform window handle for the main window,
	// used to attach native dialogs to it. May be nil on platforms without a
	// usable handle (e.g. Wayland).
	WindowAttachID() any
	Translator() *i18n.Translator
	Text(id, fallback string, data map[string]any) string
	SetLocale(locale string)
}
