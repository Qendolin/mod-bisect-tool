package pages

import (
	"errors"
	"fmt"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/bisect"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/util"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PageMainID is the unique identifier for the MainPage.
const PageMainID = "main_page"

// MainPage is the primary view for the bisection process.
type MainPage struct {
	*tview.Flex
	app tui.TUIApp

	// UI Components
	overviewText   *tview.TextView
	stepButton     *tview.Button
	undoButton     *tview.Button
	tabs           *widgets.TabbedPanes
	statusText     *tview.TextView
	overviewWidget *widgets.OverviewWidget

	// Tab Content
	candidatesList       *widgets.SearchableList
	candidatesTitle      *widgets.TitleFrame
	clearedList          *widgets.SearchableList
	clearedTitle         *widgets.TitleFrame
	testGroupList        *widgets.SearchableList
	testGroupTitle       *widgets.TitleFrame
	implicitDepsList     *widgets.SearchableList
	implicitDepsTitle    *widgets.TitleFrame
	problematicModsList  *widgets.SearchableList
	problematicModsTitle *widgets.TitleFrame
}

// NewMainPage creates a new MainPage instance.
func NewMainPage(app tui.TUIApp) *MainPage {
	p := &MainPage{
		Flex:           tview.NewFlex().SetDirection(tview.FlexRow),
		app:            app,
		statusText:     tview.NewTextView().SetDynamicColors(true),
		overviewWidget: widgets.NewOverviewWidget(nil),
	}
	p.setupLayout()
	p.SetInputCapture(p.inputHandler())
	p.Update()
	p.statusText.SetText("Mods loaded, ready to start bisection.")
	return p
}

// setupLayout initializes and arranges all the UI components of the page.
func (p *MainPage) setupLayout() {
	// --- Overview Area ---
	p.overviewText = tview.NewTextView().SetDynamicColors(true)
	// Can't initialize it yet
	p.overviewWidget = widgets.NewOverviewWidget(nil)

	// This new container holds the text and the visual widget.
	overviewContentFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.overviewText, 4, 0, false).
		AddItem(p.overviewWidget, 1, 0, false)

	// --- Action Buttons ---
	p.stepButton = tview.NewButton("Start").SetSelectedFunc(func() {
		go func() {
			defer logging.HandlePanic()
			p.app.GetBisectionController().Step()
		}()
	})
	widgets.DefaultStyleButton(p.stepButton)
	p.undoButton = tview.NewButton("Undo").SetSelectedFunc(p.confirmUndo)
	widgets.DefaultStyleButton(p.undoButton)
	buttonFlex := tview.NewFlex().
		AddItem(p.stepButton, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(p.undoButton, 0, 1, false)
	buttonFlex.SetBorderPadding(1, 1, 0, 0)

	// --- Top-level Flex for Overview Section ---
	overviewFlex := tview.NewFlex().
		AddItem(overviewContentFlex, 0, 1, false).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(widgets.NewVerticalSeparator(tcell.ColorGray), 1, 0, false).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(buttonFlex, 31, 0, true)
	overviewFlex.SetBorderPadding(0, 0, 1, 1)

	// --- Tabs Setup ---
	p.tabs = widgets.NewTabbedPanes()
	p.tabs.SetBorderPadding(0, 0, 1, 1)
	p.setupTabPanes()

	// --- Final Page Layout ---
	p.AddItem(widgets.NewTitleFrame(overviewFlex, "Overview"), 6, 0, true).
		AddItem(widgets.NewTitleFrame(p.tabs, "Sets"), 0, 1, false)
}

// setupTabPanes populates the tabbed container with its pages.
func (p *MainPage) setupTabPanes() {
	p.candidatesList = widgets.NewSearchableList()
	p.clearedList = widgets.NewSearchableList()
	p.candidatesTitle = widgets.NewTitleFrame(p.candidatesList, "Candidates (Being Searched)")
	p.clearedTitle = widgets.NewTitleFrame(p.clearedList, "Cleared")
	searchPoolFlex := tview.NewFlex().
		AddItem(p.candidatesTitle, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(p.clearedTitle, 0, 1, true)
	p.tabs.AddTab("Search Pool", widgets.NewFocusWrapper(searchPoolFlex, func() []tview.Primitive {
		return []tview.Primitive{p.candidatesList, p.clearedList}
	}))

	p.testGroupList = widgets.NewSearchableList()
	p.implicitDepsList = widgets.NewSearchableList()
	p.testGroupTitle = widgets.NewTitleFrame(p.testGroupList, "Mods in Next Test Group")
	p.implicitDepsTitle = widgets.NewTitleFrame(p.implicitDepsList, "Implicitly Included Dependencies")
	testGroupFlex := tview.NewFlex().
		AddItem(p.testGroupTitle, 0, 1, true).
		AddItem(nil, 1, 0, false).
		AddItem(p.implicitDepsTitle, 0, 1, true)
	p.tabs.AddTab("Test Group", widgets.NewFocusWrapper(testGroupFlex, func() []tview.Primitive {
		return []tview.Primitive{p.testGroupList, p.implicitDepsList}
	}))

	p.problematicModsList = widgets.NewSearchableList()
	p.problematicModsTitle = widgets.NewTitleFrame(p.problematicModsList, "Problematic Mods")
	p.tabs.AddTab("Problematic Mods", widgets.NewFocusWrapper(p.problematicModsTitle, func() []tview.Primitive {
		return []tview.Primitive{p.problematicModsList}
	}))
}

func (p *MainPage) inputHandler() func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {

		// I don't know a proper fix for this
		if util.IsTextInput(p.app.GetFocus()) {
			return event
		}

		// If page-wide hotkeys are pressed, handle them.
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 's', 'S':
				go func() {
					defer logging.HandlePanic()
					p.app.GetBisectionController().Step()
				}()
				return nil
			case 'u', 'U':
				p.confirmUndo()
				return nil
			case 'm', 'M':
				p.app.Navigation().SwitchTo(PageManageModsID)
				return nil
			case 'r', 'R':
				p.app.Dialogs().ShowQuestionDialog("Confirmation", "Are you sure you want to reset the search?", "", false, func() {
					go func() {
						defer logging.HandlePanic()
						p.app.GetBisectionController().ResetSearch()
					}()
				}, nil)
				return nil
			}
		}

		return event // Return event if no one handled it
	}
}

// GetFocusablePrimitives implements the Focusable interface for the MainPage.
func (p *MainPage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{
		p.stepButton,
		p.undoButton,
		p.tabs,
	}
}

// Add this new method to the MainPage struct to implement the PageActivator interface.
func (p *MainPage) OnPageActivated() {
	p.Update()
}

// Update is the main entry point for updating the page's content based on the latest state of the search process.
func (p *MainPage) Update() {
	vm := p.app.GetViewModel()

	if !vm.IsReady {
		p.overviewText.SetText("Waiting for mods to be loaded and bisection to start...")
		return
	}

	// Pass by reference to avoid large copy
	p.updateOverview(&vm)
	p.updateModLists(&vm)
	p.updateTestGroupTab(&vm)
	p.updateOverviewWidget(&vm)
}

func (p *MainPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "S", Action: "Step"},
		{Input: "U", Action: "Undo"},
		{Input: "M", Action: "Manage Mods"},
		{Input: "R", Action: "Reset"},
	}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status
func (p *MainPage) GetStatusPrimitive() *tview.TextView {
	return p.statusText
}

// updateOverview updates the main status text and action buttons using the ViewModel.
func (p *MainPage) updateOverview(vm *ui.BisectionViewModel) {
	status, buttonText := p.determineStatusAndButtonText(vm)
	p.statusText.SetText(status)
	p.stepButton.SetLabel(buttonText)

	lastResultStr := "N/A"
	if vm.Progress.LastTestResult != imcs.TestResultUndefined {
		color := "green"
		if vm.Progress.LastTestResult == imcs.TestResultFail {
			color = "red"
		}
		if vm.Progress.LastTestResult == imcs.TestResultIndeterminate {
			color = "yellow"
		}
		lastResultStr = fmt.Sprintf("[%s]%s[-:-:-]", color, vm.Progress.LastTestResult)
	}

	overviewText := fmt.Sprintf(
		"Status: %s\nProgress: Round %d - Iteration %d - Test %d / ~%d\nLast Result: %s\nFound Problems: %d",
		status, vm.Progress.Round, vm.Progress.Iteration, vm.Progress.StepCount, vm.Progress.EstimatedMaxTests, lastResultStr, len(vm.Sets.CurrentConflict),
	)
	p.overviewText.SetText(overviewText)
}

// determineStatusAndButtonText computes the user-facing status string and button label from the ViewModel.
func (p *MainPage) determineStatusAndButtonText(vm *ui.BisectionViewModel) (status, buttonText string) {
	switch {
	case vm.Progress.IsHalted:
		return "Search Halted", "Results"
	case vm.Progress.IsComplete:
		return "Search Complete", "Results"
	case vm.Progress.StepCount > 0 && vm.CurrentTestPlan.IsPlanned():
		if vm.Progress.IsVerificationStep {
			return "Verifying final conflict set...", "Step"
		}
		return fmt.Sprintf("Test in progress (Iter %d)...", vm.Progress.Iteration), "Step"
	case vm.Progress.IsVerificationStep:
		return "Ready to verify conflict set", "Verify"
	case vm.Progress.StepCount > 0 || len(vm.Sets.CurrentConflict) > 0:
		return "Ready for next step", "Step"
	default:
		return "Ready to start bisection", "Start"
	}
}

// updateModLists populates the Candidates, Known Safe, and Problematic lists from the ViewModel.
func (p *MainPage) updateModLists(vm *ui.BisectionViewModel) {
	modCount := len(vm.Mods.All)

	p.updateList(p.candidatesList, p.candidatesTitle, sets.MakeSlice(vm.Sets.Candidate), vm.Mods.Infos, fmt.Sprintf("Candidates: %d / %d", len(vm.Sets.Candidate), modCount))
	p.updateList(p.problematicModsList, p.problematicModsTitle, sets.MakeSlice(vm.Sets.CurrentConflict), vm.Mods.Infos, fmt.Sprintf("Problematic Mods (Current Round): %d", len(vm.Sets.CurrentConflict)))
	p.updateList(p.clearedList, p.clearedTitle, sets.MakeSlice(vm.Sets.Cleared), vm.Mods.Infos, fmt.Sprintf("Cleared: %d", len(vm.Sets.Cleared)))
}

// updateTestGroupTab populates the lists in the "Test Group" tab from the ViewModel.
func (p *MainPage) updateTestGroupTab(vm *ui.BisectionViewModel) {
	if !vm.CurrentTestPlan.IsPlanned() {
		p.updateList(p.testGroupList, p.testGroupTitle, nil, vm.Mods.Infos, "Mods in Next Test Group: 0")
		p.updateList(p.implicitDepsList, p.implicitDepsTitle, nil, vm.Mods.Infos, "Implicitly Included Dependencies: 0")
		return
	}

	testSet := vm.CurrentTestPlan.ModIDsToTest
	p.updateList(p.testGroupList, p.testGroupTitle, sets.MakeSlice(testSet), vm.Mods.Infos, fmt.Sprintf("Mods in Next Test Group: %d", len(testSet)))

	// Calculate and display implicit dependencies.
	effectiveSet := p.app.GetModStatusController().ResolveEffectiveSet(testSet)
	implicitDeps := sets.Subtract(effectiveSet, testSet)
	p.updateList(p.implicitDepsList, p.implicitDepsTitle, sets.MakeSlice(implicitDeps), vm.Mods.Infos, fmt.Sprintf("Implicitly Included Dependencies: %d", len(implicitDeps)))
}

// updateOverviewWidget updates the visual overview bar from the ViewModel.
func (p *MainPage) updateOverviewWidget(vm *ui.BisectionViewModel) {
	p.overviewWidget.SetAllMods(vm.Mods.All)

	var effectiveSet sets.Set
	if vm.CurrentTestPlan.IsPlanned() {
		// Calculate the full effective set for the test.
		effectiveSet = p.app.GetModStatusController().ResolveEffectiveSet(vm.CurrentTestPlan.ModIDsToTest)
	}

	candidates := sets.Set{}
	// This makes the display more intuitive
	if !vm.Progress.IsVerificationStep {
		candidates = vm.Sets.Candidate
	}

	p.overviewWidget.UpdateState(vm.Sets.CurrentConflict, vm.Sets.Cleared, candidates, effectiveSet)
}

// updateList is a helper to populate a SearchableList and its title.
func (p *MainPage) updateList(list *widgets.SearchableList, titleFrame *widgets.TitleFrame, mods []string, modsInfo map[string]ui.ModViewModel, title string) {
	if len(mods) > 0 {
		list.SetItems(p.formatModList(mods, modsInfo))
	} else {
		list.SetItems([]string{"---"})
	}

	titleFrame.SetTitle(title)
}

// formatModList formats a list of mod IDs into user-friendly strings.
func (p *MainPage) formatModList(modIDs []string, modsInfo map[string]ui.ModViewModel) []string {
	formatted := make([]string, len(modIDs))
	for i, id := range modIDs {
		if mod, ok := modsInfo[id]; ok {
			formatted[i] = fmt.Sprintf("%s [gray](%s)[-:-:-]", id, mod.Name)
		} else {
			formatted[i] = id
		}
	}
	return formatted
}

func (p *MainPage) confirmUndo() {
	p.app.Dialogs().ShowQuestionDialog("Confirmation", "Are you sure you want to undo the last step?", "", true, func() {
		go func() {
			defer logging.HandlePanic()
			err := p.app.GetBisectionController().Undo()
			if err != nil {
				p.app.ExecuteAndDraw(func() {
					if errors.Is(err, bisect.ErrUndoStackEmpty) {
						p.app.Dialogs().ShowInfoDialog("Cannot Undo", "Nothing left to undo.", "", nil)
					} else {
						p.app.Dialogs().ShowErrorDialog("Cannot Undo", "The undo operation failed.", err, nil)
					}
				})
			}
		}()
	}, nil)
}
