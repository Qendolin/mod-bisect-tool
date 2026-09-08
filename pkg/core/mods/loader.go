package mods

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// ModLoader loads mod information from the filesystem, parses metadata,
// resolves conflicts, and builds dependency provider maps.
type ModLoader struct {
	ModParser
	Adapter *FileAdapter
}

// bufferedLog represents a log message captured by a worker for later printing.
// It is defined here as it is an internal implementation detail of the concurrent loader.
type bufferedLog struct {
	Level   logging.LogLevel
	Message string
}

// logBuffer is a slice of bufferedLogs with a helper method for appending.
// It is created once per top-level JAR and shared across the entire nested
// subtree, so per-subtree guards (e.g. warnedFallback) stay consistent.
type logBuffer struct {
	entries []bufferedLog
	// warnedFallback records whether the "(Neo)Forge parsing is enabled but ..."
	// warning has already been emitted for this subtree.
	warnedFallback bool
}

// add formats and appends a new log entry to the buffer.
func (b *logBuffer) add(level logging.LogLevel, format string, v ...interface{}) {
	b.entries = append(b.entries, bufferedLog{Level: level, Message: fmt.Sprintf(format, v...)})
}

// task to be processed by a worker goroutine.
type processFileTask struct {
	fileEntry os.DirEntry
}

// result of a worker goroutine processing a single file.
type processFileResult struct {
	mod          *Mod
	parseError   error
	baseFileName string
	logs         logBuffer // Use the new logBuffer type.
}

type ModLoadingProgressCallback = func(fileName string, i, count int)

// LoadMods discovers mods, parses metadata, resolves conflicts, and builds provider maps.
func (ml *ModLoader) LoadMods(modsDir string, overrides *DependencyOverrides, progressReport ModLoadingProgressCallback) (
	map[string]*Mod, PotentialProvidersMap, error,
) {
	if ml.Adapter == nil {
		return nil, nil, fmt.Errorf("ModLoader: Adapter is required")
	}
	if ml.RunLoader == "" {
		return nil, nil, fmt.Errorf("ModLoader: no mod loader selected")
	}
	logging.Infof("ModLoader: Loading mods with loader: %s.", ml.RunLoader.String())

	potentialProviders := make(PotentialProvidersMap)
	addImplicitProvides(potentialProviders)

	diskFiles, err := os.ReadDir(modsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading mods directory %s: %w", modsDir, err)
	}

	filesToProcess := ml.filterJarFiles(diskFiles)
	if len(filesToProcess) == 0 {
		logging.Infof("ModLoader: No mod files found in %s", modsDir)
		return make(map[string]*Mod), potentialProviders, nil
	}

	parsedFileResults := ml.parseJarFilesConcurrently(filesToProcess, modsDir, progressReport)

	// allMods contains only the winning top-level mod for each top-level ID.
	allMods := make(map[string]*Mod)
	if err := ml.resolveModConflicts(parsedFileResults, allMods); err != nil {
		logging.Errorf("ModLoader: Error during mod conflict resolution: %v. Proceeding with available mods.", err)
	}

	if overrides != nil && len(overrides.Rules) > 0 {
		logging.Info("ModLoader: Applying dependency overrides...")
		ml.applyOverridesToLoadedMods(allMods, overrides)
		logging.Info("ModLoader: Dependency overrides applied.")
	}

	populateProviderMaps(allMods, potentialProviders)

	logging.Infof("ModLoader: Finished loading. Total %d mods loaded. %d potential capabilities provided.", len(allMods), len(potentialProviders))

	return allMods, potentialProviders, nil
}

// filterJarFiles returns a slice of os.DirEntry for files ending with .jar or .jar.disabled.
func (ml *ModLoader) filterJarFiles(diskFiles []os.DirEntry) []os.DirEntry {
	var filesToProcess []os.DirEntry
	for _, file := range diskFiles {
		if file.IsDir() {
			continue
		}
		filename := file.Name()
		if ml.Adapter.IsValidPath(filename) {
			filesToProcess = append(filesToProcess, file)
		}
	}
	return filesToProcess
}

// parseJarFilesConcurrently processes JAR files in parallel to extract mod metadata.
func (ml *ModLoader) parseJarFilesConcurrently(filesToProcess []os.DirEntry, modsDir string, progressReport ModLoadingProgressCallback) []processFileResult {
	numFiles := len(filesToProcess)
	if numFiles == 0 {
		return nil
	}
	numWorkers := min(numFiles, runtime.NumCPU())

	tasks := make(chan processFileTask, numWorkers*2)
	results := make(chan processFileResult, numWorkers*2)
	progressChan := make(chan processFileTask, numWorkers*2)
	// progressDone is closed once the progress reporter has drained progressChan,
	// guaranteeing every progress callback is delivered before LoadMods returns.
	progressDone := make(chan struct{})
	var wg sync.WaitGroup
	var progress atomic.Int32

	go func() {
		defer close(progressDone)
		for task := range progressChan {
			if progressReport != nil {
				progressReport(task.fileEntry.Name(), int(progress.Add(1)-1), numFiles)
			}
		}
	}()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer logging.HandlePanic()
			ml.jarProcessingWorker(modsDir, &wg, tasks, progressChan, results)
		}()
	}

	go func() {
		defer logging.HandlePanic()
		for _, file := range filesToProcess {
			tasks <- processFileTask{fileEntry: file}
		}
		close(tasks)
		wg.Wait()
		close(results)
		close(progressChan)
	}()

	var collectedResults []processFileResult
	for res := range results {
		// Drain the log buffer from the worker first. This ensures log messages
		// appear before the final status message for that file.
		for _, logEntry := range res.logs.entries {
			switch logEntry.Level {
			case logging.LevelDebug:
				logging.Debug(logEntry.Message)
			case logging.LevelInfo:
				logging.Info(logEntry.Message)
			case logging.LevelWarn:
				logging.Warn(logEntry.Message)
			case logging.LevelError:
				logging.Error(logEntry.Message)
			}
		}

		if res.parseError != nil {
			logging.Warnf("ModLoader: Failed to load mod metadata from file '%s.jar': %v", res.baseFileName, res.parseError)
			continue
		}
		if res.mod != nil {
			ml.logParsedFile(res)
			collectedResults = append(collectedResults, res)
		}
	}
	// Wait for the progress reporter to flush all callbacks before returning,
	// so loading completion is never reported before the progress that led to it.
	<-progressDone
	return collectedResults
}

// logParsedFile handles the logging for a single successfully parsed file result.
func (ml *ModLoader) logParsedFile(res processFileResult) {
	currentMod := res.mod
	nestedMods := res.mod.NestedModules

	logging.Infof("ModLoader: ├─ Mod %s (%s v%s) from file '%s.jar'",
		currentMod.Metadata.ID, currentMod.FriendlyName(), currentMod.Metadata.Version,
		res.baseFileName)

	for i, nested := range nestedMods {
		treeSymbol := "├"
		if i == len(nestedMods)-1 {
			treeSymbol = "└"
		}
		logging.Infof("ModLoader: │   %s─ Mod %s (%s v%s) provided by %s from '%s'.",
			treeSymbol, nested.Info.ID, nested.Info.Name, nested.Info.Version, currentMod.Metadata.ID, nested.PathInJar)
	}
}

// jarProcessingWorker is a goroutine worker that processes file tasks.
func (ml *ModLoader) jarProcessingWorker(modsDir string, wg *sync.WaitGroup, tasks <-chan processFileTask, progress chan<- processFileTask, results chan<- processFileResult) {
	defer wg.Done()
	for task := range tasks {
		if progress != nil {
			// Non blocking
			select {
			case progress <- task:
			default:
			}
		}

		fullPath := filepath.Join(modsDir, task.fileEntry.Name())
		baseFilename := ml.Adapter.BaseFilename(task.fileEntry.Name())

		// Create a log buffer for this specific task.
		var logBuffer logBuffer
		topLevelModMetadata, nestedModMetadata, classIndex, err := ml.ExtractModMetadata(fullPath, baseFilename+".jar", &logBuffer)
		if err != nil {
			results <- processFileResult{baseFileName: baseFilename, parseError: fmt.Errorf("extracting metadata from %s: %w", task.fileEntry.Name(), err), logs: logBuffer}
			continue
		}

		currentMod := &Mod{
			Path:          fullPath,
			BaseFilename:  baseFilename,
			Metadata:      topLevelModMetadata,
			NestedModules: nestedModMetadata,
			ClassIndex:    classIndex,
		}
		results <- processFileResult{
			mod:          currentMod,
			baseFileName: baseFilename,
			logs:         logBuffer,
		}
	}
}
