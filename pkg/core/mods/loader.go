package mods

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
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

// LoadMods discovers mods, parses metadata, resolves basic conflicts, and builds provider maps.
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
		topLevelModMetadata, nestedModMetadata, err := ml.ExtractModMetadata(fullPath, baseFilename+".jar", &logBuffer)
		if err != nil {
			results <- processFileResult{baseFileName: baseFilename, parseError: fmt.Errorf("extracting metadata from %s: %w", task.fileEntry.Name(), err), logs: logBuffer}
			continue
		}

		currentMod := &Mod{
			Path:          fullPath,
			BaseFilename:  baseFilename,
			Metadata:      topLevelModMetadata,
			NestedModules: nestedModMetadata,
		}
		results <- processFileResult{
			mod:          currentMod,
			baseFileName: baseFilename,
			logs:         logBuffer,
		}
	}
}

// resolveModConflicts handles multiple JAR files providing the same mod ID, choosing a winner.
func (ml *ModLoader) resolveModConflicts(parsedFileResults []processFileResult, allMods map[string]*Mod) error {
	// Group all parsed results by the primary mod ID they represent.
	candidatesByID := make(map[string][]*Mod)
	for _, res := range parsedFileResults {
		modID := res.mod.Metadata.ID
		candidatesByID[modID] = append(candidatesByID[modID], res.mod)
	}

	var multiError []string
	var disabledDuplicates []string

	for modID, candidates := range candidatesByID {
		// Two candidates sharing a base filename are two states of the same file
		// (foo.jar vs foo.jar.disabled). The tool toggles a mod by its base stem,
		// so during a test foo.jar must be disable-able, but its rename target
		// is occupied by the disabled twin, and os.Rename would clobber it.
		// Relocating the disabled twin gives each file a distinct stem, turning
		// the pair into ordinary duplicate mods handled below.
		if err := ml.disambiguateSameStemDuplicates(candidates); err != nil {
			errMsg := fmt.Sprintf("error disambiguating same-stem duplicates for mod %s: %v", modID, err)
			logging.Error("ModLoader: " + errMsg)
			multiError = append(multiError, errMsg)
			// Best-effort: still pick a winner, but do not disable losers since
			// the ambiguous pair could not be separated safely.
			allMods[modID] = determineWinner(modID, candidates)
			continue
		}

		winner := determineWinner(modID, candidates)
		allMods[modID] = winner // Add ONLY the winner to the final mod map.

		// Now, handle disabling files for the losers.
		for _, loser := range candidates {
			if loser.Path == winner.Path {
				continue // Don't disable the winner.
			}
			// Only disable losers whose own file is currently active. A loser
			// that is already .jar.disabled needs no action (and re-disabling
			// it would rename the file onto itself). This restores the old
			// IsInitiallyActive gate without re-adding the field.
			if !ml.Adapter.IsEnabledPath(loser.Path) {
				continue
			}
			if err := ml.Adapter.Disable(loser.Path); err != nil {
				if os.IsNotExist(err) {
					continue // File vanished after parsing; effectively disabled already.
				}
				errMsg := fmt.Sprintf("error disabling non-winning duplicate '%s' (for mod %s): %v", loser.BaseFilename, modID, err)
				logging.Error("ModLoader: " + errMsg)
				multiError = append(multiError, errMsg)
			} else {
				disabledDuplicates = append(disabledDuplicates, filepath.Base(loser.Path))
			}
		}
	}

	if len(disabledDuplicates) > 0 {
		logging.Infof("ModLoader: Disabled %d non-winning duplicate active files: %s", len(disabledDuplicates), strings.Join(disabledDuplicates, ", "))
	}
	if len(multiError) > 0 {
		return fmt.Errorf("encountered errors during conflict resolution: %s", strings.Join(multiError, "; "))
	}
	return nil
}

// disambiguateSameStemDuplicates handles the case where a mod ID is provided by
// both an enabled file (foo.jar) and a disabled file (foo.jar.disabled) that
// share the same base filename. Because the tool toggles a mod by that stem
// (foo.jar <-> foo.jar.disabled), the two files cannot coexist: disabling
// foo.jar during a test would rename it onto the path the disabled twin already
// occupies, clobbering it. The disabled twin is therefore relocated to a
// distinct stem (foo-dup.jar.disabled) so the pair is handled as ordinary
// duplicate mods instead.
func (ml *ModLoader) disambiguateSameStemDuplicates(candidates []*Mod) error {
	byBase := make(map[string]*Mod)
	for _, c := range candidates {
		existing, ok := byBase[c.BaseFilename]
		if !ok {
			byBase[c.BaseFilename] = c
			continue
		}

		// Two files share the stem: exactly one is the enabled file and one the
		// disabled twin. Relocate the disabled one.
		disabledTwin := c
		if ml.Adapter.IsEnabledPath(c.Path) {
			disabledTwin = existing
		}
		if err := ml.relocateDisabledTwin(disabledTwin); err != nil {
			return err
		}
		// Keep whichever file is now associated with the stem in the map; the
		// relocated twin is no longer colliding.
		if disabledTwin == existing {
			byBase[c.BaseFilename] = c
		}
	}
	return nil
}

// relocateDisabledTwin renames a .jar.disabled file to a distinct -dup stem so
// it no longer collides with its enabled counterpart. It updates the mod's
// Path and BaseFilename in place.
func (ml *ModLoader) relocateDisabledTwin(mod *Mod) error {
	dir := filepath.Dir(mod.Path)
	base := ml.Adapter.BaseFilename(filepath.Base(mod.Path))

	newBase := base + "-dup"
	newPath := ml.Adapter.DisabledPath(filepath.Join(dir, newBase))
	for i := 2; ; i++ {
		enabled := ml.Adapter.EnabledPath(filepath.Join(dir, newBase))
		if _, eErr := os.Stat(enabled); os.IsNotExist(eErr) {
			if _, dErr := os.Stat(newPath); os.IsNotExist(dErr) {
				break
			}
		}
		newBase = fmt.Sprintf("%s-dup%d", base, i)
		newPath = ml.Adapter.DisabledPath(filepath.Join(dir, newBase))
	}

	if err := os.Rename(mod.Path, newPath); err != nil {
		return fmt.Errorf("renaming disabled duplicate '%s' to '%s': %w", mod.Path, newPath, err)
	}
	logging.Warnf("ModLoader: Renamed disabled duplicate '%s' to '%s' to disambiguate same-stem mod files.", mod.Path, newPath)
	mod.Path = newPath
	mod.BaseFilename = ml.Adapter.BaseFilename(filepath.Base(newPath))
	return nil
}

// determineWinner sorts candidates for the same mod ID and selects the best one.
// The priority is: Higher Version > Alphabetical Filename (as a stable tie-breaker).
func determineWinner(modID string, candidates []*Mod) *Mod {
	// This function is called when multiple files provide the same top-level mod ID.
	if len(candidates) == 1 {
		return candidates[0]
	}

	logging.Warnf("ModLoader: Found %d conflicting files for mod %s. Determining winner by version...", len(candidates), modID)

	// Sort the candidates slice in-place to find the best one.
	sort.Slice(candidates, func(i, j int) bool {
		// Rule 1: Higher version is higher priority.
		v1 := candidates[i].Metadata.Version.Version
		v2 := candidates[j].Metadata.Version.Version
		versionCmp := v1.Compare(v2)
		if versionCmp != 0 {
			return versionCmp > 0 // true if i > j, resulting in descending order.
		}

		// Rule 2 (Tie-breaker): Alphabetical base filename for deterministic order.
		return candidates[i].BaseFilename < candidates[j].BaseFilename
	})

	winner := candidates[0]
	logging.Infof("ModLoader: Winner for mod %s is v%s from file '%s'.",
		modID, winner.Metadata.Version.Version, winner.BaseFilename+".jar")

	return winner
}

// populateProviderMaps populates the potentialProviders map and the EffectiveProvides for each mod.
func populateProviderMaps(allMods map[string]*Mod, potentialProviders PotentialProvidersMap) {
	for _, mod := range allMods {
		mod.EffectiveProvides = make(map[string]version.Version)
		providerInfoBase := ProviderInfo{TopLevelModID: mod.Metadata.ID, TopLevelModVersion: mod.Metadata.Version.Version}

		addProvider(potentialProviders, mod.EffectiveProvides, mod.Metadata.ID, mod.Metadata.Version.Version, providerInfoBase, true)

		for _, p := range mod.Metadata.Provides {
			addProvider(potentialProviders, mod.EffectiveProvides, p, mod.Metadata.Version.Version, providerInfoBase, true)
		}

		for _, nested := range mod.NestedModules {
			nestedProviderInfo := providerInfoBase
			nestedProviderInfo.VersionOfProvidedItem = nested.Info.Version.Version
			addProvider(potentialProviders, mod.EffectiveProvides, nested.Info.ID, nested.Info.Version.Version, nestedProviderInfo, false)
			for _, p := range nested.Info.Provides {
				addProvider(potentialProviders, mod.EffectiveProvides, p, nested.Info.Version.Version, nestedProviderInfo, false)
			}
		}
	}
	sortAndLogProviders(allMods, potentialProviders)
}

// addProvider is a helper to add provider information to the relevant maps.
func addProvider(potentialProviders PotentialProvidersMap, effectiveProvides map[string]version.Version,
	providedID string, ver version.Version, baseInfo ProviderInfo, isDirect bool) {
	if ver == nil {
		return
	}
	updateEffectiveProvides(effectiveProvides, providedID, ver)

	providerInfo := baseInfo
	providerInfo.VersionOfProvidedItem = ver
	providerInfo.IsDirectProvide = isDirect
	addSingleProviderInfo(potentialProviders, providedID, providerInfo)
}

// sortAndLogProviders sorts all provider lists for determinism and logs them.
func sortAndLogProviders(allMods map[string]*Mod, potentialProviders PotentialProvidersMap) {
	var providerLogMessages []string
	sortedDepIDs := make([]string, 0, len(potentialProviders))
	for depID := range potentialProviders {
		sortedDepIDs = append(sortedDepIDs, depID)
	}
	sort.Strings(sortedDepIDs)

	for _, depID := range sortedDepIDs {
		infos := potentialProviders[depID]
		sortProviders(infos)
		potentialProviders[depID] = infos

		if IsImplicitMod(depID) {
			continue
		}
		if len(infos) == 1 && infos[0].IsDirectProvide && infos[0].TopLevelModID == depID {
			continue
		}
		providerLogMessages = append(providerLogMessages, formatProviderLog(depID, infos, allMods)...)
	}

	if len(providerLogMessages) > 0 {
		logging.Infof("ModLoader: Populated dependency providers for non-trivial dependencies:\n%s", strings.Join(providerLogMessages, "\n"))
	}
}

// formatProviderLog creates the log message lines for a given dependency and its providers.
func formatProviderLog(depID string, infos []ProviderInfo, allMods map[string]*Mod) []string {
	var messages []string
	if len(infos) == 1 {
		info := infos[0]
		providingMod := allMods[info.TopLevelModID]
		messages = append(messages, fmt.Sprintf("  - Dependency %s provided by %s (at v%s) from '%s'",
			depID, info.TopLevelModID, info.VersionOfProvidedItem, providingMod.BaseFilename+".jar"))
	} else {
		messages = append(messages, fmt.Sprintf("  - Dependency %s provided by:", depID))
		for _, info := range infos {
			providingMod := allMods[info.TopLevelModID]
			messages = append(messages, fmt.Sprintf("      - %s (at v%s) from '%s'",
				info.TopLevelModID, info.VersionOfProvidedItem, providingMod.BaseFilename+".jar"))
		}
	}
	return messages
}

// sortProviders sorts a slice of ProviderInfo for deterministic best-provider selection.
func sortProviders(infos []ProviderInfo) {
	if len(infos) < 2 {
		return
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].IsDirectProvide != infos[j].IsDirectProvide {
			return infos[i].IsDirectProvide
		}
		compItemVer := infos[i].VersionOfProvidedItem.Compare(infos[j].VersionOfProvidedItem)
		if compItemVer != 0 {
			return compItemVer > 0
		}
		return infos[i].TopLevelModVersion.Compare(infos[j].TopLevelModVersion) > 0
	})
}

// updateEffectiveProvides updates the effective provides map for a mod, prioritizing higher versions.
func updateEffectiveProvides(effectiveProvides map[string]version.Version, providedID string, ver version.Version) {
	if providedID == "" {
		return
	}
	if existingVersion, ok := effectiveProvides[providedID]; !ok || ver.Compare(existingVersion) > 0 {
		effectiveProvides[providedID] = ver
	}
}

func GetImplicitMods() []string {
	return []string{"java", "minecraft", "fabricloader", "quilt_loader", "neoforge", "forge"}
}

// addImplicitProvides adds common implicit dependencies to the potential providers map.
func addImplicitProvides(potentialProviders PotentialProvidersMap) {
	placeholderVersion, _ := version.Parse("0.0.0", false)

	for _, id := range GetImplicitMods() {
		potentialProviders[id] = append(potentialProviders[id], ProviderInfo{
			TopLevelModID:         id,
			VersionOfProvidedItem: placeholderVersion,
			IsDirectProvide:       true,
			TopLevelModVersion:    placeholderVersion,
		})
	}
}

// addSingleProviderInfo adds a single ProviderInfo to the potential providers map for a given ID.
func addSingleProviderInfo(potentialProviders PotentialProvidersMap, providedID string, info ProviderInfo) {
	if providedID == "" {
		return
	}
	potentialProviders[providedID] = append(potentialProviders[providedID], info)
}

// applyOverridesToLoadedMods applies a final, merged set of override rules.
func (ml *ModLoader) applyOverridesToLoadedMods(mods map[string]*Mod, overrides *DependencyOverrides) {
	if overrides == nil || len(overrides.Rules) == 0 {
		return
	}

	rulesByModID := make(map[string][]OverrideRule)
	for _, rule := range overrides.Rules {
		targetID := rule.Target()
		rulesByModID[targetID] = append(rulesByModID[targetID], rule)
	}

	foundTargets := make(map[string]struct{})

	for topLevelID, mod := range mods {
		if rules, ok := rulesByModID[topLevelID]; ok {
			logging.Infof("ModLoader: Applying %d override rule(s) to top-level mod %s.", len(rules), topLevelID)
			for _, rule := range rules {
				rule.Apply(&mod.Metadata)
				logging.Debugf("ModLoader:   - Applied rule: Target='%s', Field='%s', Key='%s', Action='%s', Value='%s'",
					rule.Target(), rule.Field(), rule.Key(), rule.Action().String(), rule.Value())
			}
			foundTargets[topLevelID] = struct{}{}
		}

		for i := range mod.NestedModules {
			nestedMod := &mod.NestedModules[i]
			if rules, ok := rulesByModID[nestedMod.Info.ID]; ok {
				logging.Infof("ModLoader: Applying %d override rule(s) to nested mod %s (within %s).", len(rules), nestedMod.Info.ID, topLevelID)
				for _, rule := range rules {
					rule.Apply(&nestedMod.Info)
					logging.Debugf("ModLoader:   - Applied rule: Target='%s', Field='%s', Key='%s', Action='%s', Value='%s'",
						rule.Target(), rule.Field(), rule.Key(), rule.Action().String(), rule.Value())
				}
				foundTargets[nestedMod.Info.ID] = struct{}{}
			}
		}
	}

	// Track unapplied targets by source
	unappliedBySource := make(map[OverrideSource]map[string]struct{})
	for targetID, rules := range rulesByModID {
		if _, found := foundTargets[targetID]; !found {
			// Use the source of the first rule targeting this mod
			source := rules[0].Source()
			if unappliedBySource[source] == nil {
				unappliedBySource[source] = make(map[string]struct{})
			}
			unappliedBySource[source][targetID] = struct{}{}
		}
	}

	// Report unapplied targets per source
	for source, unapplied := range unappliedBySource {
		if len(unapplied) > 0 {
			if source == OverrideSourceBuiltin {
				logging.Infof("ModLoader: Skipping builtin override rule(s) for unknown mod(s): %v", sets.FormatSet(unapplied))
			} else {
				logging.Warnf("ModLoader: Skipping override rule(s) for unknown mod(s) not found in any top-level or nested JAR: %v", sets.FormatSet(unapplied))
			}
		}
	}
}
