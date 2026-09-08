package mods

import (
	"archive/zip"
	"bytes"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/classfile"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// maxNestedJarDepth limits recursion into nested JARs.
const maxNestedJarDepth = 4

// vendoredClassPackagePrefixes lists library and runtime package prefixes.
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
	"javax/",
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
	// Additional, non minecraft-specific vendored packages
	"org/jetbrains/annotations/",
	"org/intellij/lang/annotations/",
}

// isVendoredClass reports whether internalName belongs to a known vendored package.
func isVendoredClass(internalName string) bool {
	for _, prefix := range vendoredClassPackagePrefixes {
		if strings.HasPrefix(internalName, prefix) {
			return true
		}
	}
	return false
}

// JarClassIndex stores classes declared and referenced by a JAR tree.
type JarClassIndex struct {
	// Declared contains internal names of classes in the JAR tree.
	Declared map[string]struct{}
	// Referenced contains internal names referenced by classes in the JAR tree.
	Referenced map[string]struct{}
}

// IndexJar adds class files in reader and nested JARs to index.
func IndexJar(reader *zip.Reader, index *JarClassIndex, depth int, identifier string) {
	if depth > maxNestedJarDepth {
		logging.Warnf("ClassDeps: Maximum nested jar depth (%d) exceeded at '%s'", maxNestedJarDepth, identifier)
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
			info, err := classfile.ParseClassFile(data)
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
			IndexJar(nestedReader, index, depth+1, f.Name)
		}
	}
}

// InferredDependency describes an undeclared dependency inferred from bytecode.
type InferredDependency struct {
	// SourceID identifies the mod with the undeclared reference.
	SourceID string
	// TargetID identifies the mod declaring the referenced classes.
	TargetID string
	// Classes lists the referenced internal class names.
	Classes []string
}

// InferDependencies identifies undeclared dependencies from mod class references.
// It ignores vendored, ambiguous, and already-related classes and targets.
func InferDependencies(allMods map[string]*Mod, dr ...*DependencyResolver) []InferredDependency {
	start := time.Now()

	indexes := make(map[string]*JarClassIndex, len(allMods))
	for id, mod := range allMods {
		if mod == nil || mod.ClassIndex == nil {
			logging.Warnf("ClassDeps: Mod '%s' has no class index; skipping.", id)
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

	var deps []InferredDependency
	for _, sourceID := range slices.Sorted(maps.Keys(indexes)) {
		source := allMods[sourceID]
		classesByTarget := make(map[string][]string)
		for class := range indexes[sourceID].Referenced {
			if isVendoredClass(class) {
				continue
			}
			ownerList := owners[class]
			if len(ownerList) != 1 {
				continue
			}
			targetID := ownerList[0]
			if targetID == sourceID {
				continue
			}
			classesByTarget[targetID] = append(classesByTarget[targetID], class)
		}

		for _, targetID := range slices.Sorted(maps.Keys(classesByTarget)) {
			target := allMods[targetID]
			if sourceAlreadyDependsOn(source, target) {
				continue
			}
			if len(dr) > 0 && dr[0] != nil && dr[0].HasInferredDependency(sourceID, targetID) {
				continue
			}
			classes := classesByTarget[targetID]
			slices.Sort(classes)
			deps = append(deps, InferredDependency{
				SourceID: sourceID,
				TargetID: targetID,
				Classes:  classes,
			})
		}
	}

	logging.Infof("ClassDeps: Inferred %d dependency/ies from %d jar(s) in %s.", len(deps), len(indexes), time.Since(start))
	return deps
}

// ApplyInferredDependencies delegates recording inferred dependencies to the resolver.
func ApplyInferredDependencies(dr *DependencyResolver, deps []InferredDependency) []InferredDependency {
	return dr.ApplyInferredDependencies(deps)
}

// sourceAlreadyDependsOn reports whether source already relates to target.
func sourceAlreadyDependsOn(source *Mod, target *Mod) bool {
	if source == nil || target == nil {
		return true
	}

	ids := []string{target.Metadata.ID}
	if len(target.EffectiveProvides) > 0 {
		ids = append(ids, slices.Sorted(maps.Keys(target.EffectiveProvides))...)
	} else if len(target.Metadata.Provides) > 0 {
		ids = append(ids, target.Metadata.Provides...)
	}

	for _, id := range ids {
		if _, ok := source.Metadata.Depends[id]; ok {
			return true
		}
		if _, ok := source.Metadata.Suggests[id]; ok {
			return true
		}
		if _, ok := source.Metadata.Recommends[id]; ok {
			return true
		}
		if _, ok := source.Metadata.Breaks[id]; ok {
			return true
		}
		if _, ok := source.Metadata.Conflicts[id]; ok {
			return true
		}
	}
	return false
}
