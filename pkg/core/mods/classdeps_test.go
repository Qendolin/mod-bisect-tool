package mods

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
)

// mustWriteJar writes a zip archive to dir and returns its full path.
func mustWriteJar(t *testing.T, dir, name string, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, zipWith(t, entries), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// classDeclaring builds class file bytes for a class that declares itself.
func classDeclaring(internalName string) []byte {
	return classFileBytes(2, 3, cpUtf8(internalName), cpClass(1))
}

// classReferencing builds class file bytes for a class that declares itself
// and references other internal class names.
func classReferencing(internalName string, refs ...string) []byte {
	// #1 own name, #2.. Utf8 entries for the refs, then one Class entry per
	// Utf8, and this_class last.
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

// modAt builds a Mod for the jar with the class index populated, mirroring
// what the loader does at load time.
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

// TestVendoredClassPrefixes spot-checks the vendored-library exclusion list:
// common shaded library packages are excluded, while Fabric API, QSL and
// ordinary mod packages remain eligible as dependency targets.
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
		t.Errorf("expected referenced class %q from the nested jar, got %v", "com/pkg/A", sortedKeys(index.Referenced))
	}
}

func TestIndexJarClassesMissingFileErrors(t *testing.T) {
	if _, err := IndexJarClasses(filepath.Join(t.TempDir(), "does-not-exist.jar")); err == nil {
		t.Fatal("expected an error for a missing jar file, got nil")
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

func TestInferPotentialDependencies(t *testing.T) {
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

	deps := InferPotentialDependencies(map[string]*Mod{
		"a": modAt(t, "a", aJar),
		"b": modAt(t, "b", bJar),
		"c": modAt(t, "c", cJar),
	})

	if len(deps) != 1 {
		t.Fatalf("expected exactly 1 potential dependency, got %+v", deps)
	}
	dep := deps[0]
	if dep.SourceID != "b" || dep.TargetID != "a" {
		t.Errorf("expected dependency b -> a, got %s -> %s", dep.SourceID, dep.TargetID)
	}
	if !slices.Equal(dep.Classes, []string{"com/a/Foo"}) {
		t.Errorf("expected classes [com/a/Foo], got %v", dep.Classes)
	}
}

func TestInferPotentialDependenciesSkipsAlreadyDeclared(t *testing.T) {
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

	if deps := InferPotentialDependencies(map[string]*Mod{"a": modAt(t, "a", aJar), "b": newMod()}); len(deps) != 0 {
		t.Errorf("expected no potential dependency when already declared, got %+v", deps)
	}

	// A dependency on one of the target's provided IDs also counts as declared.
	a := modAt(t, "a", aJar)
	a.Metadata.Provides = []string{"a-provided"}
	providedMod := newMod()
	providedMod.Metadata.Depends = VersionRanges{"a-provided": {version.Any()}}
	if deps := InferPotentialDependencies(map[string]*Mod{"a": a, "b": providedMod}); len(deps) != 0 {
		t.Errorf("expected no potential dependency when declared via provides, got %+v", deps)
	}
}

// TestInferPotentialDependenciesSkipsNestedModuleProvides verifies that a
// dependency on an ID provided by one of the target's nested modules counts
// as already declared (EffectiveProvides), so no dependency on the container
// mod is inferred.
func TestInferPotentialDependenciesSkipsNestedModuleProvides(t *testing.T) {
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

	deps := InferPotentialDependencies(map[string]*Mod{"container": container, "user": user})
	if len(deps) != 0 {
		t.Errorf("expected dependency on nested-module provide to count as declared, got %+v", deps)
	}
}

// TestInferPotentialDependenciesSkipsVendoredClasses verifies the
// minecord-style case: a mod that vendors library classes (e.g.
// org/slf4j/Logger) into its own jar does not become a dependency target for
// other mods referencing those classes.
func TestInferPotentialDependenciesSkipsVendoredClasses(t *testing.T) {
	dir := t.TempDir()

	vendorJar := mustWriteJar(t, dir, "vendor.jar", map[string][]byte{
		"org/slf4j/Logger.class": classDeclaring("org/slf4j/Logger"),
		"com/vendor/Main.class":  classDeclaring("com/vendor/Main"),
	})
	userJar := mustWriteJar(t, dir, "user.jar", map[string][]byte{
		"com/user/User.class": classReferencing("com/user/User", "org/slf4j/Logger"),
	})

	deps := InferPotentialDependencies(map[string]*Mod{
		"vendor": modAt(t, "vendor", vendorJar),
		"user":   modAt(t, "user", userJar),
	})
	if len(deps) != 0 {
		t.Errorf("expected vendored classes to not create dependencies, got %+v", deps)
	}
}

func TestInferPotentialDependenciesSkipsAmbiguousClasses(t *testing.T) {
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

	deps := InferPotentialDependencies(map[string]*Mod{
		"a": modAt(t, "a", aJar),
		"b": modAt(t, "b", bJar),
		"c": modAt(t, "c", cJar),
	})
	if len(deps) != 0 {
		t.Errorf("expected ambiguous classes to be ignored, got %+v", deps)
	}
}

func TestInferPotentialDependenciesSkipsSelfAndUnknown(t *testing.T) {
	dir := t.TempDir()

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		// References its own class and a class nobody declares.
		"com/a/Foo.class": classReferencing("com/a/Foo", "com/a/Foo", "java/lang/Object"),
	})

	deps := InferPotentialDependencies(map[string]*Mod{"a": modAt(t, "a", aJar)})
	if len(deps) != 0 {
		t.Errorf("expected self and unknown references to be ignored, got %+v", deps)
	}
}

// TestInferPotentialDependenciesSkipsMissingClassIndex verifies that a mod
// without a class index (e.g. hand-built) is skipped instead of aborting the
// inference.
func TestInferPotentialDependenciesSkipsMissingClassIndex(t *testing.T) {
	dir := t.TempDir()

	aJar := mustWriteJar(t, dir, "a.jar", map[string][]byte{
		"com/a/Foo.class": classDeclaring("com/a/Foo"),
	})
	bJar := mustWriteJar(t, dir, "b.jar", map[string][]byte{
		"com/b/Bar.class": classReferencing("com/b/Bar", "com/a/Foo"),
	})

	b := modAt(t, "b", bJar)
	b.ClassIndex = nil // Unindexed mod.

	deps := InferPotentialDependencies(map[string]*Mod{
		"a": modAt(t, "a", aJar),
		"b": b,
	})
	if len(deps) != 0 {
		t.Errorf("expected the unindexed mod to be skipped, got %+v", deps)
	}
}
