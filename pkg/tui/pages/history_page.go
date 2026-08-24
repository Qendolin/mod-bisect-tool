package pages

import (
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	historyRowHeight = 3 // Height of each entry in the master list (1 for text, 1 for overview)
)

const PageHistoryID = "history_page"

// HistoryPage displays the bisection history in a master-detail view.
type HistoryPage struct {
	*tview.Flex
	app tui.TUIApp

	masterList *widgets.FlexList

	detailPanel          *tview.Flex
	detailOverviewWidget *widgets.OverviewWidget
	detailSummaryText    *tview.TextView
	detailSetsText       *tview.TextView

	historyCache []ui.ExecutionLogEntryViewModel
}

// NewHistoryPage creates a new page for viewing bisection history.
func NewHistoryPage(app tui.TUIApp) *HistoryPage {
	p := &HistoryPage{
		Flex: tview.NewFlex(),
		app:  app,
	}

	// Master Pane
	p.masterList = widgets.NewFlexList()
	p.masterList.SetBorderPadding(0, 0, 1, 1)
	p.masterList.SetChangedFunc(func(newIndex int) {
		p.updateDetailView(newIndex)
	})
	// Detail Pane
	p.detailPanel = tview.NewFlex().SetDirection(tview.FlexRow)
	p.detailPanel.SetBorderPadding(0, 0, 1, 1)

	// Create the overview widget with an empty list of mods initially.
	// It will be updated with the real list when the page is shown.
	p.detailOverviewWidget = widgets.NewOverviewWidget(nil)
	p.detailSummaryText = tview.NewTextView().SetDynamicColors(true)
	p.detailSetsText = tview.NewTextView().SetDynamicColors(true).SetWordWrap(true).SetRegions(true)

	detailOverviewFrame := widgets.NewTitleFrame(p.detailOverviewWidget, "Overview")
	detailOverviewFrame.SetBorder(false) // Remove redundant border if parent has one

	p.detailPanel.AddItem(p.detailSummaryText, 3, 0, false) // Height for summary
	p.detailPanel.AddItem(detailOverviewFrame, 3, 0, false) // Height for overview widget
	p.detailPanel.AddItem(p.detailSetsText, 0, 1, false)    // Remaining space for sets

	// Main Layout
	p.Flex.AddItem(widgets.NewTitleFrame(p.masterList, "History"), 0, 1, true).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(widgets.NewTitleFrame(p.detailPanel, "Details"), 0, 2, false)

	p.setInputCapture()

	p.refreshHistory()

	return p
}

func (p *HistoryPage) OnPageActivated() {
	p.refreshHistory()
	// need to re-focus
	p.app.SetFocus(p.masterList)
}

// setInputCapture handles navigation within the page.
func (p *HistoryPage) setInputCapture() {
	p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {

		if event.Key() == tcell.KeyEscape || (event.Key() == tcell.KeyCtrlH && event.Modifiers()&tcell.ModCtrl != 0) {
			p.app.Navigation().CloseModal()
			return nil
		}

		switch event.Rune() {
		case 'r', 'R':
			resultPage := NewResultPage(p.app)
			p.app.Navigation().ShowModal(tui.PageResultID, resultPage)
		}

		return event
	})
}

// refreshHistory populates the master list with history entries.
func (p *HistoryPage) refreshHistory() {
	p.masterList.Clear()

	vm := p.app.GetViewModel()

	if !vm.IsReady {
		p.masterList.AddItem(tview.NewTextView().SetText("Bisect process not ready."), 1, 0, false)
		p.updateDetailView(-1)
		return
	}

	p.historyCache = p.app.GetExecutionLogViewModel().Entries
	p.detailOverviewWidget.SetAllMods(vm.Mods.All)

	if len(p.historyCache) == 0 {
		p.masterList.AddItem(tview.NewTextView().SetText("No history yet."), 1, 0, false)
		p.updateDetailView(-1)
		return
	}

	for i, entry := range p.historyCache {
		number := i + 1
		summary := fmt.Sprintf("#%d: Round %d - Iter %d - Step %d: [yellow]%s[-]",
			number, entry.Round, entry.Iteration, entry.Step, entry.Result)

		summaryView := tview.NewTextView().SetDynamicColors(true).SetText(summary)
		summaryView.SetBackgroundColor(tcell.ColorNone)
		overview := widgets.NewOverviewWidget(vm.Mods.All)
		overview.SetBackgroundColor(tcell.ColorNone)
		p.updateOverviewState(overview, &entry)

		// Create the self-contained row
		row := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(summaryView, 1, 0, false).
			AddItem(overview, 1, 0, false)

		if i != len(p.historyCache)-1 {
			sep := widgets.NewHorizontalSeparator(tcell.ColorGray)
			sep.SetBackgroundColor(tcell.ColorNone)
			row.AddItem(sep, 1, 0, false)
		}

		// Each item added to FlexList is a complete visual block of height 3.
		p.masterList.AddItem(row, historyRowHeight, 0, false)
	}

	// Set initial selection to the last item. This will trigger the ChangedFunc.
	if p.masterList.GetItemCount() > 0 {
		p.masterList.SetCurrentItem(p.masterList.GetItemCount() - 1)
	} else {
		p.updateDetailView(-1) // Ensure detail view is cleared if no items
	}
}

func (p *HistoryPage) updateOverviewState(overview *widgets.OverviewWidget, entry *ui.ExecutionLogEntryViewModel) {
	effective := p.app.GetModStatusController().ResolveEffectiveSet(entry.Plan.ModIDsToTest)
	overview.UpdateState(entry.ConflictSet, entry.ClearedSet, entry.Candidates, effective)
}

// updateDetailView shows the details for the selected history entry.
func (p *HistoryPage) updateDetailView(index int) {
	if index < 0 || index >= len(p.historyCache) {
		p.detailSummaryText.SetText("")
		p.detailOverviewWidget.UpdateState(nil, nil, nil, nil)
		p.detailSetsText.SetText("")
		return
	}

	vm := p.app.GetViewModel()

	entry := p.historyCache[index]
	p.updateOverviewState(p.detailOverviewWidget, &entry)

	// Derive summary text
	number := index + 1
	testSet := entry.Plan.ModIDsToTest
	numTested := len(testSet)
	numCandidates := len(entry.Candidates)

	// Generate StateDescription on the fly for display
	stateDesc := ""
	if entry.Kind == imcs.TestPlanVerification {
		stateDesc = "This was a verification test of the current conflict set."
	} else if entry.Kind == imcs.TestPlanComplement {
		stateDesc = "This was a complement test of the second half after an indeterminate result."
	} else if entry.Kind == imcs.TestPlanContinuation {
		stateDesc = "This was a bisection step within a candidate set."
	} else {
		stateDesc = "This was the initial test of a candidate set."
	}

	summary := fmt.Sprintf("#%d: Round %d - Iteration %d - Step %d\nResult: [yellow]%s[-]\n%s\nTested: %d mods, Candidates: %d mods remaining",
		number, entry.Round, entry.Iteration, entry.Step, entry.Result, stateDesc, numTested, numCandidates)
	p.detailSummaryText.SetText(summary)

	// Display the sets. Convert maps to sorted slices for consistent display.
	combinedConflictSets := sets.Copy(entry.ConflictSet)
	for _, conflictSet := range vm.Sets.AllConflicts {
		combinedConflictSets = sets.AddInPlace(combinedConflictSets, conflictSet)
	}
	problematicList := sets.MakeSlice(combinedConflictSets)
	testSetList := sets.MakeSlice(testSet)
	clearedList := sets.MakeSlice(entry.ClearedSet)

	sets := fmt.Sprintf("[::b]Problematic Mods[-:-:-] (%d):\n%s\n\n[::b]Mods Tested[-:-:-] (%d):\n%s\n\n[::b]Cleared Mods[-:-:-] (%d):\n%s",
		len(problematicList), strings.Join(problematicList, "\n"),
		len(testSetList), strings.Join(testSetList, "\n"),
		len(clearedList), strings.Join(clearedList, "\n"))
	p.detailSetsText.SetText(sets)
}

// Page interface implementation
func (p *HistoryPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "↑/↓", Action: "Navigate History"},
		{Input: "R", Action: "Show Results"},
	}
}
func (p *HistoryPage) GetStatusPrimitive() *tview.TextView { return nil }

// GetFocusablePrimitives implements the Focusable interface.
func (p *HistoryPage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{
		p.masterList,
		p.detailSetsText,
	}
}

// Update implements the Page interface.
func (p *HistoryPage) Update() {}
