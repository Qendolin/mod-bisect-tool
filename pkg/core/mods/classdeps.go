package mods

import (
	"archive/zip"
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// maxNestedJarDepth bounds recursion into jars bundled inside jars (jarjar,
// fabric "jars", etc.) to protect against pathological archives.
const maxNestedJarDepth = 4

// vendoredClassPackagePrefixes lists package prefixes (internal name form,
// e.g. "com/google/common/") whose classes are library/runtime classes, not
// mod classes. Mods frequently vendor (shade) these into their own jar; such
// a vendored class must not make its host mod a dependency target. Entries
// were sampled from Minecraft 1.8–26.2 and the Forge, NeoForge, Fabric and
// Quilt loader/runtime class trees. Deliberately NOT listed: net/fabricmc/
// fabric (Fabric API mods) and org/quiltmc/qsl (QSL mods).
var vendoredClassPackagePrefixes = []string{
	"com/azure/json/",
	"com/electronwill/nightconfig/",
	"com/google/",
	"com/ibm/icu/",
	"com/jcraft/",
	"com/llamalad7/mixinextras/platform/forge/",
	"com/microsoft/aad/",
	"com/mojang/",
	"com/sun/jna/",
	"cpw/mods/",
	"gnu/trove/",
	"io/github/zekerzhayard/",
	"io/netty/",
	"it/unimi/dsi/",
	"javax/vecmath/",
	"joptsimple/",
	"net/fabricmc/api/",
	"net/fabricmc/loader/",
	"net/java/games/",
	"net/jpountz/",
	"net/jodah/typetools/",
	"net/minecraft/",
	"net/minecraftforge/",
	"net/minecrell/terminalconsole/",
	"net/neoforged/",
	"org/apache/",
	"org/codehaus/plexus/util/",
	"org/jspecify/annotations/",
	"org/jline/",
	"org/joml/",
	"org/lwjgl/",
	"org/objectweb/asm/",
	"org/prismlauncher/",
	"org/quiltmc/config/",
	"org/quiltmc/json5/",
	"org/quiltmc/loader/",
	"org/slf4j/",
	"org/spongepowered/",
	"oshi/",
	"paulscode/sound/",
	"tv/twitch/",
}

// isVendoredClass reports whether the internal class name lives under one of
// the vendored (library) package prefixes.
func isVendoredClass(internalName string) bool {
	for _, prefix := range vendoredClassPackagePrefixes {
		if strings.HasPrefix(internalName, prefix) {
			return true
		}
	}
	return false
}

// JarClassIndex holds all class names declared and referenced by a jar tree
// (the top-level jar plus any nested jars). Classes inside nested jars are
// attributed to the top-level jar, mirroring how activating a container mod
// always activates its nested content.
type JarClassIndex struct {
	// Declared contains the internal names of every class file in the jar
	// tree, regardless of visibility (public, private, nested, anonymous...).
	Declared map[string]struct{}
	// Referenced contains the internal names of all CONSTANT_Class entries
	// found in the jar tree's class files.
	Referenced map[string]struct{}
}

// IndexJarClasses parses every class file in the jar and collects declared and
// referenced class names. Class files that fail to parse (e.g. unknown
// constant pool tags from future JVM versions) are logged and skipped; a
// partially indexed jar only degrades the quality of the dependency inference.
func IndexJarClasses(jarPath string) (*JarClassIndex, error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, fmt.Errorf("opening JAR %s as zip: %w", jarPath, err)
	}
	defer zr.Close()

	index := &JarClassIndex{
		Declared:   make(map[string]struct{}),
		Referenced: make(map[string]struct{}),
	}
	indexJarReader(&zr.Reader, index, 0, filepath.Base(jarPath))
	return index, nil
}

func indexJarReader(reader *zip.Reader, index *JarClassIndex, depth int, identifier string) {
	if depth > maxNestedJarDepth {
		logging.Warnf("ClassDeps: Maximum nested jar depth (%d) exceeded at '%s'; skipping deeper jars.", maxNestedJarDepth, identifier)
		return
	}

	for _, f := range reader.File {
		switch {
		case strings.HasSuffix(f.Name, ".class"):
			data, err := readZipFileEntry(f)
			if err != nil {
				logging.Warnf("ClassDeps: Failed to read class file '%s' in '%s': %v", f.Name, identifier, err)
				continue
			}
			info, err := ParseClassFile(data)
			if err != nil {
				logging.Warnf("ClassDeps: Skipping unparseable class file '%s' in '%s': %v", f.Name, identifier, err)
				continue
			}
			index.Declared[info.Name] = struct{}{}
			for _, ref := range info.References {
				index.Referenced[ref] = struct{}{}
			}
		case strings.HasSuffix(f.Name, ".jar"):
			data, err := readZipFileEntry(f)
			if err != nil {
				logging.Warnf("ClassDeps: Failed to read nested jar '%s' in '%s': %v", f.Name, identifier, err)
				continue
			}
			nestedReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				logging.Warnf("ClassDeps: Failed to open nested jar '%s' in '%s' as zip: %v", f.Name, identifier, err)
				continue
			}
			indexJarReader(nestedReader, index, depth+1, f.Name)
		}
	}
}

// PotentialDependency is an inferred undeclared dependency between two mods.
type PotentialDependency struct {
	// SourceID is the mod whose code references classes it does not declare.
	SourceID string
	// TargetID is the mod that declares the referenced classes.
	TargetID string
	// Classes are the referenced internal class names that led to this
	// inference, sorted.
	Classes []string
}

// InferPotentialDependencies reads the class index of every top-level mod
// (built during mod loading) and infers undeclared dependencies: when mod B's
// class references point at a class that only mod A declares, and B does not
// already declare a dependency on A (by ID or any of A's effective provides),
// B -> A is assumed to be an undeclared dependency. Classes declared by more
// than one mod, and classes under vendored (library) package prefixes, are
// ignored. The result is sorted deterministically.
func InferPotentialDependencies(allMods map[string]*Mod) []PotentialDependency {
	start := time.Now()

	indexes := make(map[string]*JarClassIndex, len(allMods))
	for id, mod := range allMods {
		if mod == nil || mod.ClassIndex == nil {
			logging.Warnf("ClassDeps: Mod '%s' has no class index; skipping it for the dependency inference.", id)
			continue
		}
		indexes[id] = mod.ClassIndex
	}

	// Map each declared class to the mods that declare it, skipping vendored
	// library classes (e.g. a mod bundling org/slf4j is not an slf4j provider).
	owners := make(map[string][]string, len(indexes))
	for _, id := range slices.Sorted(maps.Keys(indexes)) {
		for class := range indexes[id].Declared {
			if isVendoredClass(class) {
				continue
			}
			owners[class] = append(owners[class], id)
		}
	}

	var deps []PotentialDependency
	for _, sourceID := range slices.Sorted(maps.Keys(indexes)) {
		source := allMods[sourceID]
		classesByTarget := make(map[string][]string)
		for class := range indexes[sourceID].Referenced {
			ownerList := owners[class]
			if len(ownerList) != 1 {
				continue // Unknown origin (e.g. the JDK) or ambiguous ownership.
			}
			targetID := ownerList[0]
			if targetID == sourceID {
				continue
			}
			classesByTarget[targetID] = append(classesByTarget[targetID], class)
		}

		for _, targetID := range slices.Sorted(maps.Keys(classesByTarget)) {
			if sourceAlreadyDependsOn(source, allMods[targetID]) {
				continue
			}
			classes := classesByTarget[targetID]
			slices.Sort(classes)
			deps = append(deps, PotentialDependency{
				SourceID: sourceID,
				TargetID: targetID,
				Classes:  classes,
			})
		}
	}

	logging.Infof("ClassDeps: Inferred %d potential undeclared dependency/ies from %d jar(s) in %s.", len(deps), len(indexes), time.Since(start))
	return deps
}

// sourceAlreadyDependsOn reports whether the source mod's manifest already
// declares a dependency on the target mod, either via its ID or any of the
// IDs it effectively provides (own ID, Provides, and nested-module provides;
// falling back to ID+Provides when EffectiveProvides was not populated).
func sourceAlreadyDependsOn(source *Mod, target *Mod) bool {
	if source == nil || target == nil {
		return true // Missing data: assume declared rather than guessing.
	}

	var providedIDs []string
	if len(target.EffectiveProvides) > 0 {
		providedIDs = slices.Sorted(maps.Keys(target.EffectiveProvides))
	} else {
		providedIDs = append([]string{target.Metadata.ID}, target.Metadata.Provides...)
	}

	for _, id := range providedIDs {
		if _, ok := source.Metadata.Depends[id]; ok {
			return true
		}
	}
	return false
}
