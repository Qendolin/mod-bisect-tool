package pages

import (
	"fmt"
	"path/filepath"
	"strings"

	apppkg "github.com/Qendolin/mod-bisect-tool/pkg/app"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/probe"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/widgets"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PageSetupID is the unique identifier for the SetupPage.
const PageSetupID = "setup_page"

// loaderChoices lists the loader options in display order.
var loaderChoices = mods.SupportedRunLoaders()

// SetupPage represents the initial setup screen.
type SetupPage struct {
	*tview.Flex
	app        tui.TUIApp
	statusText *tview.TextView

	inputField         *tview.InputField
	loaderDropDown     *tview.DropDown
	loadButton         *tview.Button
	quitButton         *tview.Button
	userSelectedLoader bool

	// probeWorker serializes directory probes (one at a time, queued).
	probeWorker *probe.Worker
}

// NewSetupPage creates a new SetupPage instance.
func NewSetupPage(app tui.TUIApp) *SetupPage {
	p := &SetupPage{
		Flex:        tview.NewFlex().SetDirection(tview.FlexRow),
		app:         app,
		statusText:  tview.NewTextView().SetDynamicColors(true),
		probeWorker: probe.NewWorker(),
	}

	vm := app.GetViewModel()

	p.inputField = tview.NewInputField().
		SetLabel("Mods Directory Path: ").
		SetFieldWidth(0)
	p.inputField.SetPlaceholder("C:\\Users\\Example\\.minecraft\\mods").
		SetFieldTextColor(tcell.ColorBlack).
		SetPlaceholderTextColor(tcell.ColorGray)
	p.inputField.SetFocusFunc(func() {
		p.inputField.SetFieldBackgroundColor(tcell.ColorBlue)
	})
	p.inputField.SetBlurFunc(func() {
		p.inputField.SetFieldBackgroundColor(tcell.ColorSlateGray)
	})
	p.inputField.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			p.app.SetFocus(p.loadButton)
		}
	})
	p.inputField.SetChangedFunc(func(text string) {
		p.probeLoader(text)
	})

	loaderLabels := make([]string, len(loaderChoices))
	for i, choice := range loaderChoices {
		loaderLabels[i] = choice.String()
	}
	p.loaderDropDown = tview.NewDropDown().
		SetLabel("Mod Loader: ").
		SetOptions(loaderLabels, func(text string, index int) {
			if p.loaderDropDown != nil && p.loaderDropDown.HasFocus() {
				p.userSelectedLoader = true
			}
		}).
		SetCurrentOption(0)

	// A loader forced via the command line preselects the option and is not
	// overridden by the probe.
	if cliLoader := vm.Loader.Preferred; cliLoader != "" {
		if idx, ok := loaderIndex(cliLoader); ok {
			p.loaderDropDown.SetCurrentOption(idx)
			p.userSelectedLoader = true
		}
	}

	p.loadButton = tview.NewButton("Load Mods").SetSelectedFunc(func() {
		cleaned := strings.TrimSpace(p.inputField.GetText())
		cleaned = strings.TrimPrefix(cleaned, "\"")
		cleaned = strings.TrimPrefix(cleaned, "'")
		cleaned = strings.TrimSuffix(cleaned, "\"")
		cleaned = strings.TrimSuffix(cleaned, "'")
		cleaned = strings.TrimSpace(cleaned)
		if cleaned == "" {
			app.Dialogs().ShowErrorDialog("Error", "The mods path cannot be empty.", nil, nil)
			return
		}
		loader := p.selectedLoader()
		app.StartLoadingProcess(probe.ResolveModsDir(filepath.Clean(cleaned)), loader)
	})
	widgets.DefaultStyleButton(p.loadButton)

	p.quitButton = tview.NewButton("Quit").SetSelectedFunc(func() {
		app.ExecuteAndDraw(func() { app.Dialogs().ShowQuitDialog() })
	})
	widgets.DefaultStyleButton(p.quitButton)

	buttonsFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(p.loadButton, 30, 0, true).
		AddItem(nil, 0, 1, false).
		AddItem(p.quitButton, 30, 0, true)

	setupFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(p.inputField, 1, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(p.loaderDropDown, 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(buttonsFlex, 3, 0, false)
	setupFlex.SetBorderPadding(1, 1, 1, 1)

	buildInfo := apppkg.VersionText()

	instructions := tview.NewTextView().
		SetDynamicColors(true).
		SetText(fmt.Sprintf(`
[::b]Instructions:[-:-:-]
  - Enter the path to your Minecraft mods folder.
  - Paste path: [darkcyan::b]Ctrl+V[-:-:-] or [darkcyan::b]Shift+Right Click[-:-:-] (in most terminals).

[::b]Tool Information:[-:-:-]
  - Build: %s
  - Author: Qendolin
  - Source: https://github.com/Qendolin/mod-bisect-tool
`, buildInfo))
	instructions.SetBorderPadding(0, 0, 1, 1)

	p.AddItem(widgets.NewTitleFrame(setupFlex, "Setup"), 10, 0, true).
		AddItem(widgets.NewTitleFrame(instructions, "Info"), 0, 1, false)

	p.statusText.SetText("Welcome to the Mod Bisect Tool by Qendolin! Paste the path to your 'mods' directory below.")

	return p
}

// GetActionPrompts returns the key actions for the setup page.
func (p *SetupPage) GetActionPrompts() []tui.ActionPrompt {
	return []tui.ActionPrompt{}
}

// GetStatusPrimitive returns the tview.Primitive that displays the page's status
func (p *SetupPage) GetStatusPrimitive() *tview.TextView {
	return p.statusText
}

func (p *SetupPage) GetFocusablePrimitives() []tview.Primitive {
	return []tview.Primitive{
		p.inputField,
		p.loaderDropDown,
		p.loadButton,
		p.quitButton,
	}
}

// Update implements the Page interface.
func (p *SetupPage) Update() {}

// loaderIndex returns the index of a RunLoader in loaderChoices, if present.
func loaderIndex(loader mods.RunLoader) (int, bool) {
	for i, choice := range loaderChoices {
		if choice == loader {
			return i, true
		}
	}
	return 0, false
}

// selectedLoader returns the loader to load with: the user's selection.
func (p *SetupPage) selectedLoader() mods.RunLoader {
	idx, label := p.loaderDropDown.GetCurrentOption()
	if label != "" && idx >= 0 && idx < len(loaderChoices) {
		return loaderChoices[idx]
	}
	return mods.RunLoaderFabric
}

// probeLoader queues a probe of the given path, updating the recommended loader
// unless the user has made a manual selection. Probes run one at a time.
func (p *SetupPage) probeLoader(path string) {
	path = probe.ResolveModsDir(path)
	p.probeWorker.Request(path, func(res probe.ProbeResult) {
		p.app.ExecuteAndDraw(func() {
			if !p.userSelectedLoader && res.PrimaryLoader != "" {
				if idx, ok := loaderIndex(res.PrimaryLoader); ok {
					p.loaderDropDown.SetCurrentOption(idx)
				}
			}
		})
	})
}
