package screens

import (
	"image"
	"image/color"
	"slices"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/component"
	"github.com/Qendolin/mod-bisect-tool/pkg/app"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	guii18n "github.com/Qendolin/mod-bisect-tool/pkg/gui/i18n"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/probe"
	"github.com/ncruces/zenity"
	"golang.org/x/text/language"
)

// loaderChoices lists the loader options in the order they are shown.
var loaderChoices = mods.SupportedRunLoaders()

type SetupScreen struct {
	app         App
	pathEditor  widget.Editor
	browseClick widget.Clickable
	startClick  widget.Clickable

	loaderSelect loaderSelect
	localeSelect loaderSelect

	// probeWorker serializes directory probes (one at a time, queued).
	probeWorker *probe.Worker

	// userSelectedLoader is true once the user picks a loader manually, so the
	// probe recommendation no longer overrides the selection.
	userSelectedLoader bool

	// lastPath tracks the last probed path to avoid re-probing on every frame.
	lastPath string

	locales []locale
}

type locale struct {
	tag  language.Tag
	name string
}

func NewSetupScreen(app App) *SetupScreen {
	s := &SetupScreen{
		app:         app,
		probeWorker: probe.NewWorker(),
	}
	s.pathEditor.SingleLine = true
	s.pathEditor.Submit = true

	s.loaderSelect.contextArea = component.ContextArea{
		Activation:       pointer.ButtonPrimary,
		AbsolutePosition: true,
	}

	// A loader forced via the command line preselects the option and is not
	// overridden by the probe.
	if cliLoader := app.GetViewModel().Loader.Preferred; cliLoader != "" {
		if idx, ok := loaderChoiceIndex(cliLoader); ok {
			s.loaderSelect.selected = idx
		}
		s.userSelectedLoader = true
	}
	s.locales = make([]locale, len(guii18n.SupportedLocales()))
	for i, choice := range guii18n.SupportedLocales() {
		s.locales[i] = locale{
			tag:  choice,
			name: s.app.Translator().TextIn(choice.String(), "locale_name", choice.String(), nil),
		}
	}
	slices.SortFunc(s.locales, func(a, b locale) int {
		return strings.Compare(a.name, b.name)
	})
	for i, choice := range s.locales {
		if choice.tag == s.app.Translator().Locale() {
			s.localeSelect.selected = i
		}
	}
	return s
}

func (s *SetupScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Process button clicks
	if s.browseClick.Clicked(gtx) {
		initial := s.pathEditor.Text()
		go func() {
			opts := []zenity.Option{
				zenity.Title(s.app.Text("select_mods_folder", "Select Mods Folder", nil)),
				zenity.Directory(),
				zenity.Modal(),
				zenity.Filename(initial),
			}
			if id := s.app.WindowAttachID(); id != nil {
				opts = append(opts, zenity.Attach(id))
			}
			path, err := zenity.SelectFile(opts...)
			if err == nil && path != "" {
				s.app.Run(func() {
					s.pathEditor.SetText(path)
				})
			}
		}()
	}

	// Probe whenever the entered path changes so the recommended loader can be
	// preselected. The probe worker ignores paths that are not valid
	// directories.
	path := s.pathEditor.Text()
	if path != s.lastPath {
		s.lastPath = path
		s.probeLoader(path, nil)
	}

	if s.startClick.Clicked(gtx) {
		path := s.pathEditor.Text()
		if path == "" {
			// ShowErrorDialog blocks on a native dialog; do not run it on the
			// gio frame goroutine.
			go func() {
				defer logging.HandlePanic()
				s.app.ShowErrorDialog(s.app.Text("error", "Error", nil), s.app.Text("select_mods_folder_error", "Please select a mods folder", nil), nil)
			}()
		} else {
			if probe.IsValidDir(path) {
				s.probeLoader(path, func() {
					s.startLoading(path)
				})
			} else {
				s.startLoading(path)
			}
		}
	}

	// Centered layout configuration. The language control is an overlay so it
	// stays fixed at the top-right independently of the centered form.
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Flexed(1, layout.Spacer{}.Layout), // top spacer
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					// Center the form horizontally with a maximum width of 540dp
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(1, layout.Spacer{}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							w := gtx.Dp(540)
							if w > gtx.Constraints.Max.X {
								w = gtx.Constraints.Max.X
							}
							gtx.Constraints.Min.X = w
							gtx.Constraints.Max.X = w

							return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
								// Title
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									title := material.H4(th, s.app.Text("app_name", "Mod Bisect Tool", nil))
									title.Color = theme.PrimaryColor
									title.Alignment = text.Middle
									title.Font.Weight = font.Bold
									return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, title.Layout)
								}),
								// Instruction
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									desc := material.Body1(th, s.app.Text("setup_description", "Select your mods folder to begin.", nil))
									desc.Color = theme.TextMutedColor
									desc.Alignment = text.Middle
									return layout.Inset{Bottom: unit.Dp(32)}.Layout(gtx, desc.Layout)
								}),
								// Mods folder label
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									heading := material.Body2(th, s.app.Text("mods_folder", "Mods Folder", nil))
									heading.Color = theme.TextMutedColor
									heading.Font.Weight = font.Bold
									return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, heading.Layout)
								}),
								// Path entry and browse button
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											// Editor wrapped in a styled container
											ed := material.Editor(th, &s.pathEditor, s.app.Text("mods_folder_hint", "Enter or browse mods folder path...", nil))
											ed.TextSize = unit.Sp(12)
											ed.Color = theme.FgColor
											ed.HintColor = theme.TextMutedColor
											border := widget.Border{
												Color:        theme.BorderColor,
												CornerRadius: unit.Dp(4),
												Width:        unit.Dp(1),
											}
											return border.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layout.UniformInset(unit.Dp(10)).Layout(gtx, ed.Layout)
											})
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											btn := material.Button(th, &s.browseClick, s.app.Text("browse", "Browse...", nil))
											btn.Background = theme.CardBgColor
											btn.Color = theme.FgColor
											return btn.Layout(gtx)
										}),
									)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(24)}.Layout),
								// Loader selection
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									heading := material.Body2(th, s.app.Text("mod_loader", "Mod Loader", nil))
									heading.Color = theme.TextMutedColor
									heading.Font.Weight = font.Bold
									return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, heading.Layout)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									labels := make([]string, len(loaderChoices))
									for i, choice := range loaderChoices {
										labels[i] = choice.String()
									}
									dims := s.loaderSelect.Layout(gtx, th, labels)
									// A user selection (via the popup) must not be
									// overridden by the probe recommendation.
									if s.loaderSelect.UserChanged() {
										s.userSelectedLoader = true
									}
									return dims
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(24)}.Layout),
								// Start Button
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									btn := material.Button(th, &s.startClick, s.app.Text("start_bisection", "Start Bisection", nil))
									btn.Background = theme.PrimaryColor
									btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
									btn.TextSize = unit.Sp(16)
									btn.Inset = layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12)}
									return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
										layout.Flexed(1, btn.Layout),
									)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									desc := material.Body1(th, s.app.Text("by_author", "by Qendolin", nil))
									desc.Color = theme.TextMutedColor
									desc.Alignment = text.Middle
									desc.TextSize = unit.Sp(10)
									return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, desc.Layout)
								}),
							)
						}),
						layout.Flexed(1, layout.Spacer{}.Layout),
					)
				}),
				layout.Flexed(1, layout.Spacer{}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					version := material.Body1(th, s.app.Text("version", "Version: {{.Version}}", map[string]any{"Version": app.VersionText()}))
					version.Color = theme.TextMutedColor
					version.Alignment = text.End
					version.TextSize = unit.Sp(9)
					return layout.Inset{Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Flexed(1, layout.Spacer{}.Layout),
							layout.Rigid(version.Layout),
						)
					})
				}),
			)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = 0
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = 0
						gtx.Constraints.Max.X = gtx.Dp(40)

						labels := make([]string, len(s.locales))
						for i, choice := range s.locales {
							labels[i] = choice.name
						}
						dims := s.localeSelect.LayoutIcon(gtx, th, labels)
						if s.localeSelect.UserChanged() {
							s.localeSelect.iconOpen = false
							s.app.SetLocale(s.locales[s.localeSelect.selected].tag.String())
						}
						return dims
					}),
				)
			})
		}),
	)
}

// probeLoader queues a probe of the given path, updating the recommended loader
// unless the user has made a manual selection. Probes run one at a time.
func (s *SetupScreen) probeLoader(path string, after func()) {
	resolved := probe.ResolveModsDir(path)
	if !probe.IsValidDir(resolved) {
		return
	}
	s.probeWorker.Request(resolved, func(res probe.ProbeResult) {
		s.app.Run(func() {
			if path != s.pathEditor.Text() {
				return
			}
			if !s.userSelectedLoader && res.PrimaryLoader != "" {
				if idx, ok := loaderChoiceIndex(res.PrimaryLoader); ok {
					s.loaderSelect.selected = idx
				}
			}
			if after != nil {
				after()
			}
		})
	})
}

func (s *SetupScreen) startLoading(path string) {
	// Read the loader selection on the UI thread: the probe worker and the
	// dropdown can update it concurrently, so reading it here avoids a data race.
	s.app.Run(func() {
		defer logging.HandlePanic()
		s.app.StartLoadingProcess(probe.ResolveModsDir(path), s.selectedLoader())
	})
}

// selectedLoader returns the loader to start with: the user's selection.
func (s *SetupScreen) selectedLoader() mods.RunLoader {
	idx := s.loaderSelect.selected
	if idx < 0 || idx >= len(loaderChoices) {
		return mods.RunLoaderFabric
	}
	return loaderChoices[idx]
}

// loaderChoiceIndex returns the index of a RunLoader in loaderChoices.
func loaderChoiceIndex(loader mods.RunLoader) (int, bool) {
	for i, choice := range loaderChoices {
		if choice == loader {
			return i, true
		}
	}
	return 0, false
}

// loaderSelect is a dropdown: a clickable field showing the current selection
// that opens a popup list of options. The popup is an overlay (component.
// ContextArea), so it does not affect the surrounding layout.
type loaderSelect struct {
	contextArea component.ContextArea
	menu        component.MenuState
	options     []widget.Clickable
	iconClick   widget.Clickable
	iconOpen    bool
	selected    int
	menuInit    bool
	userChanged bool
}

// UserChanged reports whether the user changed the selection since the last
// call, and clears the flag.
func (s *loaderSelect) UserChanged() bool {
	changed := s.userChanged
	s.userChanged = false
	return changed
}

// Layout renders the select and processes input events.
func (s *loaderSelect) Layout(gtx layout.Context, th *material.Theme, labels []string) layout.Dimensions {
	for len(s.options) < len(labels) {
		s.options = append(s.options, widget.Clickable{})
	}
	if s.selected < 0 || s.selected >= len(labels) {
		s.selected = 0
	}
	if !s.menuInit {
		s.menuInit = true
		s.buildMenu(th, labels)
	}

	// Only a user click on an option counts as a manual selection; the probe
	// writes selected directly.
	for i := range s.options {
		if s.options[i].Clicked(gtx) {
			s.selected = i
			s.userChanged = true
		}
	}

	box := s.renderBox(gtx, th, labels[s.selected])

	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return box
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return s.contextArea.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				// Open the menu just below the field.
				offset := layout.Inset{Top: unit.Dp(float32(box.Size.Y)/gtx.Metric.PxPerDp + 1)}
				return offset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = box.Size.X
					return component.Menu(th, &s.menu).Layout(gtx)
				})
			})
		}),
	)
}

// LayoutIcon renders a compact language button with the same popover menu as
// the loader select. It is used in the setup overlay rather than in the form.
func (s *loaderSelect) LayoutIcon(gtx layout.Context, th *material.Theme, labels []string) layout.Dimensions {
	for len(s.options) < len(labels) {
		s.options = append(s.options, widget.Clickable{})
	}
	if s.selected < 0 || s.selected >= len(labels) {
		s.selected = 0
	}
	if !s.menuInit {
		s.menuInit = true
		s.buildMenu(th, labels)
	}
	for i := range s.options {
		if s.options[i].Clicked(gtx) {
			s.selected = i
			s.userChanged = true
		}
	}
	if s.iconClick.Clicked(gtx) {
		s.iconOpen = !s.iconOpen
	}

	button := material.Button(th, &s.iconClick, "文")
	button.Background = theme.CardBgColor
	button.Color = theme.FgColor
	button.Inset = layout.UniformInset(unit.Dp(8))
	box := button.Layout
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(box),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			if !s.iconOpen {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(40)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(140)
				gtx.Constraints.Max.X = gtx.Dp(180)
				return component.Menu(th, &s.menu).Layout(gtx)
			})
		}),
	)
}

// buildMenu populates the popup menu options once.
func (s *loaderSelect) buildMenu(th *material.Theme, labels []string) {
	s.menu.Options = s.menu.Options[:0]
	for i, label := range labels {
		i, label := i, label
		s.menu.Options = append(s.menu.Options, func(gtx layout.Context) layout.Dimensions {
			item := component.MenuItem(th, &s.options[i], label)
			item.Label.TextSize = unit.Sp(12)
			return item.Layout(gtx)
		})
	}
}

// renderBox draws the field, sized to match the path input editor.
func (s *loaderSelect) renderBox(gtx layout.Context, th *material.Theme, label string) layout.Dimensions {
	border := widget.Border{Color: theme.BorderColor, CornerRadius: unit.Dp(4), Width: unit.Dp(1)}
	return border.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(th, unit.Sp(12), label)
					lbl.Color = theme.FgColor
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return s.drawCaret(gtx, theme.TextMutedColor)
					})
				}),
			)
		})
	})
}

// drawCaret renders a small downward triangle.
func (s *loaderSelect) drawCaret(gtx layout.Context, color color.NRGBA) layout.Dimensions {
	size := gtx.Dp(8)
	gtx.Constraints = layout.Exact(image.Pt(size, size))
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(0, 0))
	path.LineTo(f32.Pt(float32(size), 0))
	path.LineTo(f32.Pt(float32(size)/2, float32(size)))
	path.Close()
	paint.FillShape(gtx.Ops, color, clip.Outline{Path: path.End()}.Op())
	return layout.Dimensions{Size: image.Pt(size, size)}
}
