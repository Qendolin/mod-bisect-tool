package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Qendolin/mod-bisect-tool/pkg/app"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/Qendolin/mod-bisect-tool/pkg/tui/tuiapp"
)

func main() {
	defer logging.HandlePanic()

	var a *app.App
	var tuiApp *tuiapp.App

	// A panic in any goroutine restores the mods state and exits. Started before
	// the app is created so early panics are still handled.
	go func() {
		for p := range logging.PanicChannel {
			if tuiApp != nil {
				tuiApp.Stop()
			}
			if a != nil {
				a.RestoreInitialModState()
			}
			fmt.Fprintf(os.Stderr, "panic: %v\n%s", p.Value, string(p.Stack))
			os.Exit(2)
		}
	}()

	cliArgs := app.ParseCLIArgs()

	// 1. Setup logging first.
	mainLogger, logFile, logPath, err := logging.ConfigureLogger(app.AppCommonName, app.AppTuiName, cliArgs.LogDir, cliArgs.NoLogFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal: could not open a log file: %v\n", err)
		os.Exit(1)
	}
	if logFile != nil {
		defer logFile.Close()
		fmt.Fprintf(os.Stderr, "Logging to %s\n", logPath)
	}
	if cliArgs.Verbose {
		mainLogger.SetDebug(true)
		logging.Infof("Main: Verbose logging enabled.")
	}
	logStartupInfo()

	// 2. Create the App structure, passing the configured logger.
	a = app.NewApp(mainLogger, cliArgs)
	tuiApp = tuiapp.NewApp(a, mainLogger)
	a.SetView(tuiApp)

	// 3. Register shutdown triggers before starting so no event is missed.
	// Interactive signals (Ctrl+C) show the quit dialog; closing the console
	// window (Windows CTRL_CLOSE_EVENT) skips the dialog and restores the mods
	// state immediately, since nobody can answer it.
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)
	consoleClosed := make(chan struct{})
	installConsoleCloseHandler(func() { close(consoleClosed) })
	go handleShutdown(a, tuiApp, shutdownCh, consoleClosed)

	// 4. Run the application.
	logging.Infof("Main: Application starting up.")
	if err := tuiApp.Start(); err != nil {
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
}

// handleShutdown reacts to termination triggers. A closed console window
// requires immediate cleanup; an interactive signal shows the quit dialog.
func handleShutdown(a *app.App, tuiApp *tuiapp.App, shutdownCh chan os.Signal, consoleClosed chan struct{}) {
	for {
		select {
		case <-consoleClosed:
			logging.Info("Main: Console window closed, restoring mod state and exiting.")
			a.RestoreInitialModState()
			os.Exit(0)
		case <-shutdownCh:
			tuiApp.ExecuteAndDraw(func() {
				tuiApp.Dialogs().ShowQuitDialog()
			})
		}
	}
}

// logStartupInfo writes diagnostics about the current environment to the log.
func logStartupInfo() {
	wd, err := os.Getwd()
	if err != nil {
		logging.Errorf("Main: Failed to get current working directory: %v", err)
	} else {
		logging.Infof("Main: Current Working Directory: %s", wd)
	}
	logging.Infof("%s", app.StartupInfo())
}
