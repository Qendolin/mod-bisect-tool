package pages

import (
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// UndeclaredDepsPage is a full-screen modal shown when double-indeterminate
// results occur and undeclared dependencies were inferred from mod bytecode.
// The user can continue with the inferred dependencies applied, or cancel.
type UndeclaredDepsPage struct {
	*tview.Flex
	app         tui.TUIApp
	statusText  *tview.TextView
	continueBtn *tview.Button
	cancelBtn   *tview.Button
	list        *widgets.ScrollTextView
	deps        []ui.InferredDependency
}

func NewUndeclaredDepsPage(app tui.TUIApp, deps []ui.InferredDependency) *UndeclaredDepsPage {
	p := &UndeclaredDepsPage{
		Flex:       tview.NewFlex().SetDirection(tview.FlexRow),
		app:        app,
		statusText: tview.NewTextView().SetDynamicColors(true),
		deps:       deps,
	}

	p.statusText.SetText("Undeclared Dependencies Detected")

	description := tview.NewTextView().SetWordWrap(true)
	description.SetText("The search was blocked by two mod groups that depend on each other through undeclared dependencies. " +
		"The following dependencies were inferred from the mods' code. Continue to retry the split with these dependencies applied.")
	description.SetBorderPadding(0, 0, 1, 1)

	p.list = widgets.NewScrollTextView()
	p.list.SetDynamicColors(true).SetScrollable(true)
	p.list.SetText(p.buildDepsList())
	p.list.SetBorderPadding(0, 0, 1, 1)

	listFrame := widgets.NewTitleFrame(p.list, "Inferred Dependencies")

	p.continueBtn = tview.NewButton("Continue")
	widgets.DefaultStyleButton(p.continueBtn)
	p.continueBtn.SetSelectedFunc(func() {
		depsCopy := append([]ui.InferredDependency(nil), deps...)
		p.app.Navigation().CloseModal()
		go func() {
			defer logging.HandlePanic()
			p.app.GetBisectionController().ApplyInferredDependencies(depsCopy)
		}()
	})

	p.cancelBtn = tview.NewButton("Cancel")
	widgets.DefaultStyleButton(p.cancelBtn)
	p.cancelBtn.SetSelectedFunc(func() {
		p.app.Navigation().CloseModal()
		go func() {
			defer logging.HandlePanic()
			p.app.GetBisectionController().CancelInferredDependencies()
		}()
	})

	buttons := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(p.cancelBtn, 10, 0, false).
		AddItem(tview.NewBox(), 2, 0, false).
		AddItem(p.continueBtn, 12, 0, true).
		AddItem(tview.NewBox(), 1, 0, false)

	p.AddItem(widgets.NewTitleFrame(description, "Info"), 3, 0, false).
		AddItem(listFrame, 0, 1, false).
		AddItem(widgets.NewHorizontalSeparator(tcell.ColorGray), 1, 0, false).
		AddItem(buttons, 3, 0, true)

	p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			p.app.Navigation().CloseModal()
			go func() {
				defer logging.HandlePanic()
				p.app.GetBisectionController().CancelInferredDependencies()
			}()
			return nil
		}
		return event
	})

	return p
}

// buildDepsList renders the inferred dependencies as a text list, grouped by source.
func (p *UndeclaredDepsPage) buildDepsList() string {
	vm := p.app.GetViewModel()

	// group targets by source preserving order
	type group struct {
		sourceID  string
		targetIDs []string
	}
	var groups []group
	idx := map[string]int{}
	for _, dep := range p.deps {
		if i, ok := idx[dep.SourceID]; ok {
			groups[i].targetIDs = append(groups[i].targetIDs, dep.TargetID)
		} else {
			idx[dep.SourceID] = len(groups)
			groups = append(groups, group{sourceID: dep.SourceID, targetIDs: []string{dep.TargetID}})
		}
	}

	var sb strings.Builder
	for i, g := range groups {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "[yellow::b]%s[-:-:-]\n", tview.Escape(formatModEntry(vm.Mods.Infos, g.sourceID)))
		for _, tgt := range g.targetIDs {
			fmt.Fprintf(&sb, "  -> %s\n", tview.Escape(formatModEntry(vm.Mods.Infos, tgt)))
		}
	}
	return sb.String()
}

// GetActionPrompts returns the key actions for the page.
func (p *UndeclaredDepsPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "Enter", Action: "Continue"},
		{Input: "ESC", Action: "Cancel"},
	}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status.
func (p *UndeclaredDepsPage) GetStatusPrimitive() *tview.TextView { return p.statusText }

// GetFocusablePrimitives implements the Focusable interface.
func (p *UndeclaredDepsPage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{p.list, p.continueBtn, p.cancelBtn}
}

// Update implements the Page interface.
func (p *UndeclaredDepsPage) Update() {}
