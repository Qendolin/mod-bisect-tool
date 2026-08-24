package mods

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// zipIndex is a zip archive with its entries indexed by path for O(1) lookups.
type zipIndex struct {
	byName map[string]*zip.File
}

// newZipIndex indexes an archive's entries. When an archive contains duplicate
// names, the first entry wins (matching the previous linear scan).
func newZipIndex(reader *zip.Reader) *zipIndex {
	byName := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if _, ok := byName[file.Name]; !ok {
			byName[file.Name] = file
		}
	}
	return &zipIndex{byName: byName}
}

// File returns the entry at the given path, or nil when the archive has none.
func (z *zipIndex) File(name string) *zip.File {
	return z.byName[name]
}

type ModParser struct {
	// RunLoader determines how a jar's manifests are resolved into a mod (see
	// RunLoader.resolveManifest).
	RunLoader RunLoader
}

// ExtractModMetadata opens a JAR and extracts its top-level and nested mod files.
func (p *ModParser) ExtractModMetadata(jarPath, jarName string, logBuffer *logBuffer) (ModMetadata, []NestedModule, error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return ModMetadata{}, nil, fmt.Errorf("opening JAR %s as zip: %w", jarPath, err)
	}
	defer zr.Close()

	return p.parseJarTree(newZipIndex(&zr.Reader), jarName, logBuffer)
}

// jarInterpretation is the result of interpreting a single jar's manifests:
// whether it is a mod, and which jarjar (nested) jars it bundles.
type jarInterpretation struct {
	metadata ModMetadata
	isMod    bool
	// jarjarEntries lists the nested jars declared via META-INF/jarjar/metadata.json.
	jarjarEntries []string
	// jarjarErr reports a failure to read or parse the jarjar metadata, if any.
	jarjarErr error
}

// interpretJar decodes the manifests this loader reads and resolves them against
// the active loader, then discovers its jarjar nested jars. Manifests of the
// other family are only checked for presence, so foreign-mod errors can be
// produced without parsing manifests the loader would reject. A jar without an
// authoritative manifest yields an empty metadata with isMod false.
func (p *ModParser) interpretJar(jar *zipIndex, jarIdentifier string, logBuffer *logBuffer) (jarInterpretation, error) {
	// hasNeoForge and hasFabric report whether a manifest file of that family is
	// present, regardless of whether this loader decodes it. The raw pointers
	// below carry decoded data only; the resolver separates presence (foreign
	// mod errors) from data (conversions).
	hasNeoForge := hasNeoForgeManifest(jar)
	hasFabric := hasFabricManifest(jar)

	var neoForgeRaw *neoForgeModsToml
	var neoForgePath string
	if p.RunLoader.parsesNeoForge() {
		var err error
		neoForgeRaw, neoForgePath, err = tryDecodeNeoForgeToml(jar, jarIdentifier)
		if err != nil {
			return jarInterpretation{}, err
		}
	}

	var fabricRaw *fabricModJson
	var fabricLoader ManifestLoader
	if p.RunLoader.parsesFabric() {
		var err error
		fabricRaw, fabricLoader, err = tryDecodeFabricModJson(jar, jarIdentifier)
		if err != nil {
			return jarInterpretation{}, err
		}
	}

	metadata, err := p.RunLoader.resolveManifest(hasNeoForge, neoForgeRaw, neoForgePath, hasFabric, fabricRaw, fabricLoader, jar, jarIdentifier, logBuffer)
	if err != nil {
		return jarInterpretation{}, err
	}

	interpretation := jarInterpretation{
		metadata: metadata,
		isMod:    metadata.Loader != ManifestLoaderNone,
	}

	if p.RunLoader.parsesNeoForge() {
		interpretation.jarjarEntries, interpretation.jarjarErr = readJarjarMetadata(jar)
	}

	return interpretation, nil
}

// parseJarTree interprets a jar and recursively parses all of its nested jars.
// A jar without a mod manifest is loaded as a library/container, or remains a
// mod with its manifest-declared and jarjar nested jars merged.
func (p *ModParser) parseJarTree(jar *zipIndex, jarIdentifier string, logBuffer *logBuffer) (ModMetadata, []NestedModule, error) {
	interpretation, err := p.interpretJar(jar, jarIdentifier, logBuffer)
	if err != nil {
		return ModMetadata{}, nil, err
	}

	if interpretation.jarjarErr != nil {
		if interpretation.isMod {
			logBuffer.add(logging.LevelWarn, "ModLoader: Jar file is a mod, but loading nested jars failed: %v", interpretation.jarjarErr)
		} else {
			logBuffer.add(logging.LevelError, "ModLoader: Failed to parse jarjar metadata for %s: %v. Loading without nested jars.", jarIdentifier, interpretation.jarjarErr)
		}
	}

	metadata := interpretation.metadata
	if !interpretation.isMod {
		// Only loaders that accept (Neo)Forge libraries reach here; loaders that
		// reject manifest-less jars error inside resolveManifest instead.
		metadata = synthesizeLibrary(jarIdentifier, interpretation.jarjarEntries)
		if len(interpretation.jarjarEntries) == 0 {
			if interpretation.jarjarErr == nil {
				logBuffer.add(logging.LevelDebug, "ModLoader: %s is not a mod and not a container, probably just a library", jarIdentifier)
			}
		} else {
			logBuffer.add(logging.LevelDebug, "ModLoader: %s is not a mod, but is a container for %d nested JAR(s).", jarIdentifier, len(interpretation.jarjarEntries))
		}
	} else if len(interpretation.jarjarEntries) > 0 {
		metadata.Jars = appendUnique(metadata.Jars, interpretation.jarjarEntries)
	}

	if err := validateMetadata(&metadata, jarIdentifier); err != nil {
		return ModMetadata{}, nil, err
	}

	allNestedMods := []NestedModule{}
	for _, nestedJarEntry := range metadata.Jars {
		if nestedJarEntry == "" {
			logBuffer.add(logging.LevelWarn, "ModLoader: Mod '%s' has a nested JAR entry with an empty 'file' path. Skipping.", metadata.ID)
			continue
		}

		foundMods, err := p.parseNestedJar(jar, nestedJarEntry, jarIdentifier, logBuffer)
		if err != nil {
			if p.RunLoader.parsesNeoForge() {
				logBuffer.add(logging.LevelDebug, "ModLoader: Skipping nested JAR '%s' in '%s' (likely a non-mod library): %v", nestedJarEntry, jarIdentifier, err)
			} else {
				logBuffer.add(logging.LevelWarn, "ModLoader: Failed to process nested JAR '%s' in '%s': %v", nestedJarEntry, jarIdentifier, err)
			}
			continue
		}
		allNestedMods = append(allNestedMods, foundMods...)
	}
	return metadata, allNestedMods, nil
}

// parseNestedJar extracts a nested jar from a parent archive and parses it and
// any of its own nested jars.
func (p *ModParser) parseNestedJar(parentJar *zipIndex, pathInParent, currentPathPrefix string, logBuffer *logBuffer) ([]NestedModule, error) {
	fullPathInJar := path.Join(currentPathPrefix, pathInParent)

	nestedZipFile := parentJar.File(pathInParent)
	if nestedZipFile == nil {
		return nil, fmt.Errorf("nested JAR '%s' not found in archive", fullPathInJar)
	}

	rc, err := nestedZipFile.Open()
	if err != nil {
		return nil, fmt.Errorf("opening nested JAR '%s': %w", fullPathInJar, err)
	}
	defer rc.Close()

	jarBytes, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("reading nested JAR '%s': %w", fullPathInJar, err)
	}

	bytesReader := bytes.NewReader(jarBytes)
	innerZipReader, err := zip.NewReader(bytesReader, int64(len(jarBytes)))
	if err != nil {
		return nil, fmt.Errorf("reading nested content '%s' as zip: %w", fullPathInJar, err)
	}

	currentMetadata, nestedMods, err := p.parseJarTree(newZipIndex(innerZipReader), fullPathInJar, logBuffer)
	if err != nil {
		return nil, fmt.Errorf("parsing metadata from '%s': %w", fullPathInJar, err)
	}

	return append([]NestedModule{{Info: currentMetadata, PathInJar: fullPathInJar}}, nestedMods...), nil
}

// synthesizeLibrary builds the synthetic metadata for a jar without a mod
// manifest. Its Jars point at the jarjar nested jars it bundles, if any.
func synthesizeLibrary(jarIdentifier string, jarjarEntries []string) ModMetadata {
	placeholderVersion, _ := version.Parse("0.0.0-synthetic", false)
	return ModMetadata{
		ID:            fmt.Sprintf("library-%s", filepath.Base(jarIdentifier)),
		Version:       VersionField{Version: placeholderVersion},
		Name:          "Library",
		Jars:          jarjarEntries,
		IsJavaLibrary: true,
	}
}

// appendUnique appends every element of src to dst that is not already present.
func appendUnique(dst, src []string) []string {
	for _, entry := range src {
		if !slices.Contains(dst, entry) {
			dst = append(dst, entry)
		}
	}
	return dst
}

// validateMetadata checks a parsed mod's required fields and reserved IDs.
func validateMetadata(metadata *ModMetadata, jarIdentifier string) error {
	if metadata.ID == "" {
		return fmt.Errorf("%s has a missing mod ID", jarIdentifier)
	}

	if metadata.Version.Version == nil {
		return fmt.Errorf("%s is missing mandatory 'version' entry", jarIdentifier)
	}

	if IsImplicitMod(metadata.ID) {
		return fmt.Errorf("mod from '%s' illegally uses reserved ID '%s'", jarIdentifier, metadata.ID)
	}
	for _, providedID := range metadata.Provides {
		if IsImplicitMod(providedID) {
			return fmt.Errorf("mod '%s' from '%s' illegally provides reserved ID '%s'", metadata.ID, jarIdentifier, providedID)
		}
	}

	return nil
}

func readZipFileEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
