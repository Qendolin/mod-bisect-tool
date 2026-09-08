package mods

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

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
