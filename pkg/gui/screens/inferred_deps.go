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
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

type inferredSourceGroup struct {
	sourceID  string
	targetIDs []string
}

// InferredDepsScreen is a full-screen view displayed when double indeterminate results occur
// and undeclared dependencies were inferred from mod bytecode.
type InferredDepsScreen struct {
	app    App
	deps   []ui.InferredDependency
	groups []inferredSourceGroup

	list widget.List

	continueClick widget.Clickable
	cancelClick   widget.Clickable
}

// NewInferredDepsScreen creates a new InferredDepsScreen.
func NewInferredDepsScreen(app App, deps []ui.InferredDependency) *InferredDepsScreen {
	s := &InferredDepsScreen{app: app, deps: deps}
	s.list.Axis = layout.Vertical

	// Group targets by source ID preserving order
	var groups []inferredSourceGroup
	groupMap := make(map[string]int)
	for _, dep := range deps {
		if idx, exists := groupMap[dep.SourceID]; exists {
			groups[idx].targetIDs = append(groups[idx].targetIDs, dep.TargetID)
		} else {
			groupMap[dep.SourceID] = len(groups)
			groups = append(groups, inferredSourceGroup{
				sourceID:  dep.SourceID,
				targetIDs: []string{dep.TargetID},
			})
		}
	}
	s.groups = groups
	return s
}

func (s *InferredDepsScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if s.continueClick.Clicked(gtx) {
		go func() {
			defer logging.HandlePanic()
			s.app.GetBisectionController().ApplyInferredDependencies(s.deps)
			s.app.Run(func() { s.app.SwitchToMainScreen() })
		}()
	}
	if s.cancelClick.Clicked(gtx) {
		go func() {
			defer logging.HandlePanic()
			s.app.GetBisectionController().CancelInferredDependencies()
		}()
	}

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				title := material.H5(th, s.app.Text("undeclared_dependencies", "Undeclared Dependencies Detected", nil))
				title.Color = theme.WarningColor
				title.Font.Weight = font.Bold
				return title.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(s.drawSeparator),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body1(th, s.app.Text("undeclared_dependencies_description",
					"The search was blocked by two mod groups that depend on each other through undeclared dependencies. The following dependencies were inferred from the mods' code. Continue to retry the split with these dependencies applied.",
					nil))
				lbl.Color = theme.FgColor
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return s.layoutDepsList(gtx, th)
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

func (s *InferredDepsScreen) layoutDepsList(gtx layout.Context, th *material.Theme) layout.Dimensions {
	vm := s.app.GetViewModel()
	return material.List(th, &s.list).Layout(gtx, len(s.groups), func(gtx layout.Context, index int) layout.Dimensions {
		group := s.groups[index]
		srcName := formatModEntry(vm.Mods.Infos, group.sourceID)

		return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body1(th, srcName)
					lbl.Color = theme.PrimaryColor
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				}),
			}
			for _, targetID := range group.targetIDs {
				tgtName := formatModEntry(vm.Mods.Infos, targetID)
				text := fmt.Sprintf("  ➔  %s", tgtName)
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(th, text)
					lbl.Color = theme.FgColor
					return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(12)}.Layout(gtx, lbl.Layout)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

func (s *InferredDepsScreen) layoutButtons(gtx layout.Context, th *material.Theme) layout.Dimensions {
	cancelBtn := material.Button(th, &s.cancelClick, s.app.Text("cancel", "Cancel", nil))
	cancelBtn.Background = theme.CardBgColor
	cancelBtn.Color = theme.FgColor

	continueBtn := material.Button(th, &s.continueClick, s.app.Text("continue", "Continue", nil))
	continueBtn.Background = theme.PrimaryColor
	continueBtn.Color = colorWhite

	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, layout.Spacer{}.Layout),
		layout.Rigid(cancelBtn.Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Rigid(continueBtn.Layout),
	)
}

func (s *InferredDepsScreen) drawSeparator(gtx layout.Context) layout.Dimensions {
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
