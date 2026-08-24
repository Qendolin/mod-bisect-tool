package pages

import (
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/rivo/tview"
)

// PageLoadingID is the unique identifier for the LoadingPage.
const PageLoadingID = "loading_page"

// LoadingPage displays progress while mods are being loaded.
type LoadingPage struct {
	*tview.Flex
	app          tui.TUIApp
	progressBar  *tview.TextView
	progressText *tview.TextView
	statusText   *tview.TextView
}

// NewLoadingPage creates a new LoadingPage instance.
func NewLoadingPage(app tui.TUIApp) *LoadingPage {
	lp := &LoadingPage{
		Flex:         tview.NewFlex().SetDirection(tview.FlexRow),
		app:          app,
		progressBar:  tview.NewTextView().SetDynamicColors(true),
		progressText: tview.NewTextView().SetDynamicColors(true),
		statusText:   tview.NewTextView().SetDynamicColors(true),
	}

	lp.progressBar.SetTextAlign(tview.AlignCenter).SetBorder(true)

	centeredFlex := tview.NewFlex().
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(tview.NewBox(), 0, 1, false).
				AddItem(lp.progressBar, 3, 0, false).
				AddItem(lp.progressText, 1, 0, false).
				AddItem(tview.NewBox(), 0, 1, false),
			80, 0, false,
		).
		AddItem(tview.NewBox(), 0, 1, false)

	lp.AddItem(widgets.NewTitleFrame(centeredFlex, "Loading Mods"), 0, 1, true)
	lp.statusText.SetText("Loading mod files from disk...")
	return lp
}

// StartLoading prepares the page for the loading process initiated by the App.
func (lp *LoadingPage) StartLoading(modsPath string) {
	lp.UpdateProgress("Starting...", 0, 0)
}

// UpdateProgress updates the progress bar and text.
func (lp *LoadingPage) UpdateProgress(currentFile string, i, count int) {
	var progress float32
	if count != 0 {
		progress = float32(i) / float32(count)
	}

	_, _, barWidth, _ := lp.progressBar.GetInnerRect()
	if barWidth <= 0 {
		return // Not ready to draw yet
	}

	filledWidth := int(progress * float32(barWidth))
	bar := fmt.Sprintf("[::b][white:blue]%s[-:-]%s[-:-:-]", strings.Repeat(" ", filledWidth), strings.Repeat(" ", barWidth-filledWidth))
	progressText := fmt.Sprintf("%d%%", int(progress*100))

	lp.progressBar.SetText(fmt.Sprintf("%s\n%s", bar, progressText))
	lp.progressText.SetText(fmt.Sprintf("Processing: [yellow]%s[-:-:-]", currentFile))
}

// GetActionPrompts returns the key actions for the loading page.
func (lp *LoadingPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status
func (lp *LoadingPage) GetStatusPrimitive() *tview.TextView {
	return lp.statusText
}

// Update implements the Page interface.
func (lp *LoadingPage) Update() {}
