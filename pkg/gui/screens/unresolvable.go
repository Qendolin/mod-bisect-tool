package screens

import (
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// UnresolvableScreen lists the mods that could not be activated because of
// unresolvable dependencies and lets the user pick, per mod, whether to ignore
// the failing dependencies (keeping the mod active) or disable it. It is shown
// right after loading, before the search can start.
type UnresolvableScreen struct {
	app App

	mods []ui.UnresolvableModInfo

	continueClick widget.Clickable
	ignoreClicks  map[string]*widget.Clickable
	disableClicks map[string]*widget.Clickable
	// decisions maps mod ID to whether its dependencies should be ignored.
	decisions map[string]bool

	list widget.List
}

func NewUnresolvableScreen(app App, mods []ui.UnresolvableModInfo) *UnresolvableScreen {
	s := &UnresolvableScreen{
		app:           app,
		mods:          mods,
		ignoreClicks:  make(map[string]*widget.Clickable, len(mods)),
		disableClicks: make(map[string]*widget.Clickable, len(mods)),
		decisions:     make(map[string]bool, len(mods)),
	}
	for _, m := range mods {
		s.ignoreClicks[m.Mod.ID] = &widget.Clickable{}
		s.disableClicks[m.Mod.ID] = &widget.Clickable{}
	}
	s.list.Axis = layout.Vertical
	return s
}

func (s *UnresolvableScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Process clicks.
	for _, m := range s.mods {
		id := m.Mod.ID
		if s.ignoreClicks[id].Clicked(gtx) {
			s.decisions[id] = true
		}
		if s.disableClicks[id].Clicked(gtx) {
			s.decisions[id] = false
		}
	}
	if s.continueClick.Clicked(gtx) {
		decisions := make(map[string]ui.UnresolvableModAction)
		for _, m := range s.mods {
			if s.decisions[m.Mod.ID] {
				decisions[m.Mod.ID] = ui.UnresolvableModActionIgnore
			}
		}
		go func() {
			defer logging.HandlePanic()
			s.app.GetModStatusController().ResolveUnresolvableMods(decisions)
			s.app.CompleteLoading()
		}()
	}

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.H6(th, s.app.Text("unresolvable_mods", "Unresolvable Mods", nil))
				lbl.Color = theme.PrimaryColor
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, s.app.Text("unresolvable_description", "Some mods could not be enabled because their dependencies could not be resolved.\nFor each mod, choose whether to ignore the failing dependencies (keeping it enabled) or disable it.", nil))
				lbl.Color = theme.FgColor
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return material.List(th, &s.list).Layout(gtx, len(s.mods), func(gtx layout.Context, i int) layout.Dimensions {
					return s.layoutModEntry(gtx, th, &s.mods[i])
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, layout.Spacer{}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, &s.continueClick, s.app.Text("continue", "Continue", nil))
						btn.Background = theme.PrimaryColor
						btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						return btn.Layout(gtx)
					}),
				)
			}),
		)
	})
}

// layoutModEntry renders a single mod: name/id and the failing dependencies on
// the left and the Ignore/Disable buttons anchored to the top on the right.
func (s *UnresolvableScreen) layoutModEntry(gtx layout.Context, th *material.Theme, m *ui.UnresolvableModInfo) layout.Dimensions {
	ignore := s.decisions[m.Mod.ID]
	name := m.Mod.Name
	if name == "" {
		name = m.Mod.ID
	}
	return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body1(th, name)
								lbl.Color = theme.FgColor
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th, "  "+m.Mod.ID)
								lbl.Color = theme.TextMutedColor
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(th, s.app.Text("unresolvable", "Unresolvable:", nil))
						lbl.Color = theme.WarningColor
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						var depChildren []layout.FlexChild
						for _, dep := range m.DepsDisplay {
							dep := dep
							depChildren = append(depChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th, "  - "+dep)
								lbl.Color = theme.WarningColor
								lbl.MaxLines = 1 // Truncate instead of wrapping.
								return lbl.Layout(gtx)
							}))
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx, depChildren...)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, s.ignoreClicks[m.Mod.ID], s.app.Text("ignore", "Ignore", nil))
						if ignore {
							btn.Background = theme.PrimaryColor
							btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						} else {
							btn.Background = theme.CardBgColor
							btn.Color = theme.FgColor
						}
						btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}
						btn.TextSize = unit.Sp(12)
						return btn.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, s.disableClicks[m.Mod.ID], s.app.Text("disable", "Disable", nil))
						if !ignore {
							btn.Background = theme.DangerColor
							btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						} else {
							btn.Background = theme.CardBgColor
							btn.Color = theme.FgColor
						}
						btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}
						btn.TextSize = unit.Sp(12)
						return btn.Layout(gtx)
					}),
				)
			}),
		)
	})
}
