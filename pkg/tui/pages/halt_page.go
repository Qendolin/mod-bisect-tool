package pages

import (
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// HaltPage is a full-screen modal that explains why the search halted: the two
// groups of mods block each other through undeclared dependencies.
type HaltPage struct {
	*tview.Flex
	app tui.TUIApp

	groupAText *tview.TextView
	groupBText *tview.TextView
	statusText *tview.TextView

	undoBtn  *tview.Button
	resetBtn *tview.Button
	closeBtn *tview.Button

	onClose func()
}

// NewHaltPage creates a new HaltPage. onClose is invoked (from the event loop)
// when the user dismisses the page.
func NewHaltPage(app tui.TUIApp, groupA, groupB sets.Set, onClose func()) *HaltPage {
	vm := app.GetViewModel()

	p := &HaltPage{
		Flex:       tview.NewFlex(),
		app:        app,
		groupAText: tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
		groupBText: tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
		statusText: tview.NewTextView().SetDynamicColors(true),
		onClose:    onClose,
	}

	p.statusText.SetText("Search Halted")
	p.groupAText.SetText(formatModList(vm.Mods.Infos, groupA))
	p.groupBText.SetText(formatModList(vm.Mods.Infos, groupB))

	explainer := widgets.NewScrollTextView()
	explainer.SetDynamicColors(true)
	explainer.SetWordWrap(true)
	explainer.SetScrollable(true)
	explainer.SetText("The search stopped because the two groups of mods below block each other through undeclared dependencies: " +
		"each group contains a mod that silently needs a mod from the other group. " +
		"This is a rare but unfortunate situation.\n\n" +
		"To continue, remove or fix one of the involved mods and start a new search.")

	groupAFrame := widgets.NewTitleFrame(p.groupAText, fmt.Sprintf("Group A (%d)", len(groupA)))
	groupBFrame := widgets.NewTitleFrame(p.groupBText, fmt.Sprintf("Group B (%d)", len(groupB)))

	groupsFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(groupAFrame, 0, 1, false).
		AddItem(tview.NewBox(), 2, 0, false).
		AddItem(groupBFrame, 0, 1, false)

	undoAction := func() {
		p.app.Dialogs().ShowQuestionDialog("Confirmation", "Are you sure you want to undo the last step?", "", true, func() {
			p.onClose()
			go func() {
				defer logging.HandlePanic()
				p.app.GetBisectionController().Undo()
			}()
		}, nil)
	}
	resetAction := func() {
		p.app.Dialogs().ShowQuestionDialog("Confirmation", "This will discard all search progress and start over. Continue?", "", false, func() {
			p.onClose()
			go func() {
				defer logging.HandlePanic()
				p.app.GetBisectionController().ResetSearch()
			}()
		}, nil)
	}

	p.undoBtn = tview.NewButton("Undo Last Step").SetSelectedFunc(undoAction)
	widgets.DefaultStyleButton(p.undoBtn)

	p.resetBtn = tview.NewButton("Reset Search").SetSelectedFunc(resetAction)
	widgets.DefaultStyleButton(p.resetBtn)

	p.closeBtn = tview.NewButton("Close").
		SetSelectedFunc(p.onClose)
	widgets.DefaultStyleButton(p.closeBtn)

	buttonFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(p.undoBtn, 0, 1, false).
		AddItem(tview.NewBox(), 2, 0, false).
		AddItem(p.resetBtn, 0, 1, false).
		AddItem(tview.NewBox(), 2, 0, false).
		AddItem(p.closeBtn, 0, 1, true).
		AddItem(tview.NewBox(), 0, 1, false)

	p.SetDirection(tview.FlexRow).
		AddItem(widgets.NewHorizontalSeparator(tcell.ColorWhite), 1, 0, false).
		AddItem(explainer, 4, 0, false).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(groupsFlex, 0, 1, false).
		AddItem(widgets.NewHorizontalSeparator(tcell.ColorWhite), 1, 0, false).
		AddItem(buttonFlex, 3, 0, true).
		AddItem(tview.NewBox(), 1, 0, false)

	p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape:
			p.onClose()
			return nil
		case event.Rune() == 'u' || event.Rune() == 'U':
			undoAction()
			return nil
		case event.Rune() == 'r' || event.Rune() == 'R':
			resetAction()
			return nil
		}
		return event
	})

	return p
}

// formatModList renders a set of mod IDs as a newline-separated list, each entry
// as "Name (ID)" where a friendly name is known and falling back to the ID.
func formatModList(modsInfo map[string]ui.ModViewModel, mods sets.Set) string {
	ids := sets.MakeSlice(mods)
	lines := make([]string, 0, len(ids))
	for _, id := range ids {
		lines = append(lines, formatModEntry(modsInfo, id))
	}
	return strings.Join(lines, "\n")
}

func formatModEntry(modsInfo map[string]ui.ModViewModel, id string) string {
	if info, ok := modsInfo[id]; ok && info.Name != "" {
		return fmt.Sprintf("%s (%s)", info.Name, id)
	}
	return id
}

// GetActionPrompts returns the key actions for the halt page.
func (p *HaltPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "U", Action: "Undo Last Step"},
		{Input: "R", Action: "Reset Search"},
		{Input: "ESC", Action: "Close"},
	}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status.
func (p *HaltPage) GetStatusPrimitive() *tview.TextView {
	return p.statusText
}

// GetFocusablePrimitives implements the Focusable interface.
func (p *HaltPage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{p.undoBtn, p.resetBtn, p.closeBtn}
}

// Update implements the Page interface.
func (p *HaltPage) Update() {}
