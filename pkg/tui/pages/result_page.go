package pages

import (
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const PageResultID = "result_page"

// resultStyles decorates the shared conflict-set writers with tview color tags.
// File names are omitted to save space in the terminal.
var resultStyles = ui.TextStyles{
	ModID:    func(s string) string { return "[red::b]" + s + "[-:-:-]" },
	Muted:    func(s string) string { return "[gray]" + s + "[-:-:-]" },
	ShowFile: false,
}

// ResultPage displays the final or intermediate results of the bisection search.
type ResultPage struct {
	*tview.Flex
	app            tui.TUIApp
	statusText     *tview.TextView
	resultView     *widgets.ScrollTextView
	closeButton    *tview.Button
	continueButton *tview.Button
}

// NewResultPage creates a new ResultPage.
func NewResultPage(app tui.TUIApp) *ResultPage {
	p := &ResultPage{
		Flex:       tview.NewFlex().SetDirection(tview.FlexRow),
		app:        app,
		statusText: tview.NewTextView().SetDynamicColors(true),
	}

	vm := app.GetResultViewModel()

	title, message, explanation := p.formatContent(&vm)

	p.resultView = widgets.NewScrollTextView()
	p.resultView.SetDynamicColors(true)
	p.resultView.SetWordWrap(true)
	p.resultView.SetText(message)
	p.resultView.SetBorderPadding(1, 0, 1, 1)

	explanationView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(explanation)
	explanationView.SetBorderPadding(1, 1, 1, 1)

	messageFrame := widgets.NewTitleFrame(p.resultView, "Result")
	explanationFrame := widgets.NewTitleFrame(explanationView, "What to do next")

	p.closeButton = tview.NewButton("Close").
		SetSelectedFunc(func() {
			p.app.Navigation().CloseModal()
		})
	widgets.DefaultStyleButton(p.closeButton)

	p.continueButton = tview.NewButton("Continue Search").
		SetSelectedFunc(func() {
			p.app.Dialogs().ShowQuestionDialog(
				"Confirmation",
				"This will start a new search for the next conflict set within the remaining mods. Continue?",
				"",
				true,
				func() { // OnYes
					p.app.Navigation().CloseModal()
					go func() {
						defer logging.HandlePanic()
						p.app.GetBisectionController().ContinueSearch()
					}()
				},
				nil, // OnNo
			)
		})
	widgets.DefaultStyleButton(p.continueButton)

	// Determine if the "Continue Search" button should be shown.
	canContinue := vm.CanContinueSearch
	buttonLayout := tview.NewFlex().
		AddItem(tview.NewBox(), 0, 1, false)

	if canContinue {
		buttonLayout.AddItem(p.closeButton, 15, 0, true)
		buttonLayout.AddItem(tview.NewBox(), 1, 0, false)
		buttonLayout.AddItem(p.continueButton, 20, 0, false)
	} else {
		p.continueButton.SetDisabled(true)
		buttonLayout.AddItem(p.closeButton, 15, 0, true)
	}
	buttonLayout.AddItem(tview.NewBox(), 0, 1, false)

	p.AddItem(messageFrame, 0, 2, false).
		AddItem(explanationFrame, 7, 0, false).
		AddItem(buttonLayout, 3, 0, true)

	p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			app.Navigation().CloseModal()
			return nil
		}
		return event
	})

	p.statusText.SetText(title)

	return p
}

// formatContent generates the appropriate text based on the bisection ViewModel.
func (p *ResultPage) formatContent(vm *ui.ResultViewModel) (title, message, explanation string) {
	if vm.State == ui.StateNotReady {
		return formatNotReadyContent()
	}

	// Combine all found conflict sets for display. For a complete search, this includes the final set found.

	if vm.State == ui.StateComplete {
		return formatCompleteContent(vm)
	}

	if vm.State == ui.StateInProgress {
		return formatInProgressContent(vm)
	}

	// No conflict element has been found yet.
	return formatNoResultsYetContent()
}

// ---------------------------------------------------------------------------
// State-level formatters
// ---------------------------------------------------------------------------

// formatNotReadyContent: the bisection has not been started at all.
func formatNotReadyContent() (title, message, explanation string) {
	title = "Search In Progress"
	message = "No results yet."
	explanation = "You haven't started the bisection yet."
	return
}

// formatNoResultsYetContent: the search is running but no conflict element has been
// discovered yet.
func formatNoResultsYetContent() (title, message, explanation string) {
	title = "Search In Progress"
	message = "No results yet."
	explanation = "No conflicts have been found yet. Continue the search on the main page."
	return
}

// formatInProgressContent: at least one conflict element has been found, but the
// search is not yet complete.
// Handles two sub-states:
//
//   - Element found, awaiting verification: a new element was just isolated but
//     the verification test (does the set reproduce the issue alone?) has not run yet.
//     We do not know yet whether more mods are involved.
//
//   - Set incomplete, searching for next element: verification returned GOOD (the current
//     set is not sufficient by itself), or we are mid-bisection looking for the next
//     element.
//     At least one more mod is known to be involved.
//
// In both sub-states the user can already fix the issue by disabling one of the found mods.
func formatInProgressContent(vm *ui.ResultViewModel) (title, message, explanation string) {
	title = "Intermediate Result"

	var b strings.Builder

	// Current, still-growing conflict set. It is kept separate from past sets,
	// which are rendered by writeArchivedConflictSets below.
	fmt.Fprintf(&b, "[::u]Current Conflict[-:-:-] (%d mods found so far)\n", len(vm.CurrentConflict.Mods))
	ui.WriteConflictSetMods(&b, vm.CurrentConflict.Mods, resultStyles)

	switch {
	case vm.IsVerificationStep:
		// Element found, awaiting verification: completeness is unknown.
		b.WriteString("  [gray]- And possibly more...\n")
		explanation = "A new conflicting mod was found, but it is not yet known if more are involved.\n" +
			"You can already fix this conflict by disabling one of the mods above.\n" +
			"Or continue the search to verify whether the conflict set is complete."
	default:
		// Set incomplete, searching for next element: at least one more mod is known to exist.
		// This covers both a fresh GOOD verification result and being mid-bisection for the
		// next element (in either case, more mods are known to be part of the conflict).
		b.WriteString("  [gray]- And at least one more...\n")
		explanation = "This conflict involves more mods than found so far.\n" +
			"You can already fix this conflict by disabling one of the mods above.\n" +
			"Or continue the search to find the remaining mods."
	}
	ui.WriteConflictSetFooter(&b, vm.CurrentConflict.IfAllDisabledAlso, resultStyles)

	writeArchivedConflictSets(&b, vm)

	message = b.String()
	return
}

// formatCompleteContent: the search has finished.
func formatCompleteContent(vm *ui.ResultViewModel) (title, message, explanation string) {
	title = "Search Complete"

	var b strings.Builder

	if len(vm.CurrentConflict.Mods) == 0 && len(vm.ArchivedConflictSets) == 0 {
		b.WriteString("No problematic mods were found.")
		explanation = "The bisection process completed without isolating a specific cause for failure."
		message = b.String()
		return
	}

	// The most recent conflict set. It is only archived into ArchivedConflictSets
	// once ContinueSearch runs, so it is rendered separately here.
	if len(vm.CurrentConflict.Mods) > 0 {
		fmt.Fprintf(&b, "[::u]Current Conflict[-:-:-] (%d mods)\n", len(vm.CurrentConflict.Mods))
		ui.WriteConflictSet(&b, vm.CurrentConflict, resultStyles)
	}

	// Independent conflict sets isolated in previous rounds.
	writeArchivedConflictSets(&b, vm)

	// Generally unresolvable mods section (dependency issues unrelated to conflicts).
	if len(vm.GenerallyUnresolvable) > 0 {
		b.WriteString("\n[gray]Mods with unresolved or unmet dependencies (may need manual review):\n")
		ui.WriteGenerallyUnresolvable(&b, vm.GenerallyUnresolvable, resultStyles)
	}

	if len(vm.ArchivedConflictSets) == 0 {
		explanation = "To fix this conflict, disable one of the mods listed above and relaunch the game.\nOnce resolved, please report the incompatibility to the mod authors."
	} else {
		explanation = "To fix each conflict, disable one mod from that conflict's list and relaunch the game.\nOnce resolved, please report the incompatibilities to the mod authors."
	}
	if vm.CanContinueSearch {
		explanation += "\n\nIf issues persist, use 'Continue Search' to find other conflicts."
	}

	message = b.String()
	return
}

// writeArchivedConflictSets renders the archived conflict sets from previous
// rounds under numbered "Independent Conflict Set" headers. Numbering continues
// after the current conflict set when it has entries.
func writeArchivedConflictSets(b *strings.Builder, vm *ui.ResultViewModel) {
	start := 1
	if len(vm.CurrentConflict.Mods) > 0 {
		start = 2
	}
	for i, cs := range vm.ArchivedConflictSets {
		fmt.Fprintf(b, "\n[::u]Independent Conflict Set #%d[-:-:-]\n", i+start)
		ui.WriteConflictSet(b, cs, resultStyles)
	}
}

// GetActionPrompts returns the key actions for the page.
func (p *ResultPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "↑/↓", Action: "Scroll Text"},
	}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status
func (p *ResultPage) GetStatusPrimitive() *tview.TextView {
	return p.statusText
}

// GetFocusablePrimitives returns the focusable primitives for the page.
func (p *ResultPage) GetFocusablePrimitives() []tview.Primitive {
	primitives := []tview.Primitive{p.closeButton}
	if !p.continueButton.IsDisabled() {
		primitives = append(primitives, p.continueButton)
	}
	primitives = append(primitives, p.resultView)
	return primitives
}

// Update implements the Page interface.
func (p *ResultPage) Update() {}
