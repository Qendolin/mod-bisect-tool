package screens

import (
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	exwidgets "github.com/Qendolin/mod-bisect-tool/pkg/gui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

type ModSelectionScreen struct {
	app App

	searchEditor  widget.Editor
	continueClick widget.Clickable
	listState     widget.List

	// checkboxStates tracks the selection state of every mod persistently
	// across frames. The widget.Bool.Value is the source of truth.
	checkboxStates    map[string]*widget.Bool
	checkboxClicks    map[string]*widget.Clickable
	statesInitialized bool

	// unresolvableMods records which mods could not be activated due to
	// unresolvable dependencies. They cannot be force-enabled here and are
	// shown greyed out.
	unresolvableMods map[string]bool
}

func NewModSelectionScreen(app App) *ModSelectionScreen {
	s := &ModSelectionScreen{
		app:            app,
		checkboxStates: make(map[string]*widget.Bool),
		checkboxClicks: make(map[string]*widget.Clickable),
	}

	s.listState.Axis = layout.Vertical
	return s
}

func (s *ModSelectionScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	vm := s.app.GetViewModel()
	statuses := s.app.GetModStatusController().GetModStatuses()

	// Initialize a checkbox for every mod once the mod list is known.
	if !s.statesInitialized && vm.IsReady {
		s.unresolvableMods = make(map[string]bool)
		for id, status := range s.app.GetModStatusController().GetModStatuses() {
			if status.IsUnresolvable {
				s.unresolvableMods[id] = true
			}
		}
		for _, id := range vm.Mods.All {
			s.checkboxStates[id] = &widget.Bool{
				Value: statuses[id].Override == ui.ModOverrideForceEnabled,
			}
			s.checkboxClicks[id] = &widget.Clickable{}
		}
		s.statesInitialized = true
	}

	if s.continueClick.Clicked(gtx) {
		// Collect the selection on the frame, then commit off the frame.
		var forceEnabled []string
		for mod, boolState := range s.checkboxStates {
			if boolState.Value && !s.unresolvableMods[mod] {
				forceEnabled = append(forceEnabled, mod)
			}
		}
		go func() {
			defer logging.HandlePanic()
			// Stage all selected mods as force-enabled and commit. This keeps them
			// enabled during all tests so the tool can find what conflicts with them.
			modStatusCtrl := s.app.GetModStatusController()
			for _, mod := range forceEnabled {
				modStatusCtrl.SetOverride(mod, ui.ModOverrideForceEnabled)
			}
			modStatusCtrl.Commit()
			s.app.Run(func() {
				s.app.SwitchToMainScreen()
			})
		}()
	}

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return s.layoutTwoPanel(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return s.layoutLeftPanel(gtx, th)
			},
			func(gtx layout.Context) layout.Dimensions {
				return s.layoutRightPanel(gtx, th, &vm)
			},
		)
	})
}

// ── Left Panel (Simplified Instructions) ─────────────────────────────────────

func (s *ModSelectionScreen) layoutLeftPanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	headerText := s.app.Text("select_mods", "Select Mods to Keep Enabled", nil)
	descText := s.app.Text("select_mods_description", "If you don't know what's causing the issue, leave everything unchecked and click Continue.\n\n"+
		"If you know the issue involves a specific mod (like a shaders mod), check it here.\n\n"+
		"This ensures it stays turned on during all tests so the tool can find what is conflicting with it.", nil)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.H6(th, headerText)
			lbl.Color = theme.PrimaryColor
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(th, descText)
			lbl.Color = theme.FgColor
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, layout.Spacer{}.Layout), // Pushes button to bottom right
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					btn := material.Button(th, &s.continueClick, s.app.Text("continue", "Continue", nil))
					btn.Background = theme.PrimaryColor
					btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					return btn.Layout(gtx)
				}),
			)
		}),
	)
}

// ── Right Panel (Compact, Scrollable List) ───────────────────────────────────

func (s *ModSelectionScreen) layoutRightPanel(gtx layout.Context, th *material.Theme, vm *ui.BisectionViewModel) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, s.app.Text("available_mods", "Available Mods", nil))
			lbl.Font.Weight = font.Bold
			lbl.Color = theme.FgColor
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, lbl.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			ed := material.Editor(th, &s.searchEditor, s.app.Text("search_mods", "Search mods...", nil))
			ed.TextSize = unit.Sp(13)
			ed.Color = theme.FgColor
			ed.HintColor = theme.TextMutedColor
			border := widget.Border{
				Color:        theme.BorderColor,
				CornerRadius: unit.Dp(4),
				Width:        unit.Dp(1),
			}
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return border.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(6)).Layout(gtx, ed.Layout)
				})
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			filter := strings.ToLower(s.searchEditor.Text())

			var filteredMods []string
			for _, m := range vm.Mods.All {
				name := vm.Mods.Infos[m].Name
				if filter == "" ||
					strings.Contains(strings.ToLower(m), filter) ||
					strings.Contains(strings.ToLower(name), filter) {
					filteredMods = append(filteredMods, m)
				}
			}

			// Wrapped in material.List linked to s.listState to capture scroll interactions
			return material.List(th, &s.listState).Layout(gtx, len(filteredMods), func(gtx layout.Context, index int) layout.Dimensions {
				modName := filteredMods[index]
				modInfo := vm.Mods.Infos[modName]

				// Unresolvable mods cannot be force-enabled; show them greyed out.
				if s.unresolvableMods[modName] {
					return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th, modInfo.Name)
								lbl.Color = theme.TextMutedColor
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th, modInfo.ID+" ("+s.app.Text("unresolvable_lower", "unresolvable", nil)+")")
								lbl.Color = theme.TextMutedColor
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
						)
					})
				}

				// The checkbox and its clickable were initialized eagerly.
				boolState := s.checkboxStates[modName]
				boolClick := s.checkboxClicks[modName]

				// Tight vertical layout for maximum element density
				return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return exwidgets.CustomCheckBox(gtx, th, boolState, boolClick, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th, modInfo.Name)
								lbl.Color = theme.FgColor
								lbl.MaxLines = 1 // Truncate with ellipsis instead of wrapping.
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th, modInfo.ID)
								lbl.Color = theme.TextMutedColor
								lbl.MaxLines = 1 // Truncate with ellipsis instead of wrapping.
								return lbl.Layout(gtx)
							}),
						)
					})
				})
			})
		}),
	)
}

// ── Shared Helpers ────────────────────────────────────────────────────────────

func (s *ModSelectionScreen) layoutTwoPanel(gtx layout.Context, left, right layout.Widget) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, left),
		layout.Rigid(layout.Spacer{Width: unit.Dp(24)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(320)
			gtx.Constraints.Max.X = gtx.Dp(320)
			return right(gtx)
		}),
	)
}
