package screens

import (
	"fmt"
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// HaltScreen is a full-screen view explaining that the search halted because
// the two groups of mods block each other through undeclared dependencies.
type HaltScreen struct {
	app App

	groupA sets.Set
	groupB sets.Set

	groupAList widget.List
	groupBList widget.List

	backClick  widget.Clickable
	undoClick  widget.Clickable
	resetClick widget.Clickable
}

// NewHaltScreen creates a new HaltScreen showing the two conflicting groups.
func NewHaltScreen(app App, groupA, groupB sets.Set) *HaltScreen {
	s := &HaltScreen{app: app, groupA: groupA, groupB: groupB}
	s.groupAList.Axis = layout.Vertical
	s.groupBList.Axis = layout.Vertical
	return s
}

func (s *HaltScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if s.undoClick.Clicked(gtx) {
		go func() {
			defer logging.HandlePanic()
			ok := s.app.ShowQuestionDialog(s.app.Text("undo_last_step", "Undo Last Step", nil), s.app.Text("undo_confirm", "Are you sure you want to undo the last step?", nil), "", true)
			if !ok {
				return
			}
			s.app.GetBisectionController().Undo()
			s.app.Run(func() { s.app.SwitchToMainScreen() })
		}()
	}
	if s.resetClick.Clicked(gtx) {
		go func() {
			defer logging.HandlePanic()
			ok := s.app.ShowQuestionDialog(s.app.Text("reset_search", "Reset Search", nil), s.app.Text("reset_search_confirm", "This will discard all search progress and start over. Continue?", nil), "", false)
			if !ok {
				return
			}
			s.app.GetBisectionController().ResetSearch()
			s.app.Run(func() { s.app.SwitchToMainScreen() })
		}()
	}
	if s.backClick.Clicked(gtx) {
		s.app.SwitchToMainScreen()
	}

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				title := material.H5(th, s.app.Text("search_halted", "Search Halted", nil))
				title.Color = theme.DangerColor
				title.Font.Weight = font.Bold
				return title.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(s.drawSeparator),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body1(th, s.app.Text("halt_description", "The search cannot continue because the two groups below depend on each other through undeclared dependencies: "+
					"at least one mod in each group silently needs a mod from the other group to run. "+
					"This is a very rare and unfortunate situation, and the tool cannot tell which group causes the problem.\n\n"+
					"To proceed, remove or fix one of the involved mods and start a new search.", nil))
				lbl.Color = theme.FgColor
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return s.layoutGroups(gtx, th)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(s.drawSeparator),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.layoutButtons(gtx, th)
			}),
		)
	})
}

func (s *HaltScreen) layoutGroups(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return s.layoutGroupPanel(gtx, th, s.app.Text("group_a", "Group A", nil), s.groupA, &s.groupAList)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(24)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return s.layoutGroupPanel(gtx, th, s.app.Text("group_b", "Group B", nil), s.groupB, &s.groupBList)
		}),
	)
}

func (s *HaltScreen) layoutGroupPanel(gtx layout.Context, th *material.Theme, title string, mods sets.Set, list *widget.List) layout.Dimensions {
	vm := s.app.GetViewModel()
	lines := make([]string, 0, len(mods))
	for _, id := range sets.MakeSlice(mods) {
		lines = append(lines, formatModEntry(vm.Mods.Infos, id))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, fmt.Sprintf("%s (%d)", title, len(mods)))
			lbl.Font.Weight = font.Bold
			lbl.Color = theme.PrimaryColor
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(th, list).Layout(gtx, len(lines), func(gtx layout.Context, index int) layout.Dimensions {
				lbl := material.Body2(th, lines[index])
				lbl.Color = theme.FgColor
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, lbl.Layout)
			})
		}),
	)
}

func (s *HaltScreen) layoutButtons(gtx layout.Context, th *material.Theme) layout.Dimensions {
	undoBtn := material.Button(th, &s.undoClick, s.app.Text("undo_last_step", "Undo Last Step", nil))
	undoBtn.Background = theme.CardBgColor
	undoBtn.Color = theme.FgColor

	resetBtn := material.Button(th, &s.resetClick, s.app.Text("reset_search", "Reset Search", nil))
	resetBtn.Background = theme.CardBgColor
	resetBtn.Color = theme.FgColor

	backBtn := material.Button(th, &s.backClick, s.app.Text("back_to_main", "Back to Main", nil))
	backBtn.Background = theme.PrimaryColor
	backBtn.Color = colorWhite

	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, layout.Spacer{}.Layout),
		layout.Rigid(undoBtn.Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Rigid(resetBtn.Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Rigid(backBtn.Layout),
	)
}

// formatModEntry renders a mod as "Name (ID)" where a friendly name is known,
// falling back to the ID.
func formatModEntry(modsInfo map[string]ui.ModViewModel, id string) string {
	if info, ok := modsInfo[id]; ok && info.Name != "" {
		return fmt.Sprintf("%s (%s)", info.Name, id)
	}
	return id
}

func (s *HaltScreen) drawSeparator(gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, theme.BorderColor, clip.Rect{Max: gtx.Constraints.Min}.Op())
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(1)}}
		}),
	)
}
