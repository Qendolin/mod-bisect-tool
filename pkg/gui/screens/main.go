package screens

import (
	"errors"
	"image"
	"image/color"
	"sort"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/bisect"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
	exwidgets "github.com/Qendolin/mod-bisect-tool/pkg/gui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

var colorWhite = color.NRGBA{R: 255, G: 255, B: 255, A: 255}

type MainScreen struct {
	app App

	candidatesList exwidgets.ModList
	activeModsList exwidgets.ModList

	stepClick widget.Clickable
	undoClick widget.Clickable

	successClick       widget.Clickable
	failureClick       widget.Clickable
	indeterminateClick widget.Clickable
	cancelClick        widget.Clickable

	isTestPromptActive bool
	// activeMods holds the mod list for the current test prompt. It is computed
	// once in ShowTestPrompt (which runs when a test becomes ready) rather than
	// on every frame, because building it requires a dependency resolution.
	activeMods []exwidgets.ModListItem
}

func NewMainScreen(app App) *MainScreen {
	s := &MainScreen{app: app}
	s.candidatesList = *exwidgets.NewModList()
	s.activeModsList = *exwidgets.NewModList()
	return s
}

func (s *MainScreen) ShowTestPrompt() {
	vm := s.app.GetViewModel()
	if !vm.CurrentTestPlan.IsPlanned() {
		return
	}
	statuses := s.app.GetModStatusController().GetModStatuses()
	effectiveSet := s.app.GetModStatusController().ResolveEffectiveSet(vm.CurrentTestPlan.ModIDsToTest)
	primary := vm.CurrentTestPlan.ModIDsToTest

	items := make([]exwidgets.ModListItem, 0, len(effectiveSet))
	for id := range effectiveSet {
		item := modListItem(id, vm.Mods.Infos[id])
		if _, isPrimary := primary[id]; !isPrimary {
			if statuses[id].Override == ui.ModOverrideForceEnabled {
				item.Tag = exwidgets.ModListTagAlwaysEnabled
			} else {
				item.Tag = exwidgets.ModListTagDependency
			}
		}
		items = append(items, item)
	}
	sortModItems(items)

	s.activeMods = items
	s.isTestPromptActive = true
}

func (s *MainScreen) HideTestPrompt() {
	s.isTestPromptActive = false
	s.activeMods = nil
}

func (s *MainScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	vm := s.app.GetViewModel()

	if s.undoClick.Clicked(gtx) && vm.Progress.CanUndo {
		go func() {
			defer logging.HandlePanic()
			ok := s.app.ShowQuestionDialog(s.app.Text("undo_last_step", "Undo Last Step", nil), s.app.Text("undo_confirm", "Are you sure you want to undo the last step?", nil), "", true)
			if !ok {
				return
			}
			err := s.app.GetBisectionController().Undo()
			if err != nil {
				if errors.Is(err, bisect.ErrUndoStackEmpty) {
					s.app.ShowInfoDialog(s.app.Text("cannot_undo", "Cannot Undo", nil), s.app.Text("nothing_to_undo", "Nothing left to undo.", nil), "")
				} else {
					s.app.ShowErrorDialog(s.app.Text("cannot_undo", "Cannot Undo", nil), s.app.Text("undo_failed", "The undo operation failed.", nil), err)
				}
			}
		}()
	}
	if s.stepClick.Clicked(gtx) && vm.IsReady && !vm.Progress.IsComplete {
		go func() {
			defer logging.HandlePanic()
			s.app.GetBisectionController().Step()
		}()
	}
	if s.successClick.Clicked(gtx) && s.isTestPromptActive {
		s.isTestPromptActive = false
		s.app.GetBisectionController().SubmitTestResult(imcs.TestResultGood)
	}
	if s.failureClick.Clicked(gtx) && s.isTestPromptActive {
		s.isTestPromptActive = false
		s.app.GetBisectionController().SubmitTestResult(imcs.TestResultFail)
	}
	if s.indeterminateClick.Clicked(gtx) && s.isTestPromptActive {
		s.isTestPromptActive = false
		s.app.GetBisectionController().SubmitTestResult(imcs.TestResultIndeterminate)
	}
	if s.cancelClick.Clicked(gtx) && s.isTestPromptActive {
		s.isTestPromptActive = false
		s.app.GetBisectionController().CancelTest()
	}

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if s.isTestPromptActive {
			return s.layoutTestPromptView(gtx, th, &vm)
		}
		return s.layoutNormalView(gtx, th, &vm)
	})
}

// ── Normal view ───────────────────────────────────────────────────────────────

func (s *MainScreen) layoutNormalView(gtx layout.Context, th *material.Theme, vm *ui.BisectionViewModel) layout.Dimensions {
	title, desc, stepBtnText, progress := normalViewContent(s.app, vm)

	var candidates []exwidgets.ModListItem
	var listHeader string
	if vm.Progress.IsVerificationStep {
		listHeader = s.app.Text("conflict_set", "Conflict Set", nil)
		candidates = modItemsFromSet(vm.Mods.Infos, vm.Sets.CurrentConflict)
	} else {
		candidates = modItemsFromSet(vm.Mods.Infos, vm.Sets.Candidate)
		listHeader = s.app.Translator().Plural("remaining_candidates", "Remaining Candidate ({{.Count}})", "Remaining Candidates ({{.Count}})", len(candidates), map[string]any{"Count": len(candidates)})
	}

	return s.layoutTwoPanel(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return s.layoutNormalLeft(gtx, th, vm, title, desc, stepBtnText, progress)
		},
		func(gtx layout.Context) layout.Dimensions {
			return s.layoutListPanel(gtx, th, &s.candidatesList, listHeader, candidates)
		},
	)
}

// normalViewContent derives all display strings and progress from the view model.
// Kept as a plain function (no receiver) since it is pure data transformation.
func normalViewContent(app App, vm *ui.BisectionViewModel) (title, desc, stepBtnText string, progress float32) {
	t := app.Translator()
	switch {
	case !vm.IsReady:
		return t.Text("initializing", "Initializing...", nil), "", t.Text("start_bisection_button", "▶  Start Bisection", nil), 0

	case vm.Progress.IsHalted:
		return t.Text("search_halted", "Search Halted", nil),
			t.Text("search_halted_description", "The search stopped because two groups of mods appear to depend on each other.\n"+
				"The halt page shows the two groups; resolve one of the involved mods and start a new search.",
				nil),
			t.Text("view_halted_groups", "View Halted Groups", nil),
			searchProgress(vm)

	case vm.Progress.IsVerificationStep:
		return t.Text("verifying_final_set", "Verifying final set...", nil),
			t.Text("verification_step_description", "The next test verifies that the found set of mods is the cause of the issue. "+
				"If the issue DOES NOT persist, a new round of tests is started to find other problematic mods.",
				nil),
			t.Text("verification_step", "▶  Verification Step", nil),
			float32(vm.Progress.StepCount) / float32(vm.Progress.StepCount+1)

	case vm.Progress.StepCount == 0:
		return t.Text("ready_to_begin", "Ready to begin", nil),
			t.Text("ready_to_begin_description", "This tool uses binary search to isolate problematic mods.\n"+
				"Each test halves the candidate set, finding conflicts efficiently.\n"+
				"Close the game if it is currently open!\n"+
				"Be ready to start the game when prompted.", nil),
			t.Text("start_bisection_button", "▶  Start Bisection", nil),
			0

	default:
		return t.Text("round_iteration", "Round {{.Round}} · Iteration {{.Iteration}}", map[string]any{"Round": vm.Progress.Round, "Iteration": vm.Progress.Iteration}),
			t.Text("estimated_tests", "Step {{.Step}} of ~{{.Max}} estimated tests.", map[string]any{"Step": vm.Progress.StepCount, "Max": vm.Progress.EstimatedMaxTests}),
			t.Text("next_step_button", "▶  Next Step", nil),
			searchProgress(vm)
	}
}

// searchProgress computes the fraction of the estimated maximum tests completed.
func searchProgress(vm *ui.BisectionViewModel) float32 {
	if vm.Progress.EstimatedMaxTests <= 0 {
		return 0
	}
	prog := float32(vm.Progress.StepCount) / float32(vm.Progress.EstimatedMaxTests)
	if prog > 1.0 {
		prog = 1.0
	}
	return prog
}

func (s *MainScreen) layoutNormalLeft(
	gtx layout.Context, th *material.Theme, vm *ui.BisectionViewModel,
	title, desc, stepBtnText string, progress float32,
) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.H6(th, title)
			lbl.Color = theme.PrimaryColor
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(s.layoutDivider),
		layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(th, desc)
					lbl.Color = theme.FgColor
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !vm.IsReady || vm.Progress.StepCount != 0 {
						return layout.Dimensions{}
					}
					return s.layoutBackupWarning(gtx, th)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			pb := material.ProgressBar(th, progress)
			pb.Color = theme.PrimaryColor
			pb.TrackColor = theme.BorderColor
			return layout.Inset{Bottom: unit.Dp(20)}.Layout(gtx, pb.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return s.layoutUndoStepButtons(gtx, th, vm, stepBtnText)
		}),
	)
}

func (s *MainScreen) layoutBackupWarning(gtx layout.Context, th *material.Theme) layout.Dimensions {
	prefix := material.Body2(th, s.app.Text("world_backup_prefix", "Make a ", nil))
	prefix.Color = theme.FgColor

	backup := material.Body2(th, s.app.Text("world_backup_word", "backup", nil))
	backup.Color = theme.WarningColor
	backup.Font.Weight = font.Bold

	suffix := material.Body2(th, s.app.Text("world_backup_suffix", " of worlds you load!", nil))
	suffix.Color = theme.FgColor

	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(prefix.Layout),
			layout.Rigid(backup.Layout),
			layout.Rigid(suffix.Layout),
		)
	})
}

func (s *MainScreen) layoutUndoStepButtons(gtx layout.Context, th *material.Theme, vm *ui.BisectionViewModel, stepText string) layout.Dimensions {
	undoBtn := material.Button(th, &s.undoClick, s.app.Text("undo", "↩  Undo", nil))
	if vm.Progress.CanUndo {
		undoBtn.Background = theme.CardBgColor
		undoBtn.Color = theme.FgColor
	} else {
		undoBtn.Background = theme.BorderColor
		undoBtn.Color = theme.TextMutedColor
	}

	stepBtn := material.Button(th, &s.stepClick, stepText)
	if vm.IsReady && !vm.Progress.IsComplete {
		stepBtn.Background = theme.PrimaryColor
		stepBtn.Color = colorWhite
	} else {
		stepBtn.Background = theme.BorderColor
		stepBtn.Color = theme.TextMutedColor
	}

	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, undoBtn.Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Flexed(1.5, stepBtn.Layout),
	)
}

// ── Test prompt view ──────────────────────────────────────────────────────────

func (s *MainScreen) layoutTestPromptView(gtx layout.Context, th *material.Theme, vm *ui.BisectionViewModel) layout.Dimensions {
	var header, desc string
	if vm.Progress.IsVerificationStep {
		header = s.app.Text("verification_test", "Verification Test", nil)
		desc = s.app.Text("verification_test_description", "Start Minecraft with the current active mod set and verify whether your issue is still present.\n\n"+
			"✗ Broken\n  The issue is still there (confirms the found conflict set is correct).\n\n"+
			"✓ Works\n  The issue is gone (suggests the set is incomplete).\n\n"+
			"? Can't Tell\n  The game crashed or you cannot observe whether the issue is present.", nil)
	} else {
		header = s.app.Text("bisection_test", "Bisection Test", nil)
		desc = s.app.Text("bisection_test_description", "Start Minecraft with the current active mod set and verify whether your issue is resolved.\n\n"+
			"✓ Works\n  The game runs fine and the issue is gone.\n\n"+
			"✗ Broken\n  The issue is still present in the game.\n\n"+
			"? Can't Tell\n  The game crashed or you cannot observe whether the issue is present.", nil)
	}

	return s.layoutTwoPanel(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return s.layoutTestPromptLeft(gtx, th, header, desc)
		},
		func(gtx layout.Context) layout.Dimensions {
			return s.layoutListPanel(gtx, th, &s.activeModsList,
				s.app.Translator().Plural("active_mods", "Active mod ({{.Count}})", "Active mods ({{.Count}})", len(s.activeMods), map[string]any{"Count": len(s.activeMods)}),
				s.activeMods,
			)
		},
	)
}

func (s *MainScreen) layoutTestPromptLeft(gtx layout.Context, th *material.Theme, header, desc string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.H6(th, header)
			lbl.Color = theme.PrimaryColor
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(s.layoutDivider),
		layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(th, desc)
			lbl.Color = theme.FgColor
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return s.layoutSuccessFailureButtons(gtx, th)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return s.layoutCancelButton(gtx, th)
		}),
	)
}

func (s *MainScreen) layoutSuccessFailureButtons(gtx layout.Context, th *material.Theme) layout.Dimensions {
	successBtn := material.Button(th, &s.successClick, s.app.Text("works", "✓ Works", nil))
	successBtn.Background = theme.SuccessColor
	successBtn.Color = colorWhite

	indeterminateBtn := material.Button(th, &s.indeterminateClick, s.app.Text("cant_tell", "? Can't Tell", nil))
	indeterminateBtn.Background = color.NRGBA{R: 90, G: 62, B: 12, A: 255} // Dark amber
	indeterminateBtn.Color = theme.WarningColor

	failureBtn := material.Button(th, &s.failureClick, s.app.Text("broken", "✗ Broken", nil))
	failureBtn.Background = theme.DangerColor
	failureBtn.Color = colorWhite

	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, successBtn.Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Flexed(1, indeterminateBtn.Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Flexed(1, failureBtn.Layout),
	)
}

func (s *MainScreen) layoutCancelButton(gtx layout.Context, th *material.Theme) layout.Dimensions {
	btn := material.Button(th, &s.cancelClick, s.app.Text("cancel_test", "Cancel Test", nil))
	btn.Background = theme.CardBgColor
	btn.Color = theme.FgColor
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, btn.Layout),
	)
}

// ── Shared helpers ────────────────────────────────────────────────────────────

// layoutTwoPanel renders a flexible left panel beside a fixed 320dp right panel.
func (s *MainScreen) layoutTwoPanel(gtx layout.Context, left, right layout.Widget) layout.Dimensions {
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

// layoutListPanel renders a bold header label above a ModList.
func (s *MainScreen) layoutListPanel(gtx layout.Context, th *material.Theme, list *exwidgets.ModList, header string, items []exwidgets.ModListItem) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(th, header)
			lbl.Font.Weight = font.Bold
			lbl.Color = theme.FgColor
			return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, lbl.Layout)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return list.Layout(gtx, th, items, s.app.Translator())
		}),
	)
}

// modListItem converts a mod id into a ModListItem, using the friendly name
// from ModsInfo when available.
func modListItem(id string, info ui.ModViewModel) exwidgets.ModListItem {
	return exwidgets.ModListItem{Name: info.Name, ID: info.ID}
}

// modItemsFromSet converts a set of mod ids into a ModList of items, ordered
// alphabetically by name.
func modItemsFromSet(modsInfo map[string]ui.ModViewModel, modSet sets.Set) []exwidgets.ModListItem {
	items := make([]exwidgets.ModListItem, 0, len(modSet))
	for id := range modSet {
		items = append(items, modListItem(id, modsInfo[id]))
	}
	sortModItems(items)
	return items
}

// sortModItems orders mod items alphabetically by name, falling back to the id.
func sortModItems(items []exwidgets.ModListItem) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].Name, items[j].Name
		if a == "" {
			a = items[i].ID
		}
		if b == "" {
			b = items[j].ID
		}
		if a != b {
			return a < b
		}
		return items[i].ID < items[j].ID
	})
}

// layoutDivider draws a full-width 1dp horizontal separator line.
// Its signature matches layout.Widget so it can be passed directly to layout.Rigid.
func (s *MainScreen) layoutDivider(gtx layout.Context) layout.Dimensions {
	sz := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(1)}
	paint.FillShape(gtx.Ops, theme.BorderColor, clip.Rect{Max: sz}.Op())
	return layout.Dimensions{Size: sz}
}
