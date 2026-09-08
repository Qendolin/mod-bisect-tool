package mods

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
)

func IndexJarClasses(jarPath string) (*JarClassIndex, error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, fmt.Errorf("opening JAR %s: %w", jarPath, err)
	}
	defer zr.Close()

	index := &JarClassIndex{
		Declared:   make(map[string]struct{}),
		Referenced: make(map[string]struct{}),
	}
	IndexJar(&zr.Reader, index, 0, filepath.Base(jarPath))
	return index, nil
}

func classFileBytes(thisClassIndex, cpCount uint16, cpEntries ...[]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xCA, 0xFE, 0xBA, 0xBE})
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))
	_ = binary.Write(&buf, binary.BigEndian, uint16(52))
	_ = binary.Write(&buf, binary.BigEndian, cpCount)
	for _, entry := range cpEntries {
		buf.Write(entry)
	}
	_ = binary.Write(&buf, binary.BigEndian, uint16(0x0021))
	_ = binary.Write(&buf, binary.BigEndian, thisClassIndex)
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))
	return buf.Bytes()
}

func cpUtf8(s string) []byte {
	out := binary.BigEndian.AppendUint16([]byte{1}, uint16(len(s)))
	return append(out, s...)
}

func cpClass(nameIndex uint16) []byte {
	return binary.BigEndian.AppendUint16([]byte{7}, nameIndex)
}

func mustWriteJar(t *testing.T, dir, name string, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, zipWith(t, entries), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func classDeclaring(internalName string) []byte {
	return classFileBytes(2, 3, cpUtf8(internalName), cpClass(1))
}

func classReferencing(internalName string, refs ...string) []byte {
	utf8Count := 1 + len(refs)
	thisClass := uint16(2 * utf8Count)
	entries := make([][]byte, 0, utf8Count+len(refs)+1)
	entries = append(entries, cpUtf8(internalName))
	for _, ref := range refs {
		entries = append(entries, cpUtf8(ref))
	}
	for i := 2; i <= utf8Count; i++ {
		entries = append(entries, cpClass(uint16(i)))
	}
	entries = append(entries, cpClass(1))
	return classFileBytes(thisClass, uint16(len(entries)+1), entries...)
}

func modAt(t *testing.T, id, jarPath string) *Mod {
	t.Helper()
	index, err := IndexJarClasses(jarPath)
	if err != nil {
		t.Fatalf("IndexJarClasses failed for %s: %v", jarPath, err)
	}
	return &Mod{
		Path:         jarPath,
		BaseFilename: filepath.Base(jarPath),
		Metadata:     ModMetadata{ID: id},
		ClassIndex:   index,
	}
}

func TestVendoredClassPrefixes(t *testing.T) {
	excluded := []string{
		"com/google/common/collect/ImmutableList",
		"com/google/gson/Gson",
		"com/ibm/icu/text/NumberFormat",
		"com/mojang/brigadier/CommandDispatcher",
		"com/mojang/blaze3d/systems/RenderSystem",
		"io/netty/channel/EventLoopGroup",
		"it/unimi/dsi/fastutil/ints/Int2ObjectMap",
		"joptsimple/OptionParser",
		"net/minecraft/client/Minecraft",
		"net/minecraftforge/fml/common/Mod",
		"net/neoforged/bus/EventBus",
		"org/apache/commons/lang3/StringUtils",
		"org/joml/Matrix4f",
		"org/objectweb/asm/ClassWriter",
		"org/quiltmc/loader/impl/QuiltLoaderImpl",
		"org/slf4j/Logger",
		"org/spongepowered/asm/mixin/Mixin",
		"oshi/SystemInfo",
		"paulscode/sound/SoundSystem",
	}
	for _, name := range excluded {
		if !isVendoredClass(name) {
			t.Errorf("expected %q to be excluded", name)
		}
	}

	eligible := []string{
		"net/fabricmc/fabric/api/item/v1/FabricItem",
		"org/quiltmc/qsl/block/impl/QuiltBlock",
		"com/example/mymod/MyMod",
		"com/mycompany/mymod/sub/Deep",
	}
	for _, name := range eligible {
		if isVendoredClass(name) {
			t.Errorf("expected %q to be eligible", name)
		}
	}
}

func TestIndexJarClassesTopLevelAndNested(t *testing.T) {
	dir := t.TempDir()

	nestedJar := zipWith(t, map[string][]byte{
		"com/pkg/B.class": classReferencing("com/pkg/B", "com/pkg/A"),
	})
	jarPath := mustWriteJar(t, dir, "container.jar", map[string][]byte{
		"com/pkg/A.class":                    classDeclaring("com/pkg/A"),
		"META-INF/jarjar/bundle.jar":         nestedJar,
		"broken.class":                       []byte("this is not a class file"),
		"META-INF/versions/17/com/x/O.class": classDeclaring("com/x/O"),
	})

	index, err := IndexJarClasses(jarPath)
	if err != nil {
		t.Fatalf("IndexJarClasses failed: %v", err)
	}

	for _, expected := range []string{"com/pkg/A", "com/pkg/B", "com/x/O"} {
		if !slices.Contains(sortedKeys(index.Declared), expected) {
			t.Errorf("expected declared class %q, got %v", expected, sortedKeys(index.Declared))
		}
	}
	if !slices.Contains(sortedKeys(index.Referenced), "com/pkg/A") {
		t.Errorf("expected referenced class %q from nested jar, got %v", "com/pkg/A", sortedKeys(index.Referenced))
	}
}

func TestIndexJarClassesMissingFileErrors(t *testing.T) {
	if _, err := IndexJarClasses(filepath.Join(t.TempDir(), "does-not-exist.jar")); err == nil {
		t.Fatal("expected error for missing jar file, got nil")
	}
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestInferDependencies(t *testing.T) {
	dir := t.TempDir()

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		"com/a/Foo.class": classDeclaring("com/a/Foo"),
	})
	bJar := mustWriteJar(t, dir, "b.jar", map[string][]byte{
		"com/b/Bar.class": classReferencing("com/b/Bar", "com/a/Foo", "com/unknown/NeverDeclared"),
	})
	cJar := mustWriteJar(t, dir, "c.jar", map[string][]byte{
		"com/c/C.class": classDeclaring("com/c/C"),
	})

	deps := InferDependencies(map[string]*Mod{
		"a": modAt(t, "a", aJar),
		"b": modAt(t, "b", bJar),
		"c": modAt(t, "c", cJar),
	})

	if len(deps) != 1 {
		t.Fatalf("expected exactly 1 inferred dependency, got %+v", deps)
	}
	dep := deps[0]
	if dep.SourceID != "b" || dep.TargetID != "a" {
		t.Errorf("expected dependency b -> a, got %s -> %s", dep.SourceID, dep.TargetID)
	}
	if !slices.Equal(dep.Classes, []string{"com/a/Foo"}) {
		t.Errorf("expected classes [com/a/Foo], got %v", dep.Classes)
	}
}

func TestInferDependenciesSkipsAlreadyDeclared(t *testing.T) {
	dir := t.TempDir()

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		"com/a/Foo.class": classDeclaring("com/a/Foo"),
	})
	bJar := mustWriteJar(t, dir, "b.jar", map[string][]byte{
		"com/b/Bar.class": classReferencing("com/b/Bar", "com/a/Foo"),
	})

	newMod := func() *Mod {
		mod := modAt(t, "b", bJar)
		mod.Metadata.Depends = VersionRanges{"a": {version.Any()}}
		return mod
	}

	if deps := InferDependencies(map[string]*Mod{"a": modAt(t, "a", aJar), "b": newMod()}); len(deps) != 0 {
		t.Errorf("expected no dependency when already declared, got %+v", deps)
	}

	a := modAt(t, "a", aJar)
	a.Metadata.Provides = []string{"a-provided"}
	providedMod := newMod()
	providedMod.Metadata.Depends = VersionRanges{"a-provided": {version.Any()}}
	if deps := InferDependencies(map[string]*Mod{"a": a, "b": providedMod}); len(deps) != 0 {
		t.Errorf("expected no dependency when declared via provides, got %+v", deps)
	}
}

func TestInferDependenciesSkipsNestedModuleProvides(t *testing.T) {
	dir := t.TempDir()

	containerJar := mustWriteJar(t, dir, "container.jar", map[string][]byte{
		"com/x/Foo.class": classDeclaring("com/x/Foo"),
	})
	userJar := mustWriteJar(t, dir, "user.jar", map[string][]byte{
		"com/b/Bar.class": classReferencing("com/b/Bar", "com/x/Foo"),
	})

	container := modAt(t, "container", containerJar)
	v := mustParseVersion(t, "1.0")
	container.EffectiveProvides = map[string]version.Version{"container": v, "sodium": v}
	user := modAt(t, "user", userJar)
	user.Metadata.Depends = VersionRanges{"sodium": {version.Any()}}

	deps := InferDependencies(map[string]*Mod{"container": container, "user": user})
	if len(deps) != 0 {
		t.Errorf("expected dependency on nested-module provide to count as declared, got %+v", deps)
	}
}

func TestInferDependenciesSkipsVendoredClasses(t *testing.T) {
	dir := t.TempDir()

	vendorJar := mustWriteJar(t, dir, "vendor.jar", map[string][]byte{
		"org/slf4j/Logger.class": classDeclaring("org/slf4j/Logger"),
		"com/vendor/Main.class":  classDeclaring("com/vendor/Main"),
	})
	userJar := mustWriteJar(t, dir, "user.jar", map[string][]byte{
		"com/user/User.class": classReferencing("com/user/User", "org/slf4j/Logger"),
	})

	deps := InferDependencies(map[string]*Mod{
		"vendor": modAt(t, "vendor", vendorJar),
		"user":   modAt(t, "user", userJar),
	})
	if len(deps) != 0 {
		t.Errorf("expected vendored classes to not create dependencies, got %+v", deps)
	}
}

func TestInferDependenciesSkipsAmbiguousClasses(t *testing.T) {
	dir := t.TempDir()

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		"com/shared/Foo.class": classDeclaring("com/shared/Foo"),
	})
	cJar := mustWriteJar(t, dir, "c.jar", map[string][]byte{
		"com/shared/Foo.class": classDeclaring("com/shared/Foo"),
	})
	bJar := mustWriteJar(t, dir, "b.jar", map[string][]byte{
		"com/b/Bar.class": classReferencing("com/b/Bar", "com/shared/Foo"),
	})

	deps := InferDependencies(map[string]*Mod{
		"a": modAt(t, "a", aJar),
		"b": modAt(t, "b", bJar),
		"c": modAt(t, "c", cJar),
	})
	if len(deps) != 0 {
		t.Errorf("expected ambiguous classes to be ignored, got %+v", deps)
	}
}

func TestInferDependenciesSkipsSelfAndUnknown(t *testing.T) {
	dir := t.TempDir()

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		"com/a/Foo.class": classReferencing("com/a/Foo", "com/a/Foo", "java/lang/Object"),
	})

	deps := InferDependencies(map[string]*Mod{"a": modAt(t, "a", aJar)})
	if len(deps) != 0 {
		t.Errorf("expected self and unknown references to be ignored, got %+v", deps)
	}
}

func TestInferDependenciesSkipsMissingClassIndex(t *testing.T) {
	dir := t.TempDir()

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		"com/a/Foo.class": classDeclaring("com/a/Foo"),
	})
	bJar := mustWriteJar(t, dir, "b.jar", map[string][]byte{
		"com/b/Bar.class": classReferencing("com/b/Bar", "com/a/Foo"),
	})

	b := modAt(t, "b", bJar)
	b.ClassIndex = nil

	deps := InferDependencies(map[string]*Mod{
		"a": modAt(t, "a", aJar),
		"b": b,
	})
	if len(deps) != 0 {
		t.Errorf("expected unindexed mod to be skipped, got %+v", deps)
	}
}

func TestApplyInferredDependencies(t *testing.T) {
	allMods := map[string]*Mod{
		"a": {Metadata: ModMetadata{ID: "a"}},
		"b": {Metadata: ModMetadata{ID: "b"}},
	}
	deps := []InferredDependency{
		{SourceID: "b", TargetID: "a", Classes: []string{"com/a/Foo"}},
	}
	applied := ApplyInferredDependencies(allMods, deps)
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied dependency, got %d", len(applied))
	}
	if allMods["b"].Metadata.Depends["a"] == nil {
		t.Fatal("expected mod b to now depend on a")
	}

	// Second apply should be a no-op because it's already present.
	appliedAgain := ApplyInferredDependencies(allMods, deps)
	if len(appliedAgain) != 0 {
		t.Fatalf("expected 0 reapplied dependencies, got %d", len(appliedAgain))
	}
}

func TestInferDependenciesSkipsSuggestsAndConflicts(t *testing.T) {
	dir := t.TempDir()

	bJar := mustWriteJar(t, dir, "b.jar", map[string][]byte{
		"com/b/B.class": classDeclaring("com/b/B"),
	})
	bMod := modAt(t, "b", bJar)

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		"com/a/A.class": classReferencing("com/a/A", "com/b/B"),
	})

	// Case 1: Mod A declares Suggests: b (optional dependency)
	aModSuggests := modAt(t, "a", aJar)
	aModSuggests.Metadata.Suggests = VersionRanges{"b": {version.Any()}}

	deps := InferDependencies(map[string]*Mod{"a": aModSuggests, "b": bMod})
	if len(deps) != 0 {
		t.Errorf("expected no dependency when target is in Suggests, got %+v", deps)
	}

	// Case 2: Mod A declares Breaks: b (incompatible mod)
	aModBreaks := modAt(t, "a", aJar)
	aModBreaks.Metadata.Breaks = VersionRanges{"b": {version.Any()}}

	deps = InferDependencies(map[string]*Mod{"a": aModBreaks, "b": bMod})
	if len(deps) != 0 {
		t.Errorf("expected no dependency when target is in Breaks, got %+v", deps)
	}
}
