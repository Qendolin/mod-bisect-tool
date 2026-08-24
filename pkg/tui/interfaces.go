package tui

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/rivo/tview"
)

// TUIApp defines methods the TUI components need to access from the TUI app struct.
type TUIApp interface {
	ui.AppController // Embed the core controller

	// --- UI methods & Managers ---
	ExecuteAndDraw(f func())
	Stop()
	Navigation() *NavigationManager
	Dialogs() *DialogManager
	Layout() *LayoutManager
	GetLogger() *logging.Logger
	GetFocus() tview.Primitive
	SetFocus(tview.Primitive)
}

// Focusable is an interface for any primitive that contains child elements
// which can be focused. It's used by the FocusManager to build a dynamic focus chain.
type Focusable interface {
	// GetFocusablePrimitives returns a slice of the immediate child primitives
	// that can receive focus.
	GetFocusablePrimitives() []tview.Primitive
}
