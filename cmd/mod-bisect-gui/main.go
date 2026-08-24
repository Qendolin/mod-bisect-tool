package main

import (
	"fmt"
	"os"

	gioapp "gioui.org/app"
	"github.com/Qendolin/mod-bisect-tool/pkg/app"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/guiapp"
	guii18n "github.com/Qendolin/mod-bisect-tool/pkg/gui/i18n"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/ncruces/zenity"
)

func main() {
	defer logging.HandlePanic()

	var a *app.App
	var guiApp *guiapp.App

	go func() {
		defer logging.HandlePanic()

		go func() {
			for p := range logging.PanicChannel {
				if guiApp != nil {
					guiApp.Stop()
				}
				if a != nil {
					a.RestoreInitialModState()
				}
				fmt.Fprintf(os.Stderr, "panic: %v\n%s", p.Value, string(p.Stack))
				os.Exit(2)
			}
		}()

		cliArgs := app.ParseCLIArgs()

		mainLogger := logging.NewLogger()

		logFile, logPath, err := logging.OpenLogFile(app.AppCommonName, app.AppGuiName, cliArgs.LogDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fatal: could not open a log file: %v\n", err)
			zenity.Error(err.Error(), zenity.Title("Failed to create log file"))
			os.Exit(1)
		}
		defer logFile.Close()
		fmt.Fprintf(os.Stderr, "Logging to %s\n", logPath)

		mainLogger.SetWriter(logFile)
		logging.SetDefault(mainLogger)

		if cliArgs.Verbose {
			mainLogger.SetDebug(true)
			logging.Infof("Main: Verbose logging enabled.")
		}
		logStartupInfo()

		a = app.NewApp(mainLogger, cliArgs)
		locale := cliArgs.Locale
		if locale == "" {
			locale = guii18n.DetectLocale()
		}
		guiApp = guiapp.NewApp(a, mainLogger, locale)
		a.SetView(guiApp)

		logging.Infof("Main: Application starting up.")
		if err := guiApp.Start(); err != nil {
			logging.Errorf("Main: Application exited with error: %v", err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			a.RestoreInitialModState()
			os.Exit(1)
		}
		a.RestoreInitialModState()

		if a.IsBisectionReady() {
			finalReport := app.GenerateLogReport(
				a.GetViewModel(),
				a.GetExecutionLogViewModel(),
				a.GetResultViewModel(),
			)
			logging.Infof("\n===== Bisection Report =====\n\n%s", finalReport)
		}

		logging.Infof("Main: Application exited gracefully.")
		os.Exit(0)
	}()

	gioapp.Main()
}

func logStartupInfo() {
	wd, err := os.Getwd()
	if err != nil {
		logging.Errorf("Main: Failed to get current working directory: %v", err)
	} else {
		logging.Infof("Main: Current Working Directory: %s", wd)
	}
	logging.Infof("%s", app.StartupInfo())
}
