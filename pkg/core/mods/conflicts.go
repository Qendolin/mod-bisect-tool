package mods

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

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
