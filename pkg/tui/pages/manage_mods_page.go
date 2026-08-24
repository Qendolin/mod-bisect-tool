package pages

import (
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/util"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const PageManageModsID = "manage_mods"

// ManageModsPage allows viewing and changing the state of all mods.
type ManageModsPage struct {
	*tview.Flex
	app tui.TUIApp

	// statuses is the last rendered snapshot of mod statuses (base + staged
	// overrides). It is refreshed from the controller on every redraw.
	statuses map[string]ui.ModStatusViewModel
	// dirty is true once the user has staged a change that has not been
	// committed or discarded yet.
	dirty bool

	modTable          *widgets.ExtendedTable
	forceEnabledList  *tview.TextView
	forceDisabledList *tview.TextView
	statusText        *tview.TextView
}

// NewManageModsPage creates a new page for managing mod states.
func NewManageModsPage(app tui.TUIApp) *ManageModsPage {
	p := &ManageModsPage{
		Flex:              tview.NewFlex(),
		app:               app,
		forceEnabledList:  tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
		forceDisabledList: tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
		statusText:        tview.NewTextView().SetDynamicColors(true),
	}

	headers := []string{"Status", "ID", "Name", "File"}
	p.modTable = widgets.NewExtendedTable(headers, true)
	p.modTable.SetMaxColumnWidths(0, 0, 35, 0) // Set name column max width
	p.modTable.SetSearchColumns(1, 2)          // Search on ID (col 1) and Name (col 2)
	p.modTable.SetBorderPadding(0, 0, 1, 1)

	p.forceDisabledList.SetBorderPadding(0, 0, 1, 1)
	p.forceEnabledList.SetBorderPadding(0, 0, 1, 1)

	p.setupLayout()
	p.SetInputCapture(p.inputHandler())

	p.statusText.SetText("Manage individual mod states.")
	return p
}

func (p *ManageModsPage) setupLayout() {
	mainListFrame := widgets.NewTitleFrame(p.modTable, "All Mods")
	enabledFrame := widgets.NewTitleFrame(p.forceEnabledList, "Force Enabled")
	disabledFrame := widgets.NewTitleFrame(p.forceDisabledList, "Force Disabled")

	sideBar := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(enabledFrame, 0, 1, false).
		AddItem(disabledFrame, 0, 1, false)

	p.AddItem(mainListFrame, 0, 3, true).
		AddItem(nil, 1, 0, false).
		AddItem(sideBar, 0, 1, false)
}

func (p *ManageModsPage) inputHandler() func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {

		// Handle escape first
		if event.Key() == tcell.KeyEscape {
			if !p.dirty {
				p.app.Navigation().GoBack() // No changes, just go back.
				return nil
			}

			// There are changes, show the apply/discard dialog.
			p.app.Dialogs().ShowQuestionDialog(
				"Outstanding Changes",
				"You have unsaved changes. Apply them?",
				"",
				true,
				func() {
					p.commitChanges()
				},
				func() {
					p.app.Navigation().GoBack()
				},
			)
			return nil
		}

		if util.IsTextInput(p.app.GetFocus()) {
			return event
		}

		if p.modTable.HasFocus() {
			// Handle state changes when table is focused
			if p.handleTableInput(event) == nil {
				return nil
			}
		}

		return event
	}
}

// commitChanges applies the staged overrides to the real state manager.
func (p *ManageModsPage) commitChanges() {
	logging.Infof("ManageModsPage: Committing staged changes.")
	go func() {
		defer logging.HandlePanic()
		p.app.GetModStatusController().Commit()

		// Any mods that were transitioned out of a special state will only re-enter
		// the search pool at the start of the next iteration.
		vm := p.app.GetViewModel()
		pendingAdditions := vm.Sets.PendingAddition
		p.app.ExecuteAndDraw(func() {
			p.dirty = false
			if len(pendingAdditions) > 0 {
				p.app.Dialogs().ShowInfoDialog(
					"Pending Changes",
					"Some mods you have changed will only be added to the search pool at the start of the next bisection iteration.",
					tview.Escape(sets.FormatSet(pendingAdditions).String()),
					func() {
						// Navigate back only after the user dismisses this second dialog.
						p.app.Navigation().GoBack()
					},
				)
			} else {
				// If there were no pending additions, navigate back immediately.
				p.app.Navigation().GoBack()
			}
		})
	}()
}

func (p *ManageModsPage) handleTableInput(event *tcell.EventKey) *tcell.EventKey {
	row, _ := p.modTable.GetSelection()
	if row <= 0 { // No selection or header selected
		return event
	}

	cell := p.modTable.GetCell(row, 1)
	if cell == nil {
		return event
	}
	modID := cell.Text
	shift := event.Modifiers()&tcell.ModShift != 0

	switch event.Rune() {
	case 'd', 'D':
		p.toggleOverride(modID, shift, ui.ModOverrideForceDisabled)
		return nil
	case 'e', 'E':
		p.toggleOverride(modID, shift, ui.ModOverrideForceEnabled)
		return nil
	case 'o', 'O':
		p.toggleOverride(modID, shift, ui.ModOverrideOmitted)
		return nil
	case 'f', 'F':
		if p.modTable.HasFocus() && !p.modTable.Search.HasFocus() {
			p.app.SetFocus(p.modTable.Search)
		}
		return nil
	}

	return event
}

// toggleOverride stages an override for a single mod, or (in bulk mode) for
// every user-editable mod. It renders only once after all changes are staged.
func (p *ManageModsPage) toggleOverride(modID string, isBulk bool, target ui.ModStatusOverride) {
	ctrl := p.app.GetModStatusController()

	// Unresolvable mods cannot be force-enabled; they are dealt with on the
	// unresolvable mods screen right after loading.
	forceEnabledUnresolvable := func(status ui.ModStatusViewModel) bool {
		return target == ui.ModOverrideForceEnabled && status.IsUnresolvable
	}

	if !isBulk {
		if status, ok := p.statuses[modID]; ok && forceEnabledUnresolvable(status) {
			p.app.Dialogs().ShowInfoDialog(
				"Cannot Force Enable",
				"This mod has unresolvable dependencies and cannot be force-enabled.\nIt was offered for Ignore/Disable on the unresolvable mods screen after loading.",
				"",
				nil,
			)
			return
		}
	}

	if isBulk {
		// If every mod already has the target state, clear them all instead.
		allHaveTarget := true
		for _, status := range p.statuses {
			if status.IsUserEditable && !forceEnabledUnresolvable(status) && status.Override != target {
				allHaveTarget = false
				break
			}
		}
		goal := target
		if allHaveTarget {
			goal = ui.ModOverrideNone
		}
		for id, status := range p.statuses {
			if status.IsUserEditable && !forceEnabledUnresolvable(status) {
				ctrl.SetOverride(id, goal)
			}
		}
	} else {
		if status, ok := p.statuses[modID]; ok && status.IsUserEditable {
			next := target
			if status.Override == target {
				next = ui.ModOverrideNone
			}
			ctrl.SetOverride(modID, next)
		}
	}

	p.dirty = true
	p.RefreshState()
}

func (p *ManageModsPage) OnPageActivated() {
	// Reset any staging left over from a previous visit.
	p.app.GetModStatusController().Discard()
	p.dirty = false
	p.RefreshState()
}

// RefreshState updates the lists with the current mod states.
func (p *ManageModsPage) RefreshState() {
	vm := p.app.GetViewModel()
	if !vm.IsReady {
		p.statuses = nil
		p.modTable.Clear()
		p.forceEnabledList.SetText("")
		p.forceDisabledList.SetText("")
		return
	}

	p.statuses = p.app.GetModStatusController().GetModStatuses()

	row, _ := p.modTable.GetSelection() // Preserve selection

	allIDs := vm.Mods.All
	tableData := make([][]string, 0, len(allIDs))
	enabledIDs := []string{}
	disabledIDs := []string{}

	var nextTestSet sets.Set
	if vm.CurrentTestPlan.IsPlanned() {
		nextTestSet = vm.CurrentTestPlan.ModIDsToTest
	}

	for _, id := range allIDs {
		status := p.statuses[id]
		mod := status.ModViewModel

		_, isGloballyPending := vm.Sets.PendingAddition[id]

		var statusStr string
		// Priority: Missing > Pending > Forced > Disabled > Omitted > Problem > Unresolvable > In Test > Inactive
		if status.IsMissing { // IsMissing is non-editable
			statusStr = "[black:red:b]MISSING[-:-:-]"
		} else if isGloballyPending {
			statusStr = "[mediumpurple]Pending[-:-:-]"
		} else if status.Override == ui.ModOverrideForceEnabled {
			statusStr = "[green]Forced[-:-:-]"
			enabledIDs = append(enabledIDs, id)
		} else if status.Override == ui.ModOverrideForceDisabled {
			statusStr = "[maroon]Disabled[-:-:-]"
			disabledIDs = append(disabledIDs, id)
		} else if status.Override == ui.ModOverrideOmitted {
			statusStr = "[steelblue]Omitted[-:-:-]"
		} else if status.IsProblematic { // IsProblematic is non-editable
			statusStr = "[red::b]Problem[-:-:-]"
		} else if status.IsUnresolvable { // IsUnresolvable is non-editable
			statusStr = "[darkgoldenrod]Unresolvable[-:-:-]"
		} else if _, ok := nextTestSet[id]; ok {
			statusStr = "[white]In Test[-:-:-]"
		} else {
			statusStr = "[gray]Inactive[-:-:-]"
		}

		name := mod.Name
		if len(name) > 35 {
			name = name[:32] + "..."
		}

		rowData := []string{statusStr, id, name, mod.BaseFilename}
		tableData = append(tableData, rowData)
	}

	p.modTable.SetData(tableData)
	p.forceEnabledList.SetText(strings.Join(enabledIDs, "\n"))
	p.forceDisabledList.SetText(strings.Join(disabledIDs, "\n"))

	if row > 0 && row < p.modTable.GetRowCount() {
		p.modTable.Select(row, 0) // Restore selection
	}
}

// GetActionPrompts returns the key actions for the page.
func (p *ManageModsPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "E", Action: "Force Enable"},
		{Input: "D", Action: "Force Disable"},
		{Input: "O", Action: "Omit"},
		{Input: "Shift+Key", Action: "Toggle All"},
	}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status.
func (p *ManageModsPage) GetStatusPrimitive() *tview.TextView {
	return p.statusText
}

// GetFocusablePrimitives implements the Focusable interface.
func (p *ManageModsPage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{
		p.modTable,
		p.forceEnabledList,
		p.forceDisabledList,
	}
}

// Update implements the Page interface, refreshing the page after a state change.
func (p *ManageModsPage) Update() {
	p.RefreshState()
}
