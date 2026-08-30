package guiapp

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/screens"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

func (a *App) Update() {
	a.window.Invalidate()
}

func (a *App) OnLoadingStarted() {
	a.SetActiveScreen(a.loadingScreen)
}

func (a *App) OnLoadingProgress(fileName string, i int, count int) {
	a.loadingScreen.UpdateProgress(fileName, i, count)
	a.Update()
}

func (a *App) OnBisectionReady() {
	a.SetActiveScreen(a.modSelectionScreen)
}

func (a *App) OnUnresolvableMods(mods []ui.UnresolvableModInfo) {
	a.Run(func() {
		a.SetActiveScreen(screens.NewUnresolvableScreen(a, mods))
	})
}

func (a *App) OnInitialModStateSelection(initiallyDisabled []string) {
	a.Run(func() {
		screen := screens.NewSetupExcludedModsScreen(a, initiallyDisabled)
		a.SetActiveScreen(screen)
		if _, present := a.GetViewModel().Mods.Infos["crash_assistant"]; present {
			go func() {
				if a.ShowCrashAssistantDialog() {
					a.Run(func() { screen.KeepDisabled("crash_assistant") })
				}
			}()
		}
	})
}

func (a *App) OnTestReady() {
	a.Run(func() {
		a.mainScreen.ShowTestPrompt()
		a.Update()
	})
}

func (a *App) OnIterationComplete() {
	a.SetActiveScreen(a.resultScreen)
}

func (a *App) OnBisectionHalted(groupA, groupB sets.Set) {
	a.Run(func() {
		a.SetActiveScreen(screens.NewHaltScreen(a, groupA, groupB))
	})
}
