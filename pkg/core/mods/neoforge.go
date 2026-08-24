package mods

import (
	"bufio"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/pelletier/go-toml/v2"
)

// neoForgeModsToml defines the structure for unmarshaling neoforge.mods.toml.
type neoForgeModsToml struct {
	Mods         []neoForgeMod                   `toml:"mods"`
	Dependencies map[string][]neoForgeDependency `toml:"dependencies"`
	// ModProperties holds the top-level [modproperties.<modId>] tables. Values
	// are free-form; Sinytra Connector uses the "fabric:provides" key to declare
	// provided Fabric API modules.
	ModProperties map[string]map[string]any `toml:"modproperties"`
	// Properties holds the top-level [properties] table, used by Sinytra
	// Connector placeholder mods to declare "connector:placeholder".
	Properties map[string]any `toml:"properties"`
}

// isConnectorPlaceholder reports whether the manifest is a Sinytra Connector
// placeholder declaring [properties] "connector:placeholder" = true. Such jars
// are Fabric mods whose forge manifest only exists so FML surfaces a
// missing-Connector dependency to users.
func (t *neoForgeModsToml) isConnectorPlaceholder() bool {
	flag, ok := t.Properties["connector:placeholder"].(bool)
	return ok && flag
}

// connectorProvides returns the "fabric:provides" list declared for modID under
// [modproperties.<modID>].
func (t *neoForgeModsToml) connectorProvides(modID string) []string {
	properties, ok := t.ModProperties[modID]
	if !ok {
		return nil
	}
	raw, ok := properties["fabric:provides"]
	if !ok {
		return nil
	}
	values, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	provides := make([]string, 0, len(values))
	for _, value := range values {
		if id, ok := value.(string); ok {
			provides = append(provides, id)
		}
	}
	return provides
}

// convertConnectorNeoForgeToml converts a (Neo)Forge manifest the way Sinytra
// Connector reads it: the regular conversion, plus the "fabric:provides" lists
// that Connector declares via [modproperties.<modId>] folded into the mod's
// provides (deduplicated against the [[mods]] provides).
func convertConnectorNeoForgeToml(tomlData *neoForgeModsToml, manifestPath, jarIdentifier string, jar *zipIndex, logBuffer *logBuffer) (ModMetadata, error) {
	metadata, err := convertNeoForgeToml(tomlData, manifestPath, jarIdentifier, jar, logBuffer)
	if err != nil {
		return ModMetadata{}, err
	}
	for _, mod := range tomlData.Mods {
		for _, provided := range tomlData.connectorProvides(mod.ModID) {
			if !slices.Contains(metadata.Provides, provided) {
				metadata.Provides = append(metadata.Provides, provided)
			}
		}
	}
	return metadata, nil
}

// neoForgeMod represents a single [[mods]] entry.
type neoForgeMod struct {
	ModID       string   `toml:"modId"`
	Version     string   `toml:"version"`
	DisplayName string   `toml:"displayName"`
	Provides    []string `toml:"provides"`
}

// neoForgeDependency represents a single dependency entry.
type neoForgeDependency struct {
	ModID string `toml:"modId"`
	Type  string `toml:"type"`
	// Mandatory is used by legacy Forge mods.toml, where dependencies declare
	// "mandatory" instead of "type". A pointer distinguishes an explicit
	// "mandatory = false" from an omitted field (which defaults to required).
	Mandatory    *bool  `toml:"mandatory"`
	VersionRange string `toml:"versionRange"`
}

// jarJarMetadata defines the structure for unmarshaling META-INF/jarjar/metadata.json.
type jarJarMetadata struct {
	Jars []struct {
		Path string `json:"path"`
	} `json:"jars"`
}

// neoForgeManifests lists the manifest files a (Neo)Forge mod may declare, in
// preference order: the modern neoforge.mods.toml before the legacy Forge
// mods.toml (1.20.1 and earlier).
var neoForgeManifests = []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"}

// hasNeoForgeManifest reports whether the jar declares a (Neo)Forge manifest,
// regardless of the active loader.
func hasNeoForgeManifest(jar *zipIndex) bool {
	for _, manifestPath := range neoForgeManifests {
		if jar.File(manifestPath) != nil {
			return true
		}
	}
	return false
}

// tryDecodeNeoForgeToml finds and decodes the jar's (Neo)Forge manifest
// (neoforge.mods.toml, falling back to the legacy Forge mods.toml). It returns
// nil when the jar declares no such manifest.
func tryDecodeNeoForgeToml(jar *zipIndex, jarIdentifier string) (*neoForgeModsToml, string, error) {
	for _, manifestPath := range neoForgeManifests {
		manifestFile := jar.File(manifestPath)
		if manifestFile == nil {
			continue
		}
		data, err := readZipFileEntry(manifestFile)
		if err != nil {
			return nil, manifestPath, fmt.Errorf("reading %s from %s: %w", manifestPath, jarIdentifier, err)
		}
		var tomlData neoForgeModsToml
		if err := toml.Unmarshal(data, &tomlData); err != nil {
			return nil, manifestPath, fmt.Errorf("unmarshaling %s from %s: %w", manifestPath, jarIdentifier, err)
		}
		return &tomlData, manifestPath, nil
	}
	return nil, "", nil
}

// readJarjarMetadata finds and parses META-INF/jarjar/metadata.json, returning
// the internal file paths it declares. A missing file is not an error.
func readJarjarMetadata(jar *zipIndex) ([]string, error) {
	jarJarFile := jar.File("META-INF/jarjar/metadata.json")
	if jarJarFile == nil {
		return nil, nil
	}

	jarJarData, err := readZipFileEntry(jarJarFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read META-INF/jarjar/metadata.json: %w", err)
	}

	var metadata jarJarMetadata
	if err := json.Unmarshal(jarJarData, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse META-INF/jarjar/metadata.json: %w", err)
	}

	jars := make([]string, len(metadata.Jars))
	for i, jarEntry := range metadata.Jars {
		jars[i] = jarEntry.Path
	}
	return jars, nil
}

// readVersionFromManifest finds and parses the META-INF/MANIFEST.MF file within a JAR
// to extract the value of the "Implementation-Version" attribute.
func readVersionFromManifest(jar *zipIndex, jarIdentifier string) (string, error) {
	manifestFile := jar.File("META-INF/MANIFEST.MF")
	if manifestFile == nil {
		return "", fmt.Errorf("mod '%s' specifies version=${file.jarVersion} but META-INF/MANIFEST.MF was not found", jarIdentifier)
	}

	rc, err := manifestFile.Open()
	if err != nil {
		return "", fmt.Errorf("could not open META-INF/MANIFEST.MF for '%s': %w", jarIdentifier, err)
	}
	defer rc.Close()

	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		line := scanner.Text()
		// Check for the specific key. A simple prefix check is sufficient and robust.
		if strings.HasPrefix(line, "Implementation-Version:") {
			// Split the line at the first colon and trim whitespace from the value.
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				version := strings.TrimSpace(parts[1])
				if version != "" {
					return version, nil
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading META-INF/MANIFEST.MF for '%s': %w", jarIdentifier, err)
	}

	return "", fmt.Errorf("mod '%s' specifies version=${file.jarVersion} but 'Implementation-Version' was not found in META-INF/MANIFEST.MF", jarIdentifier)
}

// convertNeoForgeToml translates an already decoded neoforge.mods.toml into the
// tool's internal ModMetadata format.
func convertNeoForgeToml(tomlData *neoForgeModsToml, manifestPath, jarIdentifier string, jar *zipIndex, logBuffer *logBuffer) (mm ModMetadata, err error) {
	if len(tomlData.Mods) == 0 {
		return mm, fmt.Errorf("%s from %s contains no [[mods]] entries", manifestPath, jarIdentifier)
	}

	// Translate the primary mod identity and "provides" list.
	// The first [[mods]] entry is considered the primary mod.
	primaryMod := tomlData.Mods[0]
	mm = ModMetadata{
		ID:       primaryMod.ModID,
		Name:     primaryMod.DisplayName,
		Loader:   ManifestLoaderNeoForge,
		Provides: slices.Clone(primaryMod.Provides),
	}

	mavenModVersionStr := primaryMod.Version
	if mavenModVersionStr == "${file.jarVersion}" {
		versionFromManifest, err := readVersionFromManifest(jar, jarIdentifier)
		if err != nil {
			logBuffer.add(logging.LevelWarn, "ModLoader: %v", err)
			versionFromManifest = "0.0.0"
		}
		logBuffer.add(logging.LevelDebug, "ModLoader: Resolved dynamic version for %s from MANIFEST.MF as %s", primaryMod.ModID, versionFromManifest)
		mavenModVersionStr = versionFromManifest
	}

	// Leverage the existing VersionField JSON unmarshaler to parse the version string.
	// The string must be wrapped in quotes to be treated as a valid JSON string.
	modVersionStr, err := version.TranslateMavenVersion(mavenModVersionStr)
	if err != nil {
		return mm, fmt.Errorf("translating version '%s' for mod '%s' in %s: %w", primaryMod.Version, primaryMod.ModID, jarIdentifier, err)
	}
	if err := mm.Version.UnmarshalJSON([]byte(fmt.Sprintf(`"%s"`, modVersionStr))); err != nil {
		return mm, fmt.Errorf("parsing version '%s' for mod '%s' in %s: %w", primaryMod.Version, primaryMod.ModID, jarIdentifier, err)
	}

	// Subsequent [[mods]] blocks are treated as "provides".
	if len(tomlData.Mods) > 1 {
		for _, providedMod := range tomlData.Mods[1:] {
			// Add the mod ID itself
			mm.Provides = append(mm.Provides, providedMod.ModID)
			// Also add any additional provides from the block
			mm.Provides = append(mm.Provides, providedMod.Provides...)
		}
	}

	// Translate dependencies.
	mm.Depends = make(VersionRanges)
	mm.Recommends = make(VersionRanges)
	mm.Breaks = make(VersionRanges)
	mm.Conflicts = make(VersionRanges)

	if modDependencies, ok := tomlData.Dependencies[primaryMod.ModID]; ok {
		for _, dep := range modDependencies {
			if dep.ModID == "" {
				logging.Debugf("ModLoader: Skipping dependency with no ID in %s", jarIdentifier)
				continue
			}

			// A single Maven range string can translate to multiple Fabric predicate strings (OR relationship).
			predicateStrings, err := version.TranslateMavenVersionRange(dep.VersionRange)
			if err != nil {
				return mm, fmt.Errorf("translating maven version range '%s' for dep '%s' in %s: %w", dep.VersionRange, dep.ModID, jarIdentifier, err)
			}

			// Parse each translated predicate string into a VersionPredicate object.
			predicates := make([]*version.VersionPredicate, 0, len(predicateStrings))
			for _, pStr := range predicateStrings {
				pred, err := version.ParseVersionPredicate(pStr)
				if err != nil {
					logBuffer.add(logging.LevelWarn, "ModLoader: Failed to parse translated predicate '%s' (from maven range '%s') for dep '%s' in %s: %v", pStr, dep.VersionRange, dep.ModID, jarIdentifier, err)
					pred = version.Any()
				}
				predicates = append(predicates, pred)
			}

			depType := dep.Type
			if depType == "" {
				// Legacy Forge mods.toml uses "mandatory" instead of "type".
				// An omitted mandatory flag defaults to required, matching both
				// NeoForge's empty-type default and Forge's default-true.
				if dep.Mandatory == nil || *dep.Mandatory {
					depType = "required"
				} else {
					depType = "optional"
				}
			}
			depType = strings.ToLower(depType)

			switch depType {
			case "required":
				mm.Depends[dep.ModID] = predicates
			case "optional":
				mm.Recommends[dep.ModID] = predicates
			case "incompatible":
				mm.Breaks[dep.ModID] = predicates
			case "discouraged":
				mm.Conflicts[dep.ModID] = predicates
			default:
				logging.Warnf("ModLoader: Unknown dependency type '%s' for dep '%s' in %s; treating as optional", depType, dep.ModID, jarIdentifier)
			}
		}
	}

	return mm, nil
}
