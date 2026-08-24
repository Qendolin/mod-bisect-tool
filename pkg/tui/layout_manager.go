package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// LayoutManager handles the overall visual structure of the application.
type LayoutManager struct {
	app        TUIApp
	root       *tview.Flex
	header     *tview.Flex
	status     *tview.Flex
	statusText *tview.TextView
	footer     *tview.Flex
	pages      *tview.Pages

	errorCounters    *tview.TextView
	prevErrorCount   int
	prevWarningCount int
}

// NewLayoutManager creates and initializes the UI layout manager.
func NewLayoutManager(app TUIApp, ctx context.Context) *LayoutManager {
	lm := &LayoutManager{
		app:              app,
		pages:            tview.NewPages(),
		root:             tview.NewFlex().SetDirection(tview.FlexRow),
		header:           tview.NewFlex(),
		footer:           tview.NewFlex(),
		errorCounters:    tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight),
		prevErrorCount:   -1,
		prevWarningCount: -1,
	}
	lm.setupLayout()
	go func() {
		defer logging.HandlePanic()
		lm.startErrorCounterPolling(ctx)
	}()
	return lm
}

// RootPrimitive returns the main primitive that should be set as the application's root.
func (lm *LayoutManager) RootPrimitive() tview.Primitive {
	return lm.root
}

// Pages returns the tview.Pages container for content.
func (lm *LayoutManager) Pages() *tview.Pages {
	return lm.pages
}

func (lm *LayoutManager) setupLayout() {
	lm.status = tview.NewFlex().SetDirection(tview.FlexRow)
	lm.SetHeader(nil)

	// Use boxes instead of padding to avoid transparent gap
	lm.header.AddItem(tview.NewBox(), 1, 0, false).
		AddItem(lm.status, 0, 1, false).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(lm.errorCounters, 30, 0, false).
		AddItem(tview.NewBox(), 1, 0, false)

	lm.root.SetBorder(true).
		SetTitle(" Mod Bisect Tool ").
		SetTitleAlign(tview.AlignLeft)

	lm.root.AddItem(lm.header, 1, 0, false).
		AddItem(lm.pages, 0, 1, true).
		AddItem(lm.footer, 1, 0, false)

	lm.SetErrorCounters(0, 0)
}

// startErrorCounterPolling starts a goroutine that periodically updates error/warning counters.
// It gracefully stops when the application's context is canceled.
func (lm *LayoutManager) startErrorCounterPolling(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	// Perform an initial update immediately
	lm.updateErrorCounters()

	for {
		select {
		case <-ticker.C:
			lm.updateErrorCounters()
		case <-ctx.Done():
			logging.Debugf("LayoutManager: Stopping error counter polling.")
			return
		}
	}
}

// updateErrorCounters fetches counts from the logger and updates the UI.
func (lm *LayoutManager) updateErrorCounters() {
	logger := lm.app.GetLogger()
	if logger == nil {
		return // Logger not set up yet.
	}

	entries := logger.Store().GetAll()
	errorCount := 0
	warningCount := 0

	for _, entry := range entries {
		switch entry.Level {
		case logging.LevelError:
			errorCount++
		case logging.LevelWarn:
			warningCount++
		}
	}

	// prevent unnecessary draws
	if lm.prevErrorCount != errorCount || lm.prevWarningCount != warningCount {
		lm.app.ExecuteAndDraw(func() {
			lm.SetErrorCounters(warningCount, errorCount)
		})
	}
}

// SetErrorCounters updates the error and warning counters.
func (lm *LayoutManager) SetErrorCounters(warnCount, errorCount int) {
	if lm.prevErrorCount == errorCount && lm.prevWarningCount == warnCount {
		return
	}
	lm.prevErrorCount = errorCount
	lm.prevWarningCount = warnCount

	warnBgColor := tcell.ColorYellow
	warnFgColor := tcell.ColorBlack
	errorBgColor := tcell.ColorRed
	errorFgColor := tcell.ColorBlack
	if warnCount == 0 {
		warnBgColor = tcell.ColorBlack
		warnFgColor = tcell.ColorWhite
	}
	if errorCount == 0 {
		errorBgColor = tcell.ColorBlack
		errorFgColor = tcell.ColorWhite
	}
	lm.errorCounters.SetText(fmt.Sprintf("[yellow]Warnings: [%s:%s]%d[-:-:-] [red]Errors: [%s:%s]%d[-:-:-]",
		warnFgColor.Name(), warnBgColor.Name(), warnCount, errorFgColor.Name(), errorBgColor.Name(), errorCount))
}

// SetFooter updates the action hints flexbox.
func (lm *LayoutManager) SetFooter(prompts []ActionPrompt) {
	lm.footer.Clear()
	if prompts == nil {
		return
	}
	globalPrompts := []ActionPrompt{{"Ctrl+C", "Quit"}, {"Ctrl+L", "Logs"}, {"Ctrl+H", "History"}, {"Tab", "Focus"}}
	allPrompts := append(globalPrompts, prompts...)

	var sb strings.Builder
	for i, prompt := range allPrompts {
		sb.WriteString(fmt.Sprintf("[darkcyan::b]%s[-:-:-]: %s", prompt.Input, prompt.Action))
		if i != len(allPrompts)-1 {
			sb.WriteString(" | ")
		}
	}
	lm.footer.AddItem(tview.NewTextView().SetDynamicColors(true).SetText(sb.String()), 0, 1, false)
}

// SetHeader updates the status bar
func (lm *LayoutManager) SetHeader(p *tview.TextView) {
	if p == nil {
		p = tview.NewTextView().SetDynamicColors(true)
	}
	lm.statusText = p
	lm.status.Clear()
	lm.status.AddItem(p, 0, 1, false)
}

func (lm *LayoutManager) SetStatusText(text string) {
	lm.statusText.SetText(text)
}
