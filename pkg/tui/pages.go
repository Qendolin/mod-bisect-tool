package tui

import "github.com/rivo/tview"

// Page IDs are constants used by the NavigationManager to identify pages.
// They are defined here in the high-level 'ui' package.
const (
	PageSetupID           = "setup_page"
	PageMainID            = "main_page"
	PageLogID             = "log_page"
	PageLoadingID         = "loading_page"
	PageManageModsID      = "manage_mods"
	PageHistoryID         = "history_page"
	PageResultID          = "result_page"
	PageTestID            = "test_page"
	PageUnresolvableID    = "unresolvable_page"
	PageHaltID            = "halt_page"
	PageUndeclaredDepsID  = "undeclared_deps_page"
)

type ActionPrompt struct {
	Input  string
	Action string
}

// Page is the interface that all UI pages must implement.
type Page interface {
	tview.Primitive
	GetActionPrompts() []ActionPrompt
	GetStatusPrimitive() *tview.TextView
	Update()
}

// PageActivator defines an interface for pages that need to perform an action
// when they become the active page.
type PageActivator interface {
	OnPageActivated()
}
