package screens

import (
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	exwidgets "github.com/Qendolin/mod-bisect-tool/pkg/gui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

type InitialModStateScreen struct {
	app                         App
	vm                          ui.BisectionViewModel
	initial, additional         []string
	keep, omit                  map[string]*widget.Bool
	keepClick, omitClick        map[string]*widget.Clickable
	keepAllClick, keepNoneClick widget.Clickable
	omitAllClick, omitNoneClick widget.Clickable
	continueClick               widget.Clickable
	initialList, omitList       widget.List
	searchEditor                widget.Editor
}

func NewInitialModStateScreen(app App, initiallyDisabled []string) *InitialModStateScreen {
	s := &InitialModStateScreen{
		app:       app,
		vm:        app.GetViewModel(),
		initial:   append([]string(nil), initiallyDisabled...),
		keep:      map[string]*widget.Bool{},
		omit:      map[string]*widget.Bool{},
		keepClick: map[string]*widget.Clickable{},
		omitClick: map[string]*widget.Clickable{},
	}
	initialSet := map[string]struct{}{}
	for _, id := range s.initial {
		initialSet[id] = struct{}{}
		s.keep[id] = &widget.Bool{Value: true}
		s.keepClick[id] = &widget.Clickable{}
	}
	statuses := app.GetModStatusController().GetModStatuses()
	for _, id := range s.vm.Mods.All {
		if _, ok := initialSet[id]; ok || statuses[id].Override == ui.ModOverrideForceEnabled {
			continue
		}
		s.additional = append(s.additional, id)
		s.omit[id] = &widget.Bool{}
		s.omitClick[id] = &widget.Clickable{}
	}
	s.initialList.Axis = layout.Vertical
	s.omitList.Axis = layout.Vertical
	return s
}

func (s *InitialModStateScreen) KeepDisabled(id string) {
	if _, initially := s.keep[id]; initially {
		s.keep[id].Value = true
	} else if state, ok := s.omit[id]; ok {
		state.Value = true
	}
}

func (s *InitialModStateScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	for _, id := range s.initial {
		if s.keepClick[id].Clicked(gtx) {
			s.keep[id].Value = !s.keep[id].Value
		}
	}
	for _, id := range s.additional {
		if s.omitClick[id].Clicked(gtx) {
			s.omit[id].Value = !s.omit[id].Value
		}
	}
	if s.keepAllClick.Clicked(gtx) {
		for _, id := range s.initial {
			s.keep[id].Value = true
		}
	}
	if s.keepNoneClick.Clicked(gtx) {
		for _, id := range s.initial {
			s.keep[id].Value = false
		}
	}
	if s.omitAllClick.Clicked(gtx) {
		for _, id := range s.additional {
			s.omit[id].Value = true
		}
	}
	if s.omitNoneClick.Clicked(gtx) {
		for _, id := range s.additional {
			s.omit[id].Value = false
		}
	}
	if s.continueClick.Clicked(gtx) {
		keep, omit := sets.Set{}, sets.Set{}
		for _, id := range s.initial {
			if s.keep[id].Value {
				keep[id] = struct{}{}
			}
		}
		for _, id := range s.additional {
			if s.omit[id].Value {
				omit[id] = struct{}{}
			}
		}
		go func() { defer logging.HandlePanic(); s.app.CompleteInitialModState(keep, omit) }()
	}
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.H6(th, s.app.Text("initial_mod_state", "Mod State Before Search", nil))
				l.Color = theme.PrimaryColor
				l.Font.Weight = font.Bold
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(th, s.app.Text("initial_mod_state_description", "Keep known-disabled mods disabled, or omit other mods from the search if you already know they are good, bad, or distracting during testing. The omission choices are below.", nil))
				l.Color = theme.FgColor
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
			layout.Flexed(1, s.layoutSections(th)),
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

func (s *InitialModStateScreen) layoutSections(th *material.Theme) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild

		// Keep Mods Disabled section. Only include when there are initial mods.
		if len(s.initial) > 0 {
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return s.layoutFrameHeading(gtx, th, s.app.Text("keep_mods_disabled", "Keep Mods Disabled", nil), &s.keepAllClick, &s.keepNoneClick)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return s.layoutFramedList(gtx, th, &s.initialList, s.initial, s.keep, s.keepClick, "")
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			)
		}

		// Omit Mods from Search section.
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.layoutFrameHeading(gtx, th, s.app.Text("omit_mods_from_search", "Omit Mods from Search", nil), &s.omitAllClick, &s.omitNoneClick)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return s.layoutSearch(gtx, th) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return s.layoutFramedList(gtx, th, &s.omitList, s.filteredAdditional(), s.omit, s.omitClick, "")
			}),
		)

		if len(children) == 0 {
			return layout.Dimensions{}
		}

		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
}

func (s *InitialModStateScreen) layoutFrameHeading(gtx layout.Context, th *material.Theme, title string, allClick, noneClick *widget.Clickable) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(th, title)
			l.Color = theme.FgColor
			l.Font.Weight = font.Bold
			return l.Layout(gtx)
		}),
		layout.Flexed(1, layout.Spacer{}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			all := material.Button(th, allClick, s.app.Text("all", "All", nil))
			all.Background = theme.CardBgColor
			all.Color = theme.FgColor
			all.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(8), Right: unit.Dp(8)}
			all.TextSize = unit.Sp(12)
			none := material.Button(th, noneClick, s.app.Text("none", "None", nil))
			none.Background = theme.CardBgColor
			none.Color = theme.FgColor
			none.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(8), Right: unit.Dp(8)}
			none.TextSize = unit.Sp(12)
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, layout.Rigid(all.Layout), layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout), layout.Rigid(none.Layout))
		}),
	)
}

func (s *InitialModStateScreen) layoutSearch(gtx layout.Context, th *material.Theme) layout.Dimensions {
	ed := material.Editor(th, &s.searchEditor, s.app.Text("search_mods", "Search mods...", nil))
	ed.TextSize = unit.Sp(12)
	ed.Color = theme.FgColor
	ed.HintColor = theme.TextMutedColor
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, ed.Layout)
}

func (s *InitialModStateScreen) filteredAdditional() []string {
	filter := strings.ToLower(s.searchEditor.Text())
	if filter == "" {
		return s.additional
	}
	filtered := make([]string, 0)
	for _, id := range s.additional {
		info := s.vm.Mods.Infos[id]
		if strings.Contains(strings.ToLower(id), filter) || strings.Contains(strings.ToLower(info.Name), filter) {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func (s *InitialModStateScreen) layoutFramedList(gtx layout.Context, th *material.Theme, list *widget.List, ids []string, values map[string]*widget.Bool, clicks map[string]*widget.Clickable, _ string) layout.Dimensions {
	return widget.Border{Color: theme.BorderColor, CornerRadius: unit.Dp(4), Width: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.List(th, list).Layout(gtx, len(ids), func(gtx layout.Context, i int) layout.Dimensions {
				id := ids[i]
				info := s.vm.Mods.Infos[id]
				return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return exwidgets.CustomCheckBox(gtx, th, values[id], clicks[id], func(gtx layout.Context) layout.Dimensions {
						l := material.Body2(th, info.Name+" ("+id+")")
						l.Color = theme.FgColor
						l.MaxLines = 1
						return l.Layout(gtx)
					})
				})
			})
		})
	})
}
