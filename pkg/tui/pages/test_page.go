package pages

import (
	"time"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const PageTestID = "test_page"

// TestPage instructs the user to perform a manual test.
type TestPage struct {
	*tview.Flex
	app tui.TUIApp

	successBtn *tview.Button
	failBtn    *tview.Button
	unknownBtn *tview.Button
	backBtn    *tview.Button
	statusText *tview.TextView

	// useGlyphs selects between Unicode verdict glyphs (✓/✗) and their ASCII
	// fallbacks ([+]/[-]) depending on the terminal's likely font support.
	useGlyphs bool

	// callbacks
	onSuccess       func()
	onFailure       func()
	onIndeterminate func()
	onCancel        func()
}

// NewTestPage creates a new TestPage.
func NewTestPage(app tui.TUIApp, isVerification bool, onSuccess, onFailure, onIndeterminate, onCancel func()) *TestPage {
	p := &TestPage{
		Flex:            tview.NewFlex(),
		app:             app,
		statusText:      tview.NewTextView().SetDynamicColors(true),
		onSuccess:       onSuccess,
		onFailure:       onFailure,
		onIndeterminate: onIndeterminate,
		onCancel:        onCancel,
		useGlyphs:       tui.UnicodeGlyphsSupported(),
	}

	p.statusText.SetText("Report Manual Test Outcome")

	message := `
[::b]Mod files have been updated for the next test.

Please launch Minecraft now.

Once the game has loaded (or crashed), report the outcome below.

If you can't tell whether the issue is present, e.g. the game
crashed before you could observe it, select Can't Tell.`

	if isVerification {
		p.statusText.SetText("Verify Final Problematic Set")

		message = `
[::b]A potential problematic set has been found.

This test will run with ONLY the suspected problematic mods enabled.

Please launch Minecraft and confirm the failure persists.

If you can't tell whether the failure persists, e.g. the game
crashed before you could observe it, select Can't Tell.`
	}

	instructions := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(message)

	successLabel := "[+] Works"
	failLabel := "[-[] Broken"
	if p.useGlyphs {
		successLabel = "✓ Works"
		failLabel = "✗ Broken"
	}

	p.successBtn = tview.NewButton(successLabel).
		SetSelectedFunc(p.onSuccess)
	p.successBtn.SetDisabled(true)
	p.successBtn.SetDisabledStyle(widgets.DefaultButtonDisabledStyle)
	p.successBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorDarkGreen).Background(tcell.ColorWhite))
	p.successBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorGreen).Underline(true))

	p.failBtn = tview.NewButton(failLabel).
		SetSelectedFunc(p.onFailure)
	p.failBtn.SetDisabled(true)
	p.failBtn.SetDisabledStyle(widgets.DefaultButtonDisabledStyle)
	p.failBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorDarkRed).Background(tcell.ColorWhite))
	p.failBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorRed).Underline(true))

	p.unknownBtn = tview.NewButton("? Can't Tell").
		SetSelectedFunc(p.onIndeterminate)
	p.unknownBtn.SetDisabled(true)
	p.unknownBtn.SetDisabledStyle(widgets.DefaultButtonDisabledStyle)
	p.unknownBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorSaddleBrown).Background(tcell.ColorWhite))
	p.unknownBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorGoldenrod).Underline(true))

	p.backBtn = tview.NewButton("Cancel").
		SetSelectedFunc(p.onCancel)
	p.backBtn.SetDisabled(true)
	p.backBtn.SetDisabledStyle(widgets.DefaultButtonDisabledStyle)
	p.backBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite))
	p.backBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorBlue).Underline(true))

	// prevent accidental input
	go func() {
		defer logging.HandlePanic()
		time.Sleep(300 * time.Millisecond)
		p.app.ExecuteAndDraw(func() {
			p.successBtn.SetDisabled(false)
			p.failBtn.SetDisabled(false)
			p.unknownBtn.SetDisabled(false)
			p.backBtn.SetDisabled(false)
		})
	}()

	// Row 1: the three verdict buttons.
	verdictFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 1, false). // Spacer
		AddItem(p.successBtn, 0, 1, false).
		AddItem(tview.NewBox(), 2, 0, false).
		AddItem(p.unknownBtn, 0, 1, false).
		AddItem(tview.NewBox(), 2, 0, false).
		AddItem(p.failBtn, 0, 1, false).
		AddItem(tview.NewBox(), 0, 1, false) // Spacer

	// Row 2: the cancel action, centered. The spacers are weighted so the button
	// ends up roughly the same width as the verdict buttons above it.
	cancelFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 2, false).
		AddItem(p.backBtn, 0, 1, true).
		AddItem(tview.NewBox(), 0, 2, false)

	buttonsFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(verdictFlex, 3, 0, false).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(cancelFlex, 3, 0, true)

	p.SetDirection(tview.FlexRow).
		AddItem(widgets.NewHorizontalSeparator(tcell.ColorWhite), 1, 0, false).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(instructions, 0, 2, false).
		AddItem(buttonsFlex, 7, 0, true).
		AddItem(tview.NewBox(), 1, 0, false)

	p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			p.onCancel()
			return nil
		}

		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'a', 'A':
				p.onSuccess()
				return nil
			case 'd', 'D':
				p.onFailure()
				return nil
			case 's', 'S':
				p.onIndeterminate()
				return nil
			}
		}

		return event
	})

	return p
}

// GetActionPrompts returns the key actions for the test page.
func (p *TestPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{
		{Input: "ESC", Action: "Cancel"},
		{Input: "A", Action: "Works"},
		{Input: "S", Action: "Can't Tell"},
		{Input: "D", Action: "Broken"},
	}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status
func (p *TestPage) GetStatusPrimitive() *tview.TextView {
	return p.statusText
}

// GetFocusablePrimitives implements the Focusable interface for the MainPage.
func (p *TestPage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{
		p.successBtn,
		p.unknownBtn,
		p.failBtn,
		p.backBtn,
	}
}

// Update implements the Page interface.
func (p *TestPage) Update() {}
