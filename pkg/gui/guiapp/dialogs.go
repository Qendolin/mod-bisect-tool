package guiapp

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/ncruces/zenity"
)

func (a *App) ShowErrorDialog(title, message string, err error) {
	fullMsg := message
	if err != nil {
		fullMsg += "\n\n" + a.translator.Text("details", "Details: {{.Error}}", map[string]any{"Error": err.Error()})
	}
	opts := append(a.dialogOptions(), zenity.Title(title))
	_ = zenity.Error(fullMsg, opts...)
}

func (a *App) ShowInfoDialog(title, message, details string) {
	fullMsg := message
	if details != "" {
		fullMsg += "\n\n" + details
	}
	opts := append(a.dialogOptions(), zenity.Title(title))
	_ = zenity.Info(fullMsg, opts...)
}

func (a *App) ShowQuestionDialog(title, message, details string, initial bool) (ok bool) {
	fullMsg := message
	if details != "" {
		fullMsg += "\n\n" + details
	}
	opts := append(a.dialogOptions(), zenity.Title(title))
	if !initial {
		opts = append(opts, zenity.DefaultCancel())
	}
	err := zenity.Question(fullMsg, opts...)
	return err == nil
}

func (a *App) ShowCrashAssistantDialog() bool {
	return a.ShowQuestionDialog(a.translator.Text("crash_assistant_detected", "Crash Assistant Detected", nil), a.translator.Text("crash_assistant_disable_question", "Crash Assistant can slow down the search. Do you want to disable it?", nil), "", true)
}

// Dialogs (Blocking)
func (a *App) ShowDialogErrorModLoadingGeneric(path string, err error) {
	a.ShowErrorDialog(a.translator.Text("mod_loading_error", "Mod Loading Error", nil), a.translator.Text("failed_load_mods", "Failed to load mods from '{{.Path}}'", map[string]any{"Path": path}), err)
	a.SetActiveScreen(a.setupScreen)
}

func (a *App) ShowDialogErrorModLoadingNoMods(path string) {
	a.ShowErrorDialog(a.translator.Text("mod_loading_error", "Mod Loading Error", nil), a.translator.Text("no_mods_found", "No mods were found at '{{.Path}}'.\nPlease ensure that you've entered the path correctly.", map[string]any{"Path": path}), nil)
	a.SetActiveScreen(a.setupScreen)
}

func (a *App) ShowDialogErrorBisectionInitialization(err error) {
	a.ShowErrorDialog(a.translator.Text("initialization_error", "Initialization Error", nil), a.translator.Text("failed_initialize", "Failed to initialize the bisection!", nil), err)
	a.SetActiveScreen(a.setupScreen)
}

func (a *App) ShowDialogErrorBisectionCannotContinue(err error) {
	a.ShowErrorDialog(a.translator.Text("bisection_error", "Bisection Error", nil), a.translator.Text("cannot_continue", "Cannot continue the search!", nil), err)
}

func (a *App) ShowDialogErrorBisectionPrepare(err error) {
	a.ShowErrorDialog(a.translator.Text("bisection_error", "Bisection Error", nil), a.translator.Text("prepare_error", "An error occurred and the next step could not be prepared.\nIf another program, like Minecraft, is currently accessing your mods, please close it.\n\nPlease check the application log for details.", nil), err)
}

func (a *App) ShowDialogInfoBisectionModsMissingExpected(missingMods sets.Set) {
	a.ShowInfoDialog(
		a.translator.Text("problematic_mods_removed", "Known Problematic Mod(s) Removed", nil),
		a.translator.Text("problematic_mods_removed_message", "The following mod(s), which were part of a known conflict set, have been detected as missing. This is expected. The search will now proceed with the updated mod list.", nil),
		sets.FormatSet(missingMods).String(),
	)
}

func (a *App) ShowDialogInfoBisectionUnresolvableModsDisabled(disabledMods sets.Set) {
	a.ShowInfoDialog(
		a.translator.Text("disabled_mods", "Disabled Mods", nil),
		a.translator.Text("disabled_mods_message", "The following mods were automatically disabled due to unmet dependencies:", nil),
		sets.FormatSet(disabledMods).String(),
	)
}

func (a *App) ShowDialogQuestionBisectionContinueWithMissingMods(missingMods sets.Set) bool {
	return a.ShowQuestionDialog(
		a.translator.Text("missing_mod_files", "Missing Mod Files Detected", nil),
		a.translator.Text("missing_mod_files_message", "The following mod files were unexpectedly missing. Do you want to continue the search without them?", nil),
		sets.FormatSet(missingMods).String(),
		true,
	)
}
