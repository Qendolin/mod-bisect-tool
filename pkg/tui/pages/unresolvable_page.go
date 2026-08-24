package pages

import (
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/util"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PageUnresolvableID is the unique identifier for the UnresolvablePage.
const PageUnresolvableID = "unresolvable_page"

// UnresolvablePage shows the mods that could not be activated because of
// unresolvable dependencies and lets the user pick, per mod, whether to ignore
// the failing dependencies (keeping the mod active) or disable it. It is shown
// right after loading, before the search can start.
type UnresolvablePage struct {
	*tview.Flex
	app        tui.TUIApp
	statusText *tview.TextView
	list       *widgets.FlexList
	continueBt *tview.Button

	mods []ui.UnresolvableModInfo
	// decisions maps mod ID to whether its dependencies should be ignored.
	decisions map[string]bool
	// stateButtons holds each entry's state button so it can be updated without
	// rebuilding the whole list.
	stateButtons map[string]*tview.Button
	selected     int
}

func NewUnresolvablePage(app tui.TUIApp, mods []ui.UnresolvableModInfo) *UnresolvablePage {
	p := &UnresolvablePage{
		Flex:         tview.NewFlex().SetDirection(tview.FlexRow),
		app:          app,
		statusText:   tview.NewTextView().SetDynamicColors(true),
		mods:         mods,
		decisions:    make(map[string]bool, len(mods)),
		stateButtons: make(map[string]*tview.Button, len(mods)),
	}

	p.list = widgets.NewFlexList()
	p.list.SetSelectionColor(tcell.ColorNone) // No row highlight; the state button shows the selection.
	p.list.SetChangedFunc(func(index int) {
		p.selected = index
		p.updateAllStates()
	})
	p.list.SetBorderPadding(0, 0, 1, 1)
	p.rebuildList()
	// Focus the first entry by default.
	p.setSelected(0)

	p.continueBt = tview.NewButton("Continue")
	widgets.DefaultStyleButton(p.continueBt)
	p.continueBt.SetSelectedFunc(p.continueAction)

	listFrame := widgets.NewTitleFrame(p.list, "Unresolvable Mods")
	buttons := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(p.continueBt, 16, 0, true).
		AddItem(nil, 1, 0, false)

	p.AddItem(listFrame, 0, 1, true).
		AddItem(widgets.NewHorizontalSeparator(tcell.ColorGray), 1, 0, false).
		AddItem(buttons, 3, 0, false)

	p.SetInputCapture(p.inputHandler())

	p.statusText.SetText("Some mods could not be enabled because their dependencies could not be resolved. Choose what to do with each one.")
	return p
}

// rebuildList clears and re-creates every entry in the list.
func (p *UnresolvablePage) rebuildList() {
	p.list.Clear()
	p.stateButtons = make(map[string]*tview.Button, len(p.mods))
	for _, m := range p.mods {
		p.list.AddItem(p.buildEntry(m), p.entryHeight(m), 0, false)
	}
}

// entryHeight is the number of terminal rows an entry occupies.
func (p *UnresolvablePage) entryHeight(m ui.UnresolvableModInfo) int {
	return 2 + len(m.DepsDisplay)
}

// buildEntry renders a single mod: name/id and the failing dependencies on the
// left, and a state button (Ignore/Disable) on the right. The button toggles
// the decision when clicked.
func (p *UnresolvablePage) buildEntry(m ui.UnresolvableModInfo) tview.Primitive {
	name := m.Mod.Name
	if name == "" {
		name = m.Mod.ID
	}

	lines := []string{
		fmt.Sprintf("[yellow::b]%s[-:-:-] [gray](%s)[-:-:-]", tview.Escape(name), tview.Escape(m.Mod.ID)),
		"Unresolvable:",
	}
	for _, dep := range m.DepsDisplay {
		lines = append(lines, "  - "+tview.Escape(dep))
	}

	left := tview.NewTextView().SetDynamicColors(true).SetWordWrap(false).SetScrollable(false)
	left.SetBackgroundColor(tcell.ColorNone) // Let the selection highlight show through.
	left.SetText(strings.Join(lines, "\n"))

	state := tview.NewButton("Disable")
	state.SetSelectedFunc(func() {
		p.setSelected(p.indexForMod(m.Mod.ID))
		p.toggleMod(m.Mod.ID)
	})
	p.stateButtons[m.Mod.ID] = state
	p.updateState(m.Mod.ID)

	// The button is fixed at 3 rows, anchored to the top of the entry.
	right := tview.NewFlex().SetDirection(tview.FlexRow)
	right.AddItem(state, 3, 0, false)
	right.AddItem(nil, 0, 1, false)

	entry := tview.NewFlex().SetDirection(tview.FlexColumn)
	entry.AddItem(left, 0, 1, false)
	entry.AddItem(right, 12, 0, false)
	return entry
}

// updateState refreshes the decision button of a mod's entry. The colored
// background is only shown for the currently selected row, so the button
// appearance follows the selection rather than whichever button was last clicked.
func (p *UnresolvablePage) updateState(modID string) {
	btn := p.stateButtons[modID]
	if btn == nil {
		return
	}
	decision := p.decisions[modID]

	label := "Disable"
	if decision {
		label = "Ignore"
	}
	btn.SetLabel(label)

	var style tcell.Style
	if p.indexForMod(modID) == p.selected {
		bg := tcell.ColorRed
		if decision {
			bg = tcell.ColorGreen
		}
		style = tcell.StyleDefault.Background(bg).Foreground(tcell.ColorBlack)
	} else {
		fg := tcell.ColorRed
		if decision {
			fg = tcell.ColorGreen
		}
		style = tcell.StyleDefault.Foreground(fg)
	}
	btn.SetStyle(style)
	btn.SetActivatedStyle(style)
}

// updateAllStates refreshes every entry's decision button.
func (p *UnresolvablePage) updateAllStates() {
	for _, m := range p.mods {
		p.updateState(m.Mod.ID)
	}
}

// indexForMod returns the list index of the given mod, or -1 if unknown.
func (p *UnresolvablePage) indexForMod(modID string) int {
	for i, m := range p.mods {
		if m.Mod.ID == modID {
			return i
		}
	}
	return -1
}

// setSelected moves the selection to the given index.
func (p *UnresolvablePage) setSelected(index int) {
	if len(p.mods) == 0 {
		p.selected = -1
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(p.mods) {
		index = len(p.mods) - 1
	}
	p.selected = index
	p.list.SetCurrentItem(index)
	p.updateAllStates()
}

// toggleMod flips the decision of the given mod.
func (p *UnresolvablePage) toggleMod(modID string) {
	p.decisions[modID] = !p.decisions[modID]
	p.updateState(modID)
}

// toggleSelected flips the decision of the currently selected mod.
func (p *UnresolvablePage) toggleSelected() {
	if p.selected < 0 || p.selected >= len(p.mods) {
		return
	}
	p.toggleMod(p.mods[p.selected].Mod.ID)
}

// inputHandler reacts to navigation, toggling the decision on the selected row,
// and to C continuing to the next step.
func (p *UnresolvablePage) inputHandler() func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			p.setSelected(p.selected - 1)
			return nil
		case tcell.KeyDown:
			p.setSelected(p.selected + 1)
			return nil
		case tcell.KeyHome:
			p.setSelected(0)
			return nil
		case tcell.KeyEnd:
			p.setSelected(len(p.mods) - 1)
			return nil
		case tcell.KeyEnter:
			// Let the Continue button handle Enter when it is focused.
			if p.app.GetFocus() == p.continueBt {
				return event
			}
			p.toggleSelected()
			return nil
		}

		if util.IsTextInput(p.app.GetFocus()) {
			return event
		}

		switch event.Rune() {
		case ' ':
			p.toggleSelected()
			return nil
		}
		return event
	}
}

// continueAction closes the page, applies the per-mod decisions, and completes
// loading.
func (p *UnresolvablePage) continueAction() {
	decisions := make(map[string]ui.UnresolvableModAction)
	for _, m := range p.mods {
		if p.decisions[m.Mod.ID] {
			decisions[m.Mod.ID] = ui.UnresolvableModActionIgnore
		}
	}
	p.app.Navigation().CloseModal()
	go func() {
		defer logging.HandlePanic()
		p.app.GetModStatusController().ResolveUnresolvableMods(decisions)
		p.app.CompleteLoading()
	}()
}

// GetActionPrompts returns the key actions for the page.
func (p *UnresolvablePage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "Enter/Space", Action: "Toggle Decision"},
	}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status.
func (p *UnresolvablePage) GetStatusPrimitive() *tview.TextView { return p.statusText }

// GetFocusablePrimitives implements the Focusable interface.
func (p *UnresolvablePage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{p.list, p.continueBt}
}

// Update implements the Page interface.
func (p *UnresolvablePage) Update() {}
