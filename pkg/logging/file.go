package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ConfigureLogger creates a logger and optionally opens a file-based sink.
// When disableFile is true, the logger stays in-memory only and no file is opened.
func ConfigureLogger(appName string, prefix string, preferredDir string, disableFile bool) (*Logger, *os.File, string, error) {
	logger := NewLogger()
	if disableFile {
		SetDefault(logger)
		return logger, nil, "", nil
	}

	logFile, logPath, err := OpenLogFile(appName, prefix, preferredDir)
	if err != nil {
		return nil, nil, "", err
	}
	logger.SetWriter(logFile)
	SetDefault(logger)
	return logger, logFile, logPath, nil
}

// userLogDir returns the conventional per-user log directory for the
// current OS. It does not create the directory.
func userLogDir(appName string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Logs", appName), nil

	case "windows":
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			return filepath.Join(dir, appName, "Logs"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local", appName, "Logs"), nil

	default: // linux and other unix-likes
		if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
			return filepath.Join(dir, appName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state", appName), nil
	}
}

// candidateLogDirs returns, in priority order, the directories to try for
// storing log files: current working directory, the OS-conventional
// per-user log location, then the system temp directory as a last resort.
func candidateLogDirs(appName string) []string {
	var dirs []string

	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}

	if dir, err := userLogDir(appName); err == nil {
		dirs = append(dirs, dir)
	}

	dirs = append(dirs, filepath.Join(os.TempDir(), appName))

	return dirs
}

// OpenLogFile creates (if needed) and opens a fresh log file, trying the
// current working directory first, then the OS-conventional per-user log
// location, then the system temp directory. It returns the open file and
// the full path it ended up at.
func OpenLogFile(appName string, prefix string, preferredDir string) (f *os.File, path string, err error) {
	fileName := fmt.Sprintf("%s-%s.log", prefix, time.Now().Format("2006-01-02_15-04-05"))

	dirs := candidateLogDirs(appName)
	if preferredDir != "" {
		dirs = []string{preferredDir}
	}

	var errs []error
	for _, dir := range dirs {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			errs = append(errs, fmt.Errorf("mkdir %s: %w", dir, mkErr))
			continue
		}

		candidatePath := filepath.Join(dir, fileName)
		file, openErr := os.OpenFile(candidatePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
		if openErr != nil {
			errs = append(errs, openErr)
			continue
		}

		return file, candidatePath, nil
	}

	return nil, "", fmt.Errorf("could not open a log file in any candidate location: %w", errors.Join(errs...))
}
