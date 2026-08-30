package pages

import (
	"fmt"
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

type SetupExcludedModsPage struct {
	*tview.Flex
	app                                   tui.TUIApp
	statusText                            *tview.TextView
	initial, additional                   []string
	keep, omit                            map[string]bool
	initialTable                          *widgets.ExtendedTable
	omitTable                             *widgets.ExtendedTable
	allButton, noneButton, continueButton *tview.Button
}

func NewSetupExcludedModsPage(app tui.TUIApp, initiallyDisabled []string) *SetupExcludedModsPage {
	p := &SetupExcludedModsPage{
		Flex:       tview.NewFlex().SetDirection(tview.FlexRow),
		app:        app,
		statusText: tview.NewTextView().SetDynamicColors(true),
		initial:    append([]string(nil), initiallyDisabled...),
		keep:       map[string]bool{},
		omit:       map[string]bool{},
	}
	initialSet := map[string]struct{}{}
	for _, id := range p.initial {
		initialSet[id] = struct{}{}
		p.keep[id] = true
	}
	statuses := app.GetModStatusController().GetModStatuses()
	for _, id := range app.GetViewModel().Mods.All {
		if _, ok := initialSet[id]; ok || statuses[id].Override == ui.ModOverrideForceEnabled {
			continue
		}
		p.additional = append(p.additional, id)
	}
	p.initialTable = widgets.NewExtendedTable([]string{"", "Name", "ID"}, false)
	p.initialTable.SetSearchColumns(1, 2)
	p.initialTable.SetMaxColumnWidths(0, 50, 0)

	p.omitTable = widgets.NewExtendedTable([]string{"", "Name", "ID"}, true)
	p.omitTable.SetSearchColumns(1, 2)
	p.omitTable.SetMaxColumnWidths(0, 50, 0)

	p.allButton = tview.NewButton("All")
	p.noneButton = tview.NewButton("None")
	p.continueButton = tview.NewButton("Continue")

	widgets.DefaultStyleButton(p.allButton)
	widgets.DefaultStyleButton(p.noneButton)
	widgets.DefaultStyleButton(p.continueButton)

	p.allButton.SetSelectedFunc(func() {
		for _, id := range p.initial {
			p.keep[id] = true
		}
		p.refresh()
	})
	p.noneButton.SetSelectedFunc(func() {
		for _, id := range p.initial {
			p.keep[id] = false
		}
		p.refresh()
	})
	p.continueButton.SetSelectedFunc(p.continueAction)

	p.refresh()

	description := tview.NewTextView().SetWordWrap(true).SetText(
		"Omit other mods from the search if you already know they are good, bad, or distracting during testing.")
	description.SetBorderPadding(0, 0, 1, 1)

	p.AddItem(widgets.NewTitleFrame(description, "Info"), 3, 0, false)

	if len(p.initial) > 0 {
		keepDescription := tview.NewTextView().SetWordWrap(true).SetText(
			"Keep already disabled mods disabled.")
		keepDescription.SetBorderPadding(0, 0, 1, 1)

		keepContent := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(keepDescription, 2, 0, false).
			AddItem(p.initialTable, 0, 1, true)

		frame := widgets.NewTitleFrame(keepContent, "Keep Disabled")
		frame.AddTitleItem(nil, 0, 1, false).
			AddTitleItem(p.allButton, 8, 0, false).
			AddTitleItem(nil, 1, 0, false).
			AddTitleItem(p.noneButton, 8, 0, false)

		p.AddItem(frame, 0, 1, true)
	}

	p.AddItem(widgets.NewTitleFrame(p.omitTable, "Omit Mods"), 0, 2, len(initialSet) == 0).
		AddItem(widgets.NewHorizontalSeparator(tcell.ColorGray), 1, 0, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(p.continueButton, 12, 0, true).
			AddItem(nil, 1, 0, false),
			3, 0, false)

	p.SetInputCapture(p.inputHandler())
	p.statusText.SetText("Configure Search")

	return p
}

func (p *SetupExcludedModsPage) KeepDisabled(id string) {
	if _, initial := p.keep[id]; initial {
		p.keep[id] = true
	} else {
		p.omit[id] = true
	}
	p.refresh()
}

func makeCheckmark(checked bool) string {
	if checked {
		return "[[green]x[-]]"
	}
	return "[ ]"
}

func (p *SetupExcludedModsPage) refresh() {
	vm := p.app.GetViewModel()

	initialRows := make([][]string, 0, len(p.initial))
	for _, id := range p.initial {
		info := vm.Mods.Infos[id]
		initialRows = append(initialRows, []string{makeCheckmark(p.keep[id]), info.Name, fmt.Sprintf("[gray]%s[-]", id)})
	}
	p.initialTable.SetData(initialRows)
	p.initialTable.SetClickedHandler(func(row, column int) bool {
		if column != 0 {
			return false
		}
		return p.toggleEntry(p.keep, p.initialTable, row)
	})

	omitRows := make([][]string, 0, len(p.additional))
	for _, id := range p.additional {
		info := vm.Mods.Infos[id]
		omitRows = append(omitRows, []string{makeCheckmark(p.omit[id]), info.Name, fmt.Sprintf("[gray]%s[-]", id)})
	}
	p.omitTable.SetData(omitRows)
	p.omitTable.SetClickedHandler(func(row, column int) bool {
		if column != 0 {
			return false
		}
		return p.toggleEntry(p.omit, p.omitTable, row)
	})
}

func (p *SetupExcludedModsPage) toggleEntry(state map[string]bool, table *widgets.ExtendedTable, row int) bool {
	id := p.selectedTableID(table, row)
	if id == "" {
		return false
	}
	state[id] = !state[id]

	if entry := table.GetData(row-1, 0); entry != nil {
		*entry = makeCheckmark(state[id])
	}
	table.GetCell(row, 0).SetText(makeCheckmark(state[id]))
	return true
}

func (p *SetupExcludedModsPage) selectedTableID(table *widgets.ExtendedTable, row int) string {
	if row <= 0 || table == nil {
		return ""
	}
	cell := table.GetCell(row, 2)
	if cell == nil || cell.Text == "" {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSuffix(cell.Text, "[-]"), "[gray]")
}

func (p *SetupExcludedModsPage) inputHandler() func(*tcell.EventKey) *tcell.EventKey {
	return func(e *tcell.EventKey) *tcell.EventKey {
		if util.IsTextInput(p.app.GetFocus()) {
			return e
		}

		switch e.Rune() {
		case 'a', 'A':
			for _, id := range p.initial {
				p.keep[id] = true
			}
			p.refresh()
			return nil
		case 'n', 'N':
			for _, id := range p.initial {
				p.keep[id] = false
			}
			p.refresh()
			return nil
		}

		if e.Rune() == ' ' || e.Key() == tcell.KeyEnter {
			if p.initialTable.HasFocus() {
				row, _ := p.initialTable.GetSelection()
				if !p.toggleEntry(p.keep, p.initialTable, row) {
					return e
				}
				return nil
			}
			if p.omitTable.Table.HasFocus() {
				row, _ := p.omitTable.GetSelection()
				if !p.toggleEntry(p.omit, p.omitTable, row) {
					return e
				}
				return nil
			}
		}
		return e
	}
}

func (p *SetupExcludedModsPage) continueAction() {
	keep, omit := sets.Set{}, sets.Set{}
	for _, id := range p.initial {
		if p.keep[id] {
			keep[id] = struct{}{}
		}
	}
	for _, id := range p.additional {
		if p.omit[id] {
			omit[id] = struct{}{}
		}
	}
	p.app.Navigation().CloseModal()
	go func() {
		defer logging.HandlePanic()
		p.app.CompleteInitialModState(keep, omit)
	}()
}

func (p *SetupExcludedModsPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "Space/Enter", Action: "Toggle"},
		{Input: "A/N", Action: "All/None"},
	}
}

func (p *SetupExcludedModsPage) GetStatusPrimitive() *tview.TextView { return p.statusText }

func (p *SetupExcludedModsPage) GetFocusablePrimitives() []tview.Primitive {
	if len(p.initial) > 0 {
		return []tview.Primitive{p.initialTable, p.omitTable, p.continueButton, p.allButton, p.noneButton}
	}
	return []tview.Primitive{p.omitTable, p.continueButton}
}

func (p *SetupExcludedModsPage) Update() {}
