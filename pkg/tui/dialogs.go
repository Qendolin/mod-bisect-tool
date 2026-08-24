package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type DialogManager struct {
	app TUIApp
}

func NewDialogManager(app TUIApp) *DialogManager {
	return &DialogManager{app: app}
}

// ShowErrorDialog displays a modal dialog with an error message.
func (m *DialogManager) ShowErrorDialog(title, message string, err error, onDismiss func()) {
	modal := widgets.NewRichModal().
		SetCenteredText(message).
		AddButtons([]string{"Dismiss"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			m.app.ExecuteAndDraw(func() {
				m.app.Navigation().CloseModal()
				if onDismiss != nil {
					onDismiss()
				}
			})
		})
	if err != nil {
		modal.SetDetailsText(tview.Escape(formatErrorChain(err)))
	}
	modal.SetTextColor(tcell.ColorWhite).
		SetBackgroundColor(tcell.ColorDarkRed).
		SetTitleColor(tcell.ColorWhite).
		SetBorderColor(tcell.ColorWhite)
	modal.Box.SetBackgroundColor(tcell.ColorDarkRed)
	modal.SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft)
	m.app.Navigation().ShowModal("error_dialog", NewModalPage(modal))
}

// ShowQuitDialog displays a confirmation dialog before quitting.
func (m *DialogManager) ShowQuitDialog() {
	modal := widgets.NewRichModal().
		SetCenteredText("Are you sure you want to quit?").
		AddButtons([]string{"Cancel", "Quit"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			m.app.ExecuteAndDraw(func() {
				m.app.Navigation().CloseModal()
				switch buttonIndex {
				case 1:
					logging.Info("App: Quitting.")
					m.app.Stop()
				case 0:
				}
			})
		})
	modal.SetTextColor(tcell.ColorBlack).
		SetTitleColor(tcell.ColorBlack).
		SetBorderColor(tcell.ColorWhite)
	modal.SetTitle(" Quit ").SetTitleAlign(tview.AlignLeft)
	m.app.Navigation().ShowModal("quit_dialog", NewModalPage(modal))
}

func (m *DialogManager) ShowQuestionDialog(title, question, details string, initial bool, onYes func(), onNo func()) {
	modal := widgets.NewRichModal().
		SetCenteredText(question).
		AddButtons([]string{"No", "Yes"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			m.app.ExecuteAndDraw(func() {
				m.app.Navigation().CloseModal()
				switch buttonLabel {
				case "Yes":
					if onYes != nil {
						onYes()
					}
				case "No":
					if onNo != nil {
						onNo()
					}
				}
			})
		})
	if initial {
		modal.SetFocus(1)
	}
	if details != "" {
		modal.SetDetailsText(details)
	}
	modal.SetTextColor(tcell.ColorBlack).
		SetTitleColor(tcell.ColorBlack).
		SetBorderColor(tcell.ColorWhite)
	modal.SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft)
	m.app.Navigation().ShowModal("yes_no_dialog", NewModalPage(modal))
}

// ShowInfoDialog displays a modal dialog with a neutral informational message.
func (m *DialogManager) ShowInfoDialog(title, message, details string, onDismiss func()) {
	modal := widgets.NewRichModal().
		SetCenteredText(message).
		AddButtons([]string{"Dismiss"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			m.app.ExecuteAndDraw(func() {
				m.app.Navigation().CloseModal()
				if onDismiss != nil {
					onDismiss()
				}
			})
		})
	if details != "" {
		modal.SetDetailsText(details)
	}
	modal.SetTextColor(tcell.ColorBlack).
		SetTitleColor(tcell.ColorBlack).
		SetBorderColor(tcell.ColorWhite)
	modal.SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft)
	m.app.Navigation().ShowModal("info_dialog", NewModalPage(modal))
}

// ModalPage is a simple wrapper around a tview.Modal to conform to the Page interface.
type ModalPage struct {
	*widgets.RichModal
}

// NewModalPage creates a new ModalPage.
func NewModalPage(modal *widgets.RichModal) *ModalPage {
	return &ModalPage{RichModal: modal}
}

// GetActionPrompts returns an empty map as modals have their own buttons.
func (p *ModalPage) GetActionPrompts() []ActionPrompt {
	return []ActionPrompt{}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status
func (p *ModalPage) GetStatusPrimitive() *tview.TextView {
	return nil
}

// Update implements the Page interface.
func (p *ModalPage) Update() {}

// formatErrorChain unwraps a chain of Go errors and formats them
// into a multi-line string, with each level of the error on a new line.
// This is ideal for displaying detailed error messages in a UI.
func formatErrorChain(err error) string {
	var b strings.Builder
	indent := ""
	for err != nil {
		next := errors.Unwrap(err)
		msg := err.Error()
		if next != nil {
			nextMsg := next.Error()
			if i := strings.LastIndex(msg, nextMsg); i > 0 {
				msg = strings.TrimSpace(msg[:i])
			}
		}
		fmt.Fprintf(&b, "%s- %s", indent, msg)
		if next != nil {
			b.WriteRune('\n')
		}
		indent += " "
		err = next
	}

	return b.String()
}
