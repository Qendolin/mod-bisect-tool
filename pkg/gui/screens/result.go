package screens

import (
	"fmt"
	"image"
	"image/color"
	"strings"

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
	"golang.org/x/exp/shiny/materialdesign/icons"
)

type ResultScreen struct {
	app App

	// Clicks
	restartClick  widget.Clickable
	primaryClick  widget.Clickable
	continueClick widget.Clickable
	clearedClick  widget.Clickable

	// Scroll lists
	contentList widget.List

	// Collapsible state
	clearedExpanded bool
}

func NewResultScreen(app App) *ResultScreen {
	s := &ResultScreen{app: app}
	s.contentList.Axis = layout.Vertical
	return s
}

func (s *ResultScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	bvm := s.app.GetViewModel()
	rvm := s.app.GetResultViewModel()

	// Handle button clicks
	if s.restartClick.Clicked(gtx) {
		s.app.GetBisectionController().ResetSearch()
		s.app.SwitchToMainScreen()
	}

	if s.primaryClick.Clicked(gtx) {
		if rvm.State == ui.StateComplete {
			// ShowQuitDialog blocks on a native dialog; do not run it on the
			// gio frame goroutine.
			go func() {
				defer logging.HandlePanic()
				s.app.ShowQuitDialog()
			}()
		} else {
			s.app.SwitchToMainScreen()
		}
	}

	if s.continueClick.Clicked(gtx) {
		go func() {
			ok := s.app.ShowQuestionDialog(s.app.Text("continue_search", "Continue Search", nil), s.app.Text("continue_search_confirm", "This will start a new search for the next conflict set within the remaining mods. Continue?", nil), "", true)
			if ok {
				s.app.Run(func() {
					s.app.GetBisectionController().ContinueSearch()
					s.app.SwitchToMainScreen()
				})
			}
		}()
	}

	if s.clearedClick.Clicked(gtx) {
		s.clearedExpanded = !s.clearedExpanded
	}

	// Layout pinning header and footer, with middle body scrolling
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Header
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						titleText := s.app.Text("search_in_progress", "Search In Progress", nil)
						if bvm.Progress.IsComplete {
							titleText = s.app.Text("bisection_complete", "Bisection Complete", nil)
						}
						title := material.H5(th, titleText)
						title.Color = theme.PrimaryColor
						title.Font.Weight = font.Bold
						return title.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						// Separator
						return layout.Stack{}.Layout(gtx,
							layout.Expanded(func(gtx layout.Context) layout.Dimensions {
								paint.FillShape(gtx.Ops, theme.BorderColor, clip.Rect{Max: gtx.Constraints.Min}.Op())
								return layout.Dimensions{Size: gtx.Constraints.Min}
							}),
							layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								return layout.Dimensions{Size: image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(1)}}
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				)
			}),

			// Middle Body (Scrollable List)
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				widgets := s.buildScrollableWidgets(gtx, th, &bvm, &rvm)
				return material.List(th, &s.contentList).Layout(gtx, len(widgets), func(gtx layout.Context, index int) layout.Dimensions {
					return widgets[index](gtx)
				})
			}),

			// Footer
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						// Separator
						return layout.Stack{}.Layout(gtx,
							layout.Expanded(func(gtx layout.Context) layout.Dimensions {
								paint.FillShape(gtx.Ops, theme.BorderColor, clip.Rect{Max: gtx.Constraints.Min}.Op())
								return layout.Dimensions{Size: gtx.Constraints.Min}
							}),
							layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								return layout.Dimensions{Size: image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(1)}}
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						// Action Buttons
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Flexed(1, layout.Spacer{}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								btn := material.Button(th, &s.restartClick, s.app.Text("restart_bisection", "Restart Bisection", nil))
								btn.Background = theme.CardBgColor
								btn.Color = theme.FgColor
								return btn.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								btnText := s.app.Text("quit", "Quit", nil)
								if rvm.State != ui.StateComplete {
									btnText = s.app.Text("next_step_plain", "Next Step", nil)
								}
								btn := material.Button(th, &s.primaryClick, btnText)
								btn.Background = theme.PrimaryColor
								btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
								return btn.Layout(gtx)
							}),
						)
					}),
				)
			}),
		)
	})
}

func (s *ResultScreen) buildScrollableWidgets(gtx layout.Context, th *material.Theme, bvm *ui.BisectionViewModel, rvm *ui.ResultViewModel) []layout.Widget {
	var widgets []layout.Widget

	// 1a. Render the Current Conflict Set (isolated from history)
	if len(rvm.CurrentConflict.Mods) > 0 {
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, s.app.Text("current_conflict", "Current Conflict", nil))
			lbl.Font.Weight = font.Bold
			lbl.Color = theme.WarningColor
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, lbl.Layout)
		})

		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(th, s.app.Text("single_mod_resolves", "Disabling any single mod in this group will resolve the issue:", nil))
			lbl.Color = theme.FgColor
			return layout.Inset{Bottom: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, lbl.Layout)
		})

		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				showIncompleteHints := rvm.State != ui.StateComplete
				return s.layoutConflictSetEntries(gtx, th, rvm.CurrentConflict, showIncompleteHints, rvm.IsVerificationStep)
			})
		})

		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return s.drawInnerSeparator(gtx)
			})
		})
	}

	// 1b. Render older/historical independent conflict sets
	if len(rvm.ArchivedConflictSets) > 0 {
		// The current conflict is implicitly #1 when it has entries; otherwise
		// the archived sets are numbered from #1.
		numberOffset := 2
		if len(rvm.CurrentConflict.Mods) == 0 {
			numberOffset = 1
		}
		for i, conflictSet := range rvm.ArchivedConflictSets {
			currentSet := conflictSet
			index := i

			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				title := s.app.Text("independent_conflict_set", "Independent Conflict Set #{{.Number}}", map[string]any{"Number": index + numberOffset})
				lbl := material.Body1(th, title)
				lbl.Font.Weight = font.Bold
				lbl.Color = theme.TextMutedColor
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, lbl.Layout)
			})

			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, s.app.Text("single_mod_resolves", "Disabling any single mod in this group will resolve the issue:", nil))
				lbl.Color = theme.FgColor
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, lbl.Layout)
			})

			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return s.layoutConflictSetEntries(gtx, th, currentSet, false, false)
				})
			})

			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return s.drawInnerSeparator(gtx)
				})
			})
		}
	}

	// 2. Generally Unresolvable Mods (Dependency issues unrelated to active conflicts)
	if len(rvm.GenerallyUnresolvable) > 0 {
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, s.app.Text("unresolved_dependencies", "Mods with Unresolved Dependencies", nil))
			lbl.Font.Weight = font.Bold
			lbl.Color = theme.TextMutedColor
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, lbl.Layout)
		})

		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(th, s.app.Text("unresolved_dependencies_description", "These mods have separate dependency issues that may require manual review:", nil))
			lbl.Color = theme.FgColor
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, lbl.Layout)
		})

		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return s.layoutGenerallyUnresolvable(gtx, th, rvm.GenerallyUnresolvable)
			})
		})

		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return s.drawInnerSeparator(gtx)
			})
		})
	}

	// 3. Next Steps / Actions Panel
	if len(rvm.CurrentConflict.Mods) == 0 && len(rvm.ArchivedConflictSets) == 0 && len(rvm.GenerallyUnresolvable) == 0 {
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, s.app.Text("no_conflicts", "No Conflicts Found", nil))
			lbl.Font.Weight = font.Bold
			lbl.Color = theme.PrimaryColor
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, lbl.Layout)
		})
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(th, s.app.Text("no_conflicts_description", "The bisection process completed without isolating a specific cause for failure. The issue might be external to the mods in this folder.", nil))
			lbl.Color = theme.FgColor
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, lbl.Layout)
		})
	} else if len(rvm.CurrentConflict.Mods) > 0 || len(rvm.ArchivedConflictSets) > 0 {
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, s.app.Text("what_next", "What to do next", nil))
			lbl.Font.Weight = font.Bold
			lbl.Color = theme.FgColor
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, lbl.Layout)
		})
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			// Dynamically compute the explanation message block based on state criteria
			var explanation string
			switch {
			case rvm.State == ui.StateComplete:
				explanation = s.app.Text("result_complete_explanation", "To fix each conflict, disable one mod from that conflict's list and relaunch the game.\n\nOnce confirmed, please consider reporting the incompatibility to the respective mod authors.", nil)
			case rvm.IsVerificationStep:
				explanation = s.app.Text("result_verification_explanation", "A new conflicting mod was found, but it is not yet known if more are involved.\n\nYou can already fix this conflict by disabling one of the mods above.\n\nOr continue the search to verify whether the conflict set is complete.", nil)
			default:
				explanation = s.app.Text("result_incomplete_explanation", "The current conflict involves more mods than found so far.\n\nYou can already fix this conflict by disabling one of the mods above.\n\nOr continue the search to find the remaining mods.", nil)
			}

			lbl := material.Body2(th, explanation)
			lbl.Color = theme.FgColor
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, lbl.Layout)
		})

		// Display Continue Search Option if candidates remain
		if rvm.CanContinueSearch && len(bvm.Sets.Candidate) > 0 {
			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return s.drawInnerSeparator(gtx)
				})
			})
			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body1(th, s.app.Text("still_issues", "Still having issues?", nil))
				lbl.Font.Weight = font.Bold
				lbl.Color = theme.FgColor
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, lbl.Layout)
			})
			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(th, s.app.Text("still_issues_description", "If you disabled the mods above but your game still has the issue, there might be additional conflicting mods. You can continue the bisection process to find them among the remaining candidates.", nil))
				lbl.Color = theme.FgColor
				return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, lbl.Layout)
			})
			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				btn := material.Button(th, &s.continueClick, s.app.Text("continue_search", "Continue Search", nil))
				btn.Background = theme.PrimaryColor
				btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(btn.Layout),
				)
			})
		}
	}

	// 4. Cleared Mods (Accordion without text glyph arrows)
	clearedList := sets.MakeSlice(bvm.Sets.Cleared)
	if len(clearedList) > 0 {
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return s.drawInnerSeparator(gtx)
			})
		})
		widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
			btnText := s.app.Text("show_cleared_mods", "Show Cleared Mods", nil)
			if s.clearedExpanded {
				btnText = s.app.Text("hide_cleared_mods", "Hide Cleared Mods", nil)
			}
			btnText = s.app.Text("cleared_mods_count", "{{.Label}} ({{.Count}})", map[string]any{"Label": btnText, "Count": len(clearedList)})
			btn := material.Button(th, &s.clearedClick, btnText)
			btn.Background = theme.CardBgColor
			btn.Color = theme.FgColor
			btn.Inset = layout.UniformInset(unit.Dp(10))
			return btn.Layout(gtx)
		})
		if s.clearedExpanded {
			widgets = append(widgets, func(gtx layout.Context) layout.Dimensions {
				clearedText := strings.Join(clearedList, ", ")
				lbl := material.Body2(th, clearedText)
				lbl.Color = theme.TextMutedColor
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(16)}.Layout(gtx, lbl.Layout)
			})
		}
	}

	return widgets
}

func (s *ResultScreen) layoutConflictSetEntries(gtx layout.Context, th *material.Theme, set ui.ConflictSetReport, showIncompleteHints bool, isVerification bool) layout.Dimensions {
	var children []layout.FlexChild

	// Standard items loop
	for _, entry := range set.Mods {
		currentEntry := entry
		modName := ui.FormatModRef(currentEntry.Mod)
		jarName := s.app.Text("unknown_jar", "Unknown Jar File", nil)
		if !currentEntry.Mod.IsUnknown {
			jarName = fmt.Sprintf("%s.jar", currentEntry.Mod.BaseFilename)
		}

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						i, _ := widget.NewIcon(icons.AlertWarning)
						return i.Layout(gtx, theme.WarningColor)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body1(th, modName)
								lbl.Font.Weight = font.Bold
								lbl.Color = theme.FgColor
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body2(th, jarName)
								lbl.Color = theme.TextMutedColor
								return lbl.Layout(gtx)
							}),
						)
					}),
				)
			})
		}))

		// Graphical layout for per-mod cascades
		if len(currentEntry.AlsoRequireDisable) > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(32), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var cascadeChildren []layout.FlexChild
					cascadeChildren = append(cascadeChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(th, s.app.Text("cascade_disable", "Disabling this mod also requires disabling:", nil))
						lbl.Font.Weight = font.Bold
						lbl.Color = theme.TextMutedColor
						return lbl.Layout(gtx)
					}))

					for _, cascadeMod := range currentEntry.AlsoRequireDisable {
						m := cascadeMod
						cascadeChildren = append(cascadeChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							name := m.ID
							if !m.IsUnknown {
								name = s.app.Text("from_jar", "{{.ID}} from '{{.File}}.jar'", map[string]any{"ID": m.ID, "File": m.BaseFilename})
							} else {
								name = fmt.Sprintf("%s %s", m.ID, s.app.Text("from_unknown", "from unknown", nil))
							}
							lbl := material.Body2(th, name)
							lbl.Color = theme.TextMutedColor
							return layout.Inset{Left: unit.Dp(12), Top: unit.Dp(2)}.Layout(gtx, lbl.Layout)
						}))
					}

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, cascadeChildren...)
				})
			}))
		}
	}

	// Graphical implementation of structural hints for incomplete / developing sets
	if showIncompleteHints {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(32), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				hintText := s.app.Text("at_least_one_more", "And at least one more...", nil)
				if isVerification {
					hintText = s.app.Text("possibly_more", "And possibly more...", nil)
				}

				lbl := material.Body2(th, hintText)
				lbl.Font.Style = font.Italic
				lbl.Color = theme.TextMutedColor
				return lbl.Layout(gtx)
			})
		}))
	}

	// Graphical layout for per-set cascades
	if len(set.IfAllDisabledAlso) > 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var footerChildren []layout.FlexChild
				footerChildren = append(footerChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(th, s.app.Text("cascade_all_disable", "If you disable all mods in this conflict, you must also disable:", nil))
					lbl.Font.Weight = font.Bold
					lbl.Color = theme.TextMutedColor
					return lbl.Layout(gtx)
				}))

				for _, cascadeMod := range set.IfAllDisabledAlso {
					m := cascadeMod
					footerChildren = append(footerChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						name := m.ID
						if !m.IsUnknown {
							name = s.app.Text("from_jar", "{{.ID}} from '{{.File}}.jar'", map[string]any{"ID": m.ID, "File": m.BaseFilename})
						} else {
							name = fmt.Sprintf("%s %s", m.ID, s.app.Text("from_unknown", "from unknown", nil))
						}
						lbl := material.Body2(th, name)
						lbl.Color = theme.TextMutedColor
						return layout.Inset{Left: unit.Dp(12), Top: unit.Dp(2)}.Layout(gtx, lbl.Layout)
					}))
				}

				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, footerChildren...)
			})
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (s *ResultScreen) layoutGenerallyUnresolvable(gtx layout.Context, th *material.Theme, items []ui.UnresolvedDependencyReport) layout.Dimensions {
	var children []layout.FlexChild

	for _, item := range items {
		currentItem := item
		if currentItem.Mod.IsUnknown {
			continue
		}

		modName := fmt.Sprintf("%s (%s)", currentItem.Mod.ID, currentItem.Mod.Name)
		jarName := fmt.Sprintf("%s.jar", currentItem.Mod.BaseFilename)

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(th, modName)
						lbl.Font.Weight = font.Bold
						lbl.Color = theme.FgColor
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(th, jarName)
						lbl.Color = theme.TextMutedColor
						return lbl.Layout(gtx)
					}),
				)
			})
		}))

		if len(currentItem.UnmetDependencies) > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(24), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var innerChildren []layout.FlexChild
					innerChildren = append(innerChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(th, s.app.Text("unresolved_or_unmet", "Unresolved or unmet dependencies:", nil))
						lbl.Font.Weight = font.Bold
						lbl.Color = theme.TextMutedColor
						return lbl.Layout(gtx)
					}))

					for _, dep := range currentItem.UnmetDependencies {
						d := dep
						innerChildren = append(innerChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							name := d.ID
							if !d.IsUnknown {
								name = s.app.Text("from_jar", "{{.ID}} from '{{.File}}.jar'", map[string]any{"ID": d.ID, "File": d.BaseFilename})
							} else {
								name = fmt.Sprintf("%s %s", d.ID, s.app.Text("from_unknown", "from unknown", nil))
							}
							lbl := material.Body2(th, name)
							lbl.Color = theme.TextMutedColor
							return layout.Inset{Left: unit.Dp(12), Top: unit.Dp(2)}.Layout(gtx, lbl.Layout)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, innerChildren...)
				})
			}))
		}

		if len(currentItem.RequiredByTransitive) > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(24), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var innerChildren []layout.FlexChild
					innerChildren = append(innerChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(th, s.app.Text("cascade_disable_would", "Disabling this mod would also require disabling:", nil))
						lbl.Font.Weight = font.Bold
						lbl.Color = theme.TextMutedColor
						return lbl.Layout(gtx)
					}))

					for _, dep := range currentItem.RequiredByTransitive {
						d := dep
						innerChildren = append(innerChildren, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							name := d.ID
							if !d.IsUnknown {
								name = s.app.Text("from_jar", "{{.ID}} from '{{.File}}.jar'", map[string]any{"ID": d.ID, "File": d.BaseFilename})
							} else {
								name = fmt.Sprintf("%s %s", d.ID, s.app.Text("from_unknown", "from unknown", nil))
							}
							lbl := material.Body2(th, name)
							lbl.Color = theme.TextMutedColor
							return layout.Inset{Left: unit.Dp(12), Top: unit.Dp(2)}.Layout(gtx, lbl.Layout)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, innerChildren...)
				})
			}))
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (s *ResultScreen) drawInnerSeparator(gtx layout.Context) layout.Dimensions {
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
