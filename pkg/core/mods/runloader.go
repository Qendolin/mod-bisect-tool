package mods

import (
	"fmt"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// RunLoader identifies the mod loader the user runs Minecraft with. It decides
// how a jar's manifests are resolved into a mod (see resolveManifest).
type RunLoader string

const (
	// RunLoaderFabric runs the Fabric loader (with Quilt mods accepted).
	RunLoaderFabric RunLoader = "fabric"
	// RunLoaderNeoForge runs the (Neo)Forge loader.
	RunLoaderNeoForge RunLoader = "neoforge"
	// RunLoaderNeoForgeWithFabric runs (Neo)Forge with Fabric mods via Sinytra
	// Connector. (Neo)Forge manifests are preferred, Fabric is the fallback.
	RunLoaderNeoForgeWithFabric RunLoader = "neoforge-with-fabric"
	// RunLoaderFabricWithNeoForge runs Fabric with (Neo)Forge mods via Kilt.
	// Fabric manifests are preferred, (Neo)Forge is the fallback.
	RunLoaderFabricWithNeoForge RunLoader = "fabric-with-neoforge"
)

// String returns the user-facing label for the loader.
func (l RunLoader) String() string {
	switch l {
	case RunLoaderFabric:
		return "Fabric"
	case RunLoaderNeoForge:
		return "(Neo)Forge"
	case RunLoaderNeoForgeWithFabric:
		return "(Neo)Forge with Fabric (Sinytra Connector)"
	case RunLoaderFabricWithNeoForge:
		return "Fabric with (Neo)Forge (Kilt)"
	default:
		return string(l)
	}
}

// ParseRunLoader parses a command-line value into a RunLoader.
func ParseRunLoader(value string) (RunLoader, error) {
	switch value {
	case string(RunLoaderFabric):
		return RunLoaderFabric, nil
	case string(RunLoaderNeoForge):
		return RunLoaderNeoForge, nil
	case "connector":
		return RunLoaderNeoForgeWithFabric, nil
	case "kilt":
		return RunLoaderFabricWithNeoForge, nil
	default:
		return "", fmt.Errorf("unknown loader %q (expected fabric, neoforge, connector or kilt)", value)
	}
}

// SupportedRunLoaders returns the loaders offered to users, in display order.
// It is shared by the GUI and TUI setup screens.
func SupportedRunLoaders() []RunLoader {
	return []RunLoader{RunLoaderFabric, RunLoaderNeoForge, RunLoaderNeoForgeWithFabric, RunLoaderFabricWithNeoForge}
}

// parsesNeoForge reports whether the loader reads (Neo)Forge manifests
// (neoforge.mods.toml / mods.toml) and their jarjar (nested JAR) libraries.
func (l RunLoader) parsesNeoForge() bool {
	switch l {
	case RunLoaderNeoForge, RunLoaderNeoForgeWithFabric, RunLoaderFabricWithNeoForge:
		return true
	default:
		return false
	}
}

// parsesFabric reports whether the loader reads Fabric/Quilt manifests
// (fabric.mod.json / quilt.mod.json).
func (l RunLoader) parsesFabric() bool {
	switch l {
	case RunLoaderFabric, RunLoaderNeoForgeWithFabric, RunLoaderFabricWithNeoForge:
		return true
	default:
		return false
	}
}

// resolveManifest decides which of the jar's manifests is authoritative for this
// loader and converts it into mod metadata. It is the single place where
// selection rules, warnings, and conversions live.
//
// hasNeoForge and hasFabric report manifest presence for families this loader
// does not decode (used to produce foreign-mod errors); the raw pointers carry
// decoded data only. A jar without any authoritative manifest yields empty
// metadata; the caller treats it as a (Neo)Forge library.
func (l RunLoader) resolveManifest(
	hasNeoForge bool, neoForgeRaw *neoForgeModsToml, neoForgePath string,
	hasFabric bool, fabricRaw *fabricModJson, fabricLoader ManifestLoader,
	jar *zipIndex, jarIdentifier string, logBuffer *logBuffer,
) (ModMetadata, error) {
	switch l {
	case RunLoaderFabric:
		return resolveFabricManifest(fabricRaw, fabricLoader, hasNeoForge, jarIdentifier, l)
	case RunLoaderNeoForge:
		return resolveNeoForgeManifest(neoForgeRaw, neoForgePath, hasFabric, jarIdentifier, jar, logBuffer, l)
	case RunLoaderNeoForgeWithFabric:
		return resolveConnectorManifest(neoForgeRaw, neoForgePath, fabricRaw, fabricLoader, jar, jarIdentifier, logBuffer)
	case RunLoaderFabricWithNeoForge:
		return resolveKiltManifest(neoForgeRaw, neoForgePath, fabricRaw, fabricLoader, jar, jarIdentifier, logBuffer)
	default:
		return ModMetadata{}, fmt.Errorf("unknown run loader %q", l)
	}
}

// resolveFabricManifest resolves a jar for the plain Fabric loader. Fabric/Quilt
// is the only accepted family.
func resolveFabricManifest(
	fabricRaw *fabricModJson, fabricLoader ManifestLoader,
	hasNeoForge bool, jarIdentifier string, l RunLoader,
) (ModMetadata, error) {
	if fabricRaw == nil {
		if hasNeoForge {
			return ModMetadata{}, fmt.Errorf("%s is a (Neo)Forge mod, which is not supported by the %s loader", jarIdentifier, l)
		}
		return ModMetadata{}, fmt.Errorf("no mod manifest found in %s", jarIdentifier)
	}
	return convertFabricModJson(fabricRaw, fabricLoader), nil
}

// resolveNeoForgeManifest resolves a jar for the plain (Neo)Forge loader. A jar
// without a (Neo)Forge manifest yields empty metadata (loaded as a library).
func resolveNeoForgeManifest(
	neoForgeRaw *neoForgeModsToml, neoForgePath string,
	hasFabric bool, jarIdentifier string, jar *zipIndex, logBuffer *logBuffer, l RunLoader,
) (ModMetadata, error) {
	if neoForgeRaw == nil {
		if hasFabric {
			return ModMetadata{}, fmt.Errorf("%s is a Fabric/Quilt mod, which is not supported by the %s loader", jarIdentifier, l)
		}
		return ModMetadata{}, nil // no manifest, the caller loads it as a library
	}
	return convertNeoForgeToml(neoForgeRaw, neoForgePath, jarIdentifier, jar, logBuffer)
}

// resolveConnectorManifest resolves a jar for the Connector loader ((Neo)Forge
// with Fabric via Sinytra Connector). (Neo)Forge is preferred; a placeholder jar
// (dummy forge manifest + real Fabric manifest) loads as its Fabric self.
func resolveConnectorManifest(
	neoForgeRaw *neoForgeModsToml, neoForgePath string,
	fabricRaw *fabricModJson, fabricLoader ManifestLoader,
	jar *zipIndex, jarIdentifier string, logBuffer *logBuffer,
) (ModMetadata, error) {
	if neoForgeRaw != nil && !neoForgeRaw.isConnectorPlaceholder() {
		return convertConnectorNeoForgeToml(neoForgeRaw, neoForgePath, jarIdentifier, jar, logBuffer)
	}
	if fabricRaw != nil {
		// A placeholder jar carries a dummy forge manifest, so no fallback
		// warning. Only a Fabric jar missing a forge manifest entirely warns.
		if neoForgeRaw == nil && !logBuffer.warnedFallback {
			logBuffer.add(logging.LevelWarn, "ModLoader: (Neo)Forge parsing is enabled but %s is missing a (neo)forge.mods.toml and will fall back to %s parsing.", jarIdentifier, fabricLoader)
			logBuffer.warnedFallback = true
		}
		return convertFabricModJson(fabricRaw, fabricLoader), nil
	}
	if neoForgeRaw != nil {
		// Placeholder without a Fabric manifest: keep (Neo)Forge behavior,
		// including any modproperties provides.
		return convertConnectorNeoForgeToml(neoForgeRaw, neoForgePath, jarIdentifier, jar, logBuffer)
	}
	return ModMetadata{}, nil // no manifest, the caller loads it as a library
}

// resolveKiltManifest resolves a jar for the Kilt loader (Fabric with (Neo)Forge
// via Kilt). Fabric/Quilt is preferred, (Neo)Forge is the fallback.
func resolveKiltManifest(
	neoForgeRaw *neoForgeModsToml, neoForgePath string,
	fabricRaw *fabricModJson, fabricLoader ManifestLoader,
	jar *zipIndex, jarIdentifier string, logBuffer *logBuffer,
) (ModMetadata, error) {
	if fabricRaw != nil {
		return convertFabricModJson(fabricRaw, fabricLoader), nil
	}
	if neoForgeRaw != nil {
		return convertNeoForgeToml(neoForgeRaw, neoForgePath, jarIdentifier, jar, logBuffer)
	}
	return ModMetadata{}, nil // no manifest, the caller loads it as a library
}
