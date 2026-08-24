package tui

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/rivo/tview"
)

// NavigationManager handles page state and transitions using a hybrid model
// of persistent "workspace" pages and transient "modal" overlays.
type NavigationManager struct {
	app             TUIApp
	pages           *tview.Pages // The tview.Pages primitive from the layout
	persistentPages map[string]Page
	history         []string
	modalStack      []string
}

// NewNavigationManager creates a new manager for page navigation.
func NewNavigationManager(app TUIApp, pages *tview.Pages) *NavigationManager {
	return &NavigationManager{
		app:             app,
		pages:           pages,
		persistentPages: make(map[string]Page),
		history:         make([]string, 0),
		modalStack:      make([]string, 0),
	}
}

// Register adds a persistent page to the manager. These pages are created
// once and switched to by their ID.
func (n *NavigationManager) Register(pageID string, page Page) {
	if _, exists := n.persistentPages[pageID]; exists {
		logging.Errorf("NavigationManager: A page with ID '%s' is already registered. It will be replaced.", pageID)
		n.pages.RemovePage(pageID)
	}

	n.persistentPages[pageID] = page
	n.pages.AddPage(pageID, page, true, false) // Add but don't make visible yet
}

// updateUIForPage is a helper to set the footer, header, and focus.
func (n *NavigationManager) updateUIForPage(page Page) {
	if page == nil {
		n.app.Layout().SetFooter(nil)
		n.app.Layout().SetHeader(nil)
		return
	}
	n.app.Layout().SetFooter(page.GetActionPrompts())
	n.app.Layout().SetHeader(page.GetStatusPrimitive())
	n.app.SetFocus(page)
}

// SwitchTo changes the main visible page to the one specified by pageID.
// It also manages the navigation history for the GoBack() function.
func (n *NavigationManager) SwitchTo(pageID string) {
	page, ok := n.persistentPages[pageID]
	if !ok {
		return // Do not switch to an unregistered page
	}

	currentID, _ := n.pages.GetFrontPage()
	// Only add to history if it's a different persistent page
	if currentID != pageID && len(n.modalStack) == 0 {
		if _, isPersistent := n.persistentPages[currentID]; isPersistent {
			n.history = append(n.history, currentID)
		}
	}

	n.pages.SwitchToPage(pageID)
	n.updateUIForPage(n.persistentPages[pageID])

	if activator, ok := page.(PageActivator); ok {
		activator.OnPageActivated()
	}

	n.showTopModal()
}

// GoBack navigates to the previous page in the history stack.
func (n *NavigationManager) GoBack() {
	if len(n.history) == 0 {
		return // Cannot go back
	}

	// Pop the last page from history
	lastPageID := n.history[len(n.history)-1]
	n.history = n.history[:len(n.history)-1]

	n.pages.SwitchToPage(lastPageID)
	lastPage := n.persistentPages[lastPageID]
	n.updateUIForPage(lastPage)

	if activator, ok := lastPage.(PageActivator); ok {
		activator.OnPageActivated()
	}

	n.showTopModal()
}

// ShowModal displays a transient page (like a dialog) over the current view.
func (n *NavigationManager) ShowModal(pageID string, page Page) {
	n.pages.AddPage(pageID, page, true, true)
	n.modalStack = append(n.modalStack, pageID)
	n.updateUIForPage(page)
	n.app.SetFocus(page)

	if activator, ok := page.(PageActivator); ok {
		activator.OnPageActivated()
	}
}

// CloseModal removes the top-most modal page.
func (n *NavigationManager) CloseModal() {
	if len(n.modalStack) == 0 {
		return
	}

	// Pop the modal page
	modalID := n.modalStack[len(n.modalStack)-1]
	n.modalStack = n.modalStack[:len(n.modalStack)-1]
	n.pages.RemovePage(modalID)

	// Update UI for the page that is now in front
	n.showTopModal()
	currentPage := n.GetCurrentPage(true)
	n.updateUIForPage(currentPage)

	// Refresh the revealed page's content. Closing a modal can change the state
	// it displays (e.g. a halted search), so it must re-read the view model.
	if currentPage != nil {
		currentPage.Update()
	}
}

// GetCurrentPage returns the Page interface of the front-most primitive.
func (n *NavigationManager) GetCurrentPage(includeModal bool) Page {
	if includeModal {
		if len(n.modalStack) > 0 {
			modalID := n.modalStack[len(n.modalStack)-1]
			_, primitive := n.pages.GetFrontPage()
			if p, ok := primitive.(Page); ok && modalID != "" {
				return p
			}
		}
	}

	// If no modal, get the persistent page
	pageID, _ := n.pages.GetFrontPage()
	if page, ok := n.persistentPages[pageID]; ok {
		return page
	}

	return nil
}

// GetCurrentPageID returns the page id of the front-most primitive.
func (n *NavigationManager) GetCurrentPageID(includeModal bool) string {
	if includeModal {
		if len(n.modalStack) > 0 {
			modalID := n.modalStack[len(n.modalStack)-1]
			if modalID != "" {
				return modalID
			}
		}
	}

	pageID, _ := n.pages.GetFrontPage()
	return pageID
}

// showTopModal ensures the top-most modal page is visible and focused.
func (n *NavigationManager) showTopModal() {
	if len(n.modalStack) == 0 {
		return
	}

	modalID := n.modalStack[len(n.modalStack)-1]
	n.pages.ShowPage(modalID)

	_, primitive := n.pages.GetFrontPage()
	if p, ok := primitive.(Page); ok {
		n.app.SetFocus(p)
		n.updateUIForPage(p)
	}
}
