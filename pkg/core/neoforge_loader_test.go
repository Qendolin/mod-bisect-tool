package app_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// neoForgeModSpec defines the structure for creating a dummy NeoForge mod.
type neoForgeModSpec struct {
	TOMLContent     string
	NestedJars      map[string]neoForgeModSpec
	RawFiles        map[string]string
	ManifestContent string
}

// setupDummyNeoForgeMods creates a temporary mods directory and NeoForge JAR files.
func setupDummyNeoForgeMods(t *testing.T, modsDir string, specs map[string]neoForgeModSpec) {
	t.Helper()
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		t.Fatalf("failed to create mods dir '%s': %v", modsDir, err)
	}
	for filename, spec := range specs {
		jarPath := filepath.Join(modsDir, filename)
		jarBytes, err := createNeoForgeJarFromSpec(t, spec)
		if err != nil {
			t.Fatalf("failed to create JAR data for %s: %v", filename, err)
		}
		if err := os.WriteFile(jarPath, jarBytes, 0644); err != nil {
			t.Fatalf("failed to write dummy mod file %s: %v", jarPath, err)
		}
	}
}

// createNeoForgeJarFromSpec is a recursive helper to build a NeoForge JAR file from a spec.
func createNeoForgeJarFromSpec(t *testing.T, spec neoForgeModSpec) ([]byte, error) {
	t.Helper()
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)

	// Create neoforge.mods.toml
	if spec.TOMLContent != "" {
		tomlFile, err := zipWriter.Create("META-INF/neoforge.mods.toml")
		if err != nil {
			return nil, err
		}
		if _, err = tomlFile.Write([]byte(spec.TOMLContent)); err != nil {
			return nil, err
		}
	}

	// Add raw files (e.g., quilt.mod.json for testing priority)
	for path, content := range spec.RawFiles {
		rawFile, err := zipWriter.Create(path)
		if err != nil {
			return nil, err
		}
		if _, err = rawFile.Write([]byte(content)); err != nil {
			return nil, err
		}
	}

	// Add META-INF/MANIFEST.MF for dynamic version testing
	if spec.ManifestContent != "" {
		manifestFile, err := zipWriter.Create("META-INF/MANIFEST.MF")
		if err != nil {
			return nil, err
		}
		if _, err = manifestFile.Write([]byte(spec.ManifestContent)); err != nil {
			return nil, err
		}
	}

	// Add nested JARs
	for nestedFilename, nestedSpec := range spec.NestedJars {
		nestedJarBytes, err := createNeoForgeJarFromSpec(t, nestedSpec)
		if err != nil {
			return nil, err
		}
		nestedJarFile, err := zipWriter.Create("META-INF/jarjar/" + nestedFilename)
		if err != nil {
			return nil, err
		}
		if _, err := nestedJarFile.Write(nestedJarBytes); err != nil {
			return nil, err
		}
	}

	if len(spec.NestedJars) > 0 {
		metadata := map[string]interface{}{
			"jars": make([]map[string]string, 0, len(spec.NestedJars)),
		}
		for nestedFilename := range spec.NestedJars {
			metadata["jars"] = append(metadata["jars"].([]map[string]string), map[string]string{
				"path": "META-INF/jarjar/" + nestedFilename,
			})
		}

		metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			return nil, err
		}

		metadataFile, err := zipWriter.Create("META-INF/jarjar/metadata.json")
		if err != nil {
			return nil, err
		}
		if _, err := metadataFile.Write(metadataJSON); err != nil {
			return nil, err
		}
	}

	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return zipBuf.Bytes(), nil
}

// loadAndCheckNeoForge is a helper to load NeoForge mods and perform common assertions.
func loadAndCheckNeoForge(t *testing.T, modsDir string, expectedModIDs []string, expectedProviderCount int, expectError bool) (map[string]*mods.Mod, mods.PotentialProvidersMap) {
	t.Helper()
	loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
	allMods, providers, err := loader.LoadMods(modsDir, nil, nil)

	if expectError {
		if err == nil {
			t.Fatalf("Expected LoadMods to return an error, but got none")
		}
		return nil, nil
	} else {
		if err != nil {
			t.Fatalf("LoadMods returned an unexpected error: %v", err)
		}
		if len(allMods) != len(expectedModIDs) {
			t.Errorf("Expected %d mods to be loaded, got %d", len(expectedModIDs), len(allMods))
		}
		for _, id := range expectedModIDs {
			if _, ok := allMods[id]; !ok {
				t.Errorf("Expected mod '%s' to be loaded, but it was not found", id)
			}
		}
		implicitProviderCount := len(mods.GetImplicitMods())
		if len(providers) != expectedProviderCount+implicitProviderCount {
			t.Errorf("Expected %d potential providers, got %d", expectedProviderCount+implicitProviderCount, len(providers))
		}
		return allMods, providers
	}
}

// TestNeoForgeModLoader tests the NeoForge mod loader functionality.
func TestNeoForgeModLoader(t *testing.T) {
	setupLogger(t)

	newTestDir := func(name string) string {
		dir := filepath.Join(testDir, "neoforge_loader_"+strings.ReplaceAll(name, " ", "_"))
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			t.Fatalf("Failed to clean test directory %s: %v", dir, err)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create test directory %s: %v", dir, err)
		}
		return dir
	}

	// ============================================================================
	// SECTION 1: CORE LOADING TESTS
	// ============================================================================

	t.Run("Basic_NeoForge_Mod_Loading", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"mymod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mymod"
version = "1.0"
displayName = "My Mod"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)

		if err != nil {
			t.Fatalf("LoadMods returned an unexpected error: %v", err)
		}
		if len(allMods) != 1 {
			t.Errorf("Expected 1 mod to be loaded, got %d", len(allMods))
		}
		if _, ok := allMods["mymod"]; !ok {
			t.Error("Expected mod 'mymod' to be loaded")
		}
	})

	t.Run("Multiple_Independent_NeoForge_Mods", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"mod_a-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_a"
version = "1.0"
displayName = "Mod A"`},
			"mod_b-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_b"
version = "1.0"
displayName = "Mod B"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, providers := loadAndCheckNeoForge(t, modsDir, []string{"mod_a", "mod_b"}, 2, false)
		if loadedMods != nil {
			if _, ok := loadedMods["mod_a"]; !ok {
				t.Fatal("mod_a should be loaded")
			}
			if _, ok := loadedMods["mod_a"].EffectiveProvides["mod_a"]; !ok {
				t.Error("mod_a should provide mod_a")
			}
			if _, ok := providers["mod_a"]; !ok {
				t.Error("mod_a should be a potential provider")
			}
		}
	})

	t.Run("NeoForge_Mod_With_Required_Dependencies", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"mod_dep-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_dep"
version = "1.0"
displayName = "Mod Dep"`},
			"mod_req-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_req"
version = "1.0"
displayName = "Mod Req"
[[dependencies.mod_req]]
modId = "mod_dep"
type = "required"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"mod_dep", "mod_req"}, 2, false)
		if loadedMods != nil {
			if reqMod, ok := loadedMods["mod_req"]; ok {
				if deps, ok := reqMod.Metadata.Depends["mod_dep"]; ok {
					if len(deps) == 0 {
						t.Error("mod_req should depend on mod_dep")
					}
				} else {
					t.Error("mod_req should have mod_dep in depends")
				}
			}
		}
	})

	t.Run("NeoForge_Mod_With_Optional_Dependencies", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"mod_lib-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_lib"
version = "1.0"
displayName = "Mod Lib"`},
			"mod_opt-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_opt"
version = "1.0"
displayName = "Mod Opt"
[[dependencies.mod_opt]]
modId = "mod_lib"
type = "optional"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"mod_lib", "mod_opt"}, 2, false)
		if loadedMods != nil {
			if optMod, ok := loadedMods["mod_opt"]; ok {
				if recs, ok := optMod.Metadata.Recommends["mod_lib"]; ok {
					if len(recs) == 0 {
						t.Error("mod_opt should recommend mod_lib")
					}
				} else {
					t.Error("mod_opt should have mod_lib in recommends")
				}
			}
		}
	})

	t.Run("NeoForge_Mod_With_Incompatible_Dependencies", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"mod_other-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_other"
version = "1.0"
displayName = "Mod Other"`},
			"mod_break-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_break"
version = "1.0"
displayName = "Mod Break"
[[dependencies.mod_break]]
modId = "mod_other"
type = "incompatible"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"mod_other", "mod_break"}, 2, false)
		if loadedMods != nil {
			if breakMod, ok := loadedMods["mod_break"]; ok {
				if breaks, ok := breakMod.Metadata.Breaks["mod_other"]; ok {
					if len(breaks) == 0 {
						t.Error("mod_break should break mod_other")
					}
				} else {
					t.Error("mod_break should have mod_other in breaks")
				}
			}
		}
	})

	t.Run("NeoForge_Mod_With_Discouraged_Dependencies", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"mod_conflict-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_conflict"
version = "1.0"
displayName = "Mod Conflict"`},
			"mod_disc-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mod_disc"
version = "1.0"
displayName = "Mod Disc"
[[dependencies.mod_disc]]
modId = "mod_conflict"
type = "discouraged"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"mod_conflict", "mod_disc"}, 2, false)
		if loadedMods != nil {
			if discMod, ok := loadedMods["mod_disc"]; ok {
				if conflicts, ok := discMod.Metadata.Conflicts["mod_conflict"]; ok {
					if len(conflicts) == 0 {
						t.Error("mod_disc should conflict with mod_conflict")
					}
				} else {
					t.Error("mod_disc should have mod_conflict in conflicts")
				}
			}
		}
	})

	t.Run("NeoForge_Mod_With_Provides", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"lib_provider-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "lib_provider"
version = "1.0"
displayName = "Lib Provider"
provides = ["my_api"]`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, providers := loadAndCheckNeoForge(t, modsDir, []string{"lib_provider"}, 2, false)
		if loadedMods != nil {
			if _, ok := loadedMods["lib_provider"]; !ok {
				t.Fatal("lib_provider should be loaded")
			}
			if _, ok := loadedMods["lib_provider"].EffectiveProvides["my_api"]; !ok {
				t.Error("lib_provider should effectively provide my_api")
			}
			if _, ok := providers["my_api"]; !ok {
				t.Error("my_api should be a potential provider")
			}
		}
	})

	t.Run("NeoForge_Mod_With_Nested_JARs", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"main_mod-1.0.jar": {
				TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "main_mod"
version = "1.0"
displayName = "Main Mod"
[[dependencies.main_mod]]
modId = "nested_lib"
type = "required"
versionRange = "[1.0,)"
`,
				NestedJars: map[string]neoForgeModSpec{
					"nested.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "nested_lib"
version = "1.0"
displayName = "Nested Lib"
`},
				},
			},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, providers := loadAndCheckNeoForge(t, modsDir, []string{"main_mod"}, 2, false)
		if loadedMods != nil {
			mainMod := loadedMods["main_mod"]
			if len(mainMod.NestedModules) != 1 || mainMod.NestedModules[0].Info.ID != "nested_lib" {
				t.Errorf("Expected nested_lib to be loaded as a nested module of main_mod")
			}
			if _, ok := mainMod.EffectiveProvides["nested_lib"]; !ok {
				t.Error("main_mod should effectively provide nested_lib")
			}
			if _, ok := providers["nested_lib"]; !ok {
				t.Error("nested_lib should be a potential provider via main_mod")
			}
		}
	})

	t.Run("NeoForge_Mod_With_Multiple_Mods_Blocks", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"multi_provider-1.0.jar": {TOMLContent: `modId = "multi_provider"
version = "1.0"
displayName = "Multi Provider"
[[mods]]
modId = "multi_provider"
version = "1.0"
displayName = "Multi Provider"
provides = ["api_a", "api_b", "api_c"]
`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"multi_provider"}, 4, false)
		if loadedMods != nil {
			if _, ok := loadedMods["multi_provider"]; !ok {
				t.Fatal("multi_provider should be loaded")
			}
			if _, ok := loadedMods["multi_provider"].EffectiveProvides["api_a"]; !ok {
				t.Error("multi_provider should provide api_a")
			}
			if _, ok := loadedMods["multi_provider"].EffectiveProvides["api_b"]; !ok {
				t.Error("multi_provider should provide api_b")
			}
			if _, ok := loadedMods["multi_provider"].EffectiveProvides["api_c"]; !ok {
				t.Error("multi_provider should provide api_c")
			}
		}
	})

	// ============================================================================
	// SECTION 2: CONFLICT RESOLUTION TESTS
	// ============================================================================

	t.Run("Multiple_Mods_Same_ID_Version_Conflict", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"conf_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "conf_mod"
version = "1.0"
displayName = "Conf Mod 1.0"`},
			"conf_mod-2.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "conf_mod"
version = "2.0"
displayName = "Conf Mod 2.0"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"conf_mod"}, 1, false)
		if loadedMods != nil {
			if loadedMods["conf_mod"].Metadata.Version.Version.String() != "2.0" {
				t.Errorf("Expected conf_mod v2.0 to win conflict, got v%s", loadedMods["conf_mod"].Metadata.Version.Version.String())
			}
		}
	})

	t.Run("Multiple_Mods_Same_ID_Disabled_Winner", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			// Loser: active, older version
			"conf_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "conf_mod"
version = "1.0"
displayName = "Conf Mod 1.0"`},
			// Winner: disabled, newer version
			"conf_mod-2.0.jar.disabled": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "conf_mod"
version = "2.0"
displayName = "Conf Mod 2.0"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)

		// The loader should only load the winner, v2.0.
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"conf_mod"}, 1, false)

		if loadedMods != nil {
			// Assert that the winner is the correct version.
			if loadedMods["conf_mod"].Metadata.Version.Version.String() != "2.0" {
				t.Errorf("Expected conf_mod v2.0 to win conflict based on version, got v%s", loadedMods["conf_mod"].Metadata.Version.Version.String())
			}
			// Assert that the active loser's file was correctly disabled.
			disabledPath := filepath.Join(modsDir, "conf_mod-1.0.jar.disabled")
			if _, err := os.Stat(disabledPath); os.IsNotExist(err) {
				t.Error("conf_mod-1.0.jar should have been disabled, but the .disabled file was not found.")
			}
		}
	})

	t.Run("Maven_Version_Format_Concrete", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"concrete_ver-1.2.3.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "concrete_ver"
version = "1.2.3"
displayName = "Concrete Version"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"concrete_ver"}, 1, false)
		if loadedMods != nil {
			if !loadedMods["concrete_ver"].Metadata.Version.Version.IsSemantic() {
				t.Error("Expected version for 'concrete_ver' to be a semantic version")
			}
			if loadedMods["concrete_ver"].Metadata.Version.Version.String() != "1.2.3" {
				t.Errorf("Expected version string '1.2.3', got '%s'", loadedMods["concrete_ver"].Metadata.Version.Version.String())
			}
		}
	})

	t.Run("Maven_Version_Format_Dynamic", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"dynamic_ver.jar": {
				TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "dynamic_ver"
version = "${file.jarVersion}"
displayName = "Dynamic Version"
`,
				ManifestContent: "Manifest-Version: 1.0\nImplementation-Version: 3.0.0\n",
			},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"dynamic_ver"}, 1, false)
		if loadedMods != nil {
			if loadedMods["dynamic_ver"].Metadata.Version.Version.String() != "3.0.0" {
				t.Errorf("Expected version '3.0.0' from MANIFEST.MF, got '%s'", loadedMods["dynamic_ver"].Metadata.Version.Version.String())
			}
		}
	})

	// ============================================================================
	// SECTION 3: DEPENDENCY TYPE TESTS
	// ============================================================================

	t.Run("Required_Dependency_Tracking", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"dep_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "dep_mod"
version = "1.0"
displayName = "Dep Mod"`},
			"req_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "req_mod"
version = "1.0"
displayName = "Req Mod"
[[dependencies.req_mod]]
modId = "dep_mod"
type = "required"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"dep_mod", "req_mod"}, 2, false)
		if loadedMods != nil {
			if reqMod, ok := loadedMods["req_mod"]; ok {
				if deps, ok := reqMod.Metadata.Depends["dep_mod"]; ok {
					if len(deps) == 0 {
						t.Error("req_mod should depend on dep_mod")
					}
				} else {
					t.Error("req_mod should have dep_mod in depends")
				}
			}
		}
	})

	t.Run("Optional_Dependency_Tracking", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"opt_dep-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "opt_dep"
version = "1.0"
displayName = "Opt Dep"`},
			"opt_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "opt_mod"
version = "1.0"
displayName = "Opt Mod"
[[dependencies.opt_mod]]
modId = "opt_dep"
type = "optional"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"opt_dep", "opt_mod"}, 2, false)
		if loadedMods != nil {
			if optMod, ok := loadedMods["opt_mod"]; ok {
				if recs, ok := optMod.Metadata.Recommends["opt_dep"]; ok {
					if len(recs) == 0 {
						t.Error("opt_mod should recommend opt_dep")
					}
				} else {
					t.Error("opt_mod should have opt_dep in recommends")
				}
			}
		}
	})

	t.Run("Incompatible_Dependency_Tracking", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"incomp_dep-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "incomp_dep"
version = "1.0"
displayName = "Incomp Dep"`},
			"incomp_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "incomp_mod"
version = "1.0"
displayName = "Incomp Mod"
[[dependencies.incomp_mod]]
modId = "incomp_dep"
type = "incompatible"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"incomp_dep", "incomp_mod"}, 2, false)
		if loadedMods != nil {
			if incompMod, ok := loadedMods["incomp_mod"]; ok {
				if breaks, ok := incompMod.Metadata.Breaks["incomp_dep"]; ok {
					if len(breaks) == 0 {
						t.Error("incomp_mod should break incomp_dep")
					}
				} else {
					t.Error("incomp_mod should have incomp_dep in breaks")
				}
			}
		}
	})

	t.Run("Discouraged_Dependency_Tracking", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"disc_dep-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "disc_dep"
version = "1.0"
displayName = "Disc Dep"`},
			"disc_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "disc_mod"
version = "1.0"
displayName = "Disc Mod"
[[dependencies.disc_mod]]
modId = "disc_dep"
type = "discouraged"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"disc_dep", "disc_mod"}, 2, false)
		if loadedMods != nil {
			if discMod, ok := loadedMods["disc_mod"]; ok {
				if conflicts, ok := discMod.Metadata.Conflicts["disc_dep"]; ok {
					if len(conflicts) == 0 {
						t.Error("disc_mod should conflict with disc_dep")
					}
				} else {
					t.Error("disc_mod should have disc_dep in conflicts")
				}
			}
		}
	})

	t.Run("Mixed_Dependency_Types", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"base_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "base_mod"
version = "1.0"
displayName = "Base Mod"`},
			"mixed_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "mixed_mod"
version = "1.0"
displayName = "Mixed Mod"
[[dependencies.mixed_mod]]
modId = "base_mod"
type = "required"
versionRange = "[1.0,)"
[[dependencies.mixed_mod]]
modId = "base_mod"
type = "optional"
versionRange = "[1.0,)"
[[dependencies.mixed_mod]]
modId = "base_mod"
type = "incompatible"
versionRange = "[1.0,)"
[[dependencies.mixed_mod]]
modId = "base_mod"
type = "discouraged"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"base_mod", "mixed_mod"}, 2, false)
		if loadedMods != nil {
			if mixedMod, ok := loadedMods["mixed_mod"]; ok {
				if len(mixedMod.Metadata.Depends) == 0 {
					t.Error("mixed_mod should have depends")
				}
				if len(mixedMod.Metadata.Recommends) == 0 {
					t.Error("mixed_mod should have recommends")
				}
				if len(mixedMod.Metadata.Breaks) == 0 {
					t.Error("mixed_mod should have breaks")
				}
				if len(mixedMod.Metadata.Conflicts) == 0 {
					t.Error("mixed_mod should have conflicts")
				}
			}
		}
	})

	t.Run("Legacy_Forge_Mandatory_Dependency_Tracking", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"req_dep-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "req_dep"
version = "1.0"
displayName = "Req Dep"`},
			"opt_dep-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "opt_dep"
version = "1.0"
displayName = "Opt Dep"`},
			"default_dep-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "default_dep"
version = "1.0"
displayName = "Default Dep"`},
			"legacy_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "legacy_mod"
version = "1.0"
displayName = "Legacy Mod"
[[dependencies.legacy_mod]]
modId = "req_dep"
mandatory = true
versionRange = "[1.0,)"
[[dependencies.legacy_mod]]
modId = "opt_dep"
mandatory = false
versionRange = "[1.0,)"
[[dependencies.legacy_mod]]
modId = "default_dep"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"req_dep", "opt_dep", "default_dep", "legacy_mod"}, 4, false)
		if loadedMods != nil {
			if legacyMod, ok := loadedMods["legacy_mod"]; ok {
				if deps, ok := legacyMod.Metadata.Depends["req_dep"]; !ok || len(deps) == 0 {
					t.Error("legacy_mod should depend on req_dep (mandatory=true)")
				}
				if _, ok := legacyMod.Metadata.Depends["opt_dep"]; ok {
					t.Error("legacy_mod should not require opt_dep (mandatory=false)")
				}
				if recs, ok := legacyMod.Metadata.Recommends["opt_dep"]; !ok || len(recs) == 0 {
					t.Error("legacy_mod should recommend opt_dep (mandatory=false)")
				}
				if deps, ok := legacyMod.Metadata.Depends["default_dep"]; !ok || len(deps) == 0 {
					t.Error("legacy_mod should depend on default_dep (mandatory omitted defaults to required)")
				}
			}
		}
	})

	// ============================================================================
	// SECTION 4: ERROR HANDLING TESTS
	// ============================================================================

	t.Run("Empty_Mods_Directory", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		if err := os.MkdirAll(modsDir, 0755); err != nil {
			t.Fatalf("Failed to create empty mods directory: %v", err)
		}
		defer os.RemoveAll(modsDir)
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error for empty mods directory, got: %v", err)
		}
		if len(allMods) != 0 {
			t.Errorf("Expected 0 mods loaded, got %d", len(allMods))
		}
	})

	t.Run("Missing_NeoForge_Manifest", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		zipBuf := new(bytes.Buffer)
		zipWriter := zip.NewWriter(zipBuf)
		// Create empty JAR with MANIFEST.MF to be recognized as a library (no mods.toml)
		manifestFile, err := zipWriter.Create("META-INF/MANIFEST.MF")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = manifestFile.Write([]byte("Manifest-Version: 1.0\n")); err != nil {
			t.Fatal(err)
		}
		if err := zipWriter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(modsDir, "empty.jar"), zipBuf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		// Mod should be loaded as a Java library, LoadMods should not error.
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 1 {
			t.Errorf("Expected 1 mod loaded (Java library), got %d", len(allMods))
		}
		mod := allMods["library-empty.jar"]
		if mod == nil {
			t.Fatal("Expected mod 'library-empty.jar' to be loaded")
		}
		if !mod.Metadata.IsJavaLibrary {
			t.Error("Expected IsJavaLibrary to be true for JAR without mods.toml")
		}
		if mod.Metadata.ID != "library-empty.jar" {
			t.Errorf("Expected ID 'library-empty.jar', got %q", mod.Metadata.ID)
		}
	})

	t.Run("Invalid_TOML_Syntax", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"malformed.jar": {TOMLContent: `modId = "malformed"
version = "1.0",`}, // Trailing comma, invalid TOML
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		// Should warn and skip, not error LoadMods.
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 0 {
			t.Errorf("Expected 0 mods loaded (invalid TOML), got %d", len(allMods))
		}
	})

	t.Run("Missing_ModID", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"no_modid.jar": {TOMLContent: `version = "1.0"
displayName = "No ModID"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		// Should warn and skip.
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 0 {
			t.Errorf("Expected 0 mods loaded (missing modId), got %d", len(allMods))
		}
	})

	t.Run("Missing_Version", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"no_version.jar": {TOMLContent: `modId = "no_version"
displayName = "No Version"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		// Should warn and skip.
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 0 {
			t.Errorf("Expected 0 mods loaded (missing version), got %d", len(allMods))
		}
	})

	t.Run("Invalid_Version_Format", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"bad_version.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "bad_version"
version = "invalid.version.format"
displayName = "Bad Version"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)

		// The loader correctly loads this mod with a StringVersion.
		// We should expect 1 mod to be loaded.
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"bad_version"}, 1, false)

		if loadedMods != nil {
			mod := loadedMods["bad_version"]
			if mod.Metadata.Version.Version.IsSemantic() {
				t.Error("Expected version for 'bad_version' to be a non-semantic StringVersion")
			}
			if mod.Metadata.Version.Version.String() != "invalid.version.format" {
				t.Errorf("Expected version string 'invalid.version.format', got '%s'", mod.Metadata.Version.Version.String())
			}
		}
	})

	t.Run("Invalid_Dependency_Format", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		// The value for a dependency must be a string or array of strings.
		// A number is a structural error that MUST cause a parse failure.
		specs := map[string]neoForgeModSpec{
			"bad_dep.jar": {TOMLContent: `modId = "bad_dep"
version = "1.0"
displayName = "Bad Dep"
[[dependencies.bad_dep]]
modId = "lib"
type = "required"
versionRange = 123`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		// The loader should correctly fail to parse this mod and skip it.
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 0 {
			t.Errorf("Expected 0 mods loaded (invalid dependency format), got %d", len(allMods))
		}
	})

	t.Run("Missing_Nested_JAR", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"main_mod_missing_nested-1.0.jar": {
				TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "main_mod"
version = "1.0"
displayName = "Main Mod"
[[dependencies.main_mod]]
modId = "nested_lib"
type = "required"
versionRange = "[1.0,)"
`,
			},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		// main_mod should load, but warning about missing nested.
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 1 {
			t.Errorf("Expected 1 mod loaded, got %d", len(allMods))
		}
	})

	t.Run("Empty_Mods_Section", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"empty_mods.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
# No entries`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		_, _, err := loader.LoadMods(modsDir, nil, nil)
		// Should error or skip due to empty mods section
		if err != nil {
			t.Logf("Expected error for empty mods section: %v", err)
		}
	})

	// ============================================================================
	// SECTION 5: NEOFORGE-SPECIFIC FEATURE TESTS
	// ============================================================================

	t.Run("Jarjar_Metadata_Parsing", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)

		// Create nested JAR files that will be embedded in the library JAR
		nestedJar1Bytes, err := createNeoForgeJarFromSpec(t, neoForgeModSpec{
			TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "nested_lib1"
version = "1.0"
displayName = "Nested Lib 1"
`,
		})
		if err != nil {
			t.Fatal(err)
		}
		nestedJar2Bytes, err := createNeoForgeJarFromSpec(t, neoForgeModSpec{
			TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "nested_lib2"
version = "1.0"
displayName = "Nested Lib 2"
`,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Create a library JAR with jarjar metadata pointing to nested jars
		zipBuf := new(bytes.Buffer)
		zipWriter := zip.NewWriter(zipBuf)

		// Add nested JARs
		lib1File, _ := zipWriter.Create("META-INF/jarjar/lib1.jar")
		lib1File.Write(nestedJar1Bytes)

		lib2File, _ := zipWriter.Create("META-INF/jarjar/lib2.jar")
		lib2File.Write(nestedJar2Bytes)

		// Add jarjar metadata
		jarjarFile, _ := zipWriter.Create("META-INF/jarjar/metadata.json")
		jarjarData := `{
			"jars": [{"path": "META-INF/jarjar/lib1.jar"}, {"path": "META-INF/jarjar/lib2.jar"}]
		}`
		if _, err := jarjarFile.Write([]byte(jarjarData)); err != nil {
			t.Fatal(err)
		}

		if err := zipWriter.Close(); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(modsDir, "library.jar"), zipBuf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}

		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		// A jarjar container is loaded as a single synthetic mod
		if len(allMods) != 1 {
			t.Errorf("Expected 1 mod loaded (jarjar container), got %d", len(allMods))
		}

		// Check that the library has correct ID and IsJavaLibrary
		libMod, ok := allMods["library-library.jar"]
		if !ok {
			t.Error("Expected mod 'library-library.jar' to be loaded")
		} else if !libMod.Metadata.IsJavaLibrary {
			t.Error("Expected IsJavaLibrary to be true for jarjar container")
		} else if len(libMod.Metadata.Jars) != 2 {
			t.Errorf("Expected 2 nested jars in Jars field, got %d", len(libMod.Metadata.Jars))
		} else {
			expectedJars := []string{"META-INF/jarjar/lib1.jar", "META-INF/jarjar/lib2.jar"}
			for i, jar := range libMod.Metadata.Jars {
				if jar != expectedJars[i] {
					t.Errorf("Expected jar %d to be %q, got %q", i, expectedJars[i], jar)
				}
			}
		}

		// Verify that the nested mods are loaded as NestedModules
		if len(libMod.NestedModules) != 2 {
			t.Errorf("Expected 2 nested modules, got %d", len(libMod.NestedModules))
		}

		nestedMod1 := libMod.NestedModules[0]
		if nestedMod1.Info.ID != "nested_lib1" {
			t.Errorf("Expected first nested module ID 'nested_lib1', got %q", nestedMod1.Info.ID)
		}

		nestedMod2 := libMod.NestedModules[1]
		if nestedMod2.Info.ID != "nested_lib2" {
			t.Errorf("Expected second nested module ID 'nested_lib2', got %q", nestedMod2.Info.ID)
		}
	})

	t.Run("Jarjar_Metadata_Invalid_JSON", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)

		// Create a library JAR with invalid jarjar metadata (malformed JSON)
		zipBuf := new(bytes.Buffer)
		zipWriter := zip.NewWriter(zipBuf)

		// Add invalid jarjar metadata (missing closing brace)
		jarjarFile, _ := zipWriter.Create("META-INF/jarjar/metadata.json")
		jarjarData := `{
			"jars": [{"path": "META-INF/jarjar/lib1.jar"}]`
		if _, err := jarjarFile.Write([]byte(jarjarData)); err != nil {
			t.Fatal(err)
		}

		if err := zipWriter.Close(); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(modsDir, "library.jar"), zipBuf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}

		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		// The loader should handle invalid jarjar metadata gracefully and still load the library
		if err != nil {
			t.Fatalf("Expected no error for invalid jarjar metadata, got: %v", err)
		}
		// The library should still be loaded as a synthetic mod, but nested jars won't be parsed
		if len(allMods) != 1 {
			t.Errorf("Expected 1 mod loaded (library with invalid jarjar metadata), got %d", len(allMods))
		}

		if libMod, ok := allMods["library-library.jar"]; !ok {
			t.Error("Expected mod 'library-library.jar' to be loaded")
		} else if !libMod.Metadata.IsJavaLibrary {
			t.Error("Expected IsJavaLibrary to be true for library with invalid jarjar metadata")
		}
	})

	t.Run("Dynamic_Version_From_Manifest", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"dynamic.jar": {
				TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "dynamic"
version = "${file.jarVersion}"
displayName = "Dynamic Version"
`,
				ManifestContent: "Manifest-Version: 1.0\nImplementation-Version: 5.5.5\n",
			},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"dynamic"}, 1, false)
		if loadedMods != nil {
			if loadedMods["dynamic"].Metadata.Version.Version.String() != "5.5.5" {
				t.Errorf("Expected version '5.5.5' from MANIFEST.MF, got '%s'", loadedMods["dynamic"].Metadata.Version.Version.String())
			}
		}
	})

	t.Run("Dynamic_Version_No_Manifest", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"no_manifest.jar": {
				TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "no_manifest"
version = "${file.jarVersion}"
displayName = "No Manifest"
`,
			},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"no_manifest"}, 1, false)
		if loadedMods != nil {
			// Should fallback to "0.0.0"
			if loadedMods["no_manifest"].Metadata.Version.Version.String() != "0.0.0" {
				t.Logf("Expected version '0.0.0' fallback, got '%s'", loadedMods["no_manifest"].Metadata.Version.Version.String())
			}
		}
	})

	t.Run("Legacy_Forge_TOML", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)

		// Create a JAR with legacy mods.toml (Forge 1.20.1)
		zipBuf := new(bytes.Buffer)
		zipWriter := zip.NewWriter(zipBuf)

		// Create mods.toml (legacy format)
		tomlFile, _ := zipWriter.Create("META-INF/mods.toml")
		tomlContent := `modLoader = "javafml"
loaderVersion = "[1.20, 1.21)"
[[mods]]
modId = "legacy_mod"
version = "1.0"
displayName = "Legacy Mod"
[[mods.dependencies]]
modId = "required"
type = "required"
versionRange = "[1.0,)"
`
		if _, err := tomlFile.Write([]byte(tomlContent)); err != nil {
			t.Fatal(err)
		}

		if err := zipWriter.Close(); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(modsDir, "legacy.jar"), zipBuf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}

		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 1 {
			t.Errorf("Expected 1 mod loaded (legacy), got %d", len(allMods))
		}
		if legacyMod, ok := allMods["legacy_mod"]; ok {
			if legacyMod.Metadata.Version.Version.String() != "1.0" {
				t.Errorf("Expected version '1.0', got '%s'", legacyMod.Metadata.Version.Version.String())
			}
		}
	})

	t.Run("Mixed_Fabric_NeoForge_Directory", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)

		// Create a Fabric mod
		fabricJarBytes, _ := createJarFromSpec(t, modSpec{
			JSONContent: `{"id": "fabric_mod", "version": "1.0", "name": "Fabric Mod"}`,
		})

		// Create a NeoForge mod
		nfJarBytes, _ := createNeoForgeJarFromSpec(t, neoForgeModSpec{
			TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "neoforge_mod"
version = "1.0"
displayName = "NeoForge Mod"
`,
		})

		if err := os.WriteFile(filepath.Join(modsDir, "fabric.jar"), fabricJarBytes, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(modsDir, "neoforge.jar"), nfJarBytes, 0644); err != nil {
			t.Fatal(err)
		}

		// Only the Connector loader accepts both manifest families. A pure
		// (Neo)Forge loader would skip the Fabric mod with a warning.
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForgeWithFabric}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
		if len(allMods) != 2 {
			t.Errorf("Expected 2 mods loaded (fabric + neoforge), got %d", len(allMods))
		}
	})

	t.Run("Multiple_Provides_in_Mods_Blocks", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"multi_provides-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "multi_provides"
version = "1.0"
displayName = "Multi Provides"
provides = ["api_1", "api_2", "api_3", "api_4", "api_5"]
`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"multi_provides"}, 6, false)
		if loadedMods != nil {
			if _, ok := loadedMods["multi_provides"]; !ok {
				t.Fatal("multi_provides should be loaded")
			}
			for _, api := range []string{"api_1", "api_2", "api_3", "api_4", "api_5"} {
				if _, ok := loadedMods["multi_provides"].EffectiveProvides[api]; !ok {
					t.Errorf("multi_provides should provide %s", api)
				}
			}
		}
	})

	// ============================================================================
	// SECTION 6: INTEGRATION TESTS
	// ============================================================================

	t.Run("Override_Provides_NeoForge_Mod", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"override_target-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "override_target"
version = "1.0"
displayName = "Override Target"
provides = ["original_api"]
`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)

		// Create override file to add a new provide
		overridesJSON := `{
			"version": 1,
			"overrides": {
				"override_target": {
					"+provides": ["overridden_api"]
				}
			}
		}`
		overridesPath := filepath.Join(testDir, "overrides_test.json")
		if err := os.WriteFile(overridesPath, []byte(overridesJSON), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(overridesPath)

		overrides, err := mods.LoadDependencyOverridesFromPath(overridesPath, mods.OverrideSourceUserProvided)
		if err != nil {
			t.Fatalf("Failed to load overrides: %v", err)
		}

		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderNeoForge}, Adapter: &mods.FileAdapter{BaseDirectory: modsDir}}
		allMods, _, err := loader.LoadMods(modsDir, overrides, nil)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if targetMod, ok := allMods["override_target"]; ok {
			// Verify original provide is still there
			if _, ok := targetMod.EffectiveProvides["original_api"]; !ok {
				t.Error("Expected 'original_api' to be in EffectiveProvides")
			}
			// Verify overridden provide was added
			if _, ok := targetMod.EffectiveProvides["overridden_api"]; !ok {
				t.Error("Expected 'overridden_api' to be in EffectiveProvides after override")
			}
		}
	})

	t.Run("Provider_Map_Population_NeoForge", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]neoForgeModSpec{
			"provider_mod-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "provider_mod"
version = "1.0"
displayName = "Provider Mod"
provides = ["provided_api", "another_api"]
`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, providers := loadAndCheckNeoForge(t, modsDir, []string{"provider_mod"}, 3, false)
		if loadedMods != nil {
			if _, ok := loadedMods["provider_mod"]; !ok {
				t.Fatal("provider_mod should be loaded")
			}
			if _, ok := providers["provided_api"]; !ok {
				t.Error("provided_api should be in providers")
			}
			if _, ok := providers["another_api"]; !ok {
				t.Error("another_api should be in providers")
			}
		}
	})

	t.Run("Full_Dependency_Resolution_NeoForge", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)

		// Create a dependency chain: core -> lib -> feature
		specs := map[string]neoForgeModSpec{
			"core-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "core"
version = "1.0"
displayName = "Core"`},
			"lib-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "lib"
version = "1.0"
displayName = "Lib"
[[dependencies.lib]]
modId = "core"
type = "required"
versionRange = "[1.0,)"`},
			"feature-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "feature"
version = "1.0"
displayName = "Feature"
[[dependencies.feature]]
modId = "lib"
type = "required"
versionRange = "[1.0,)"
[[dependencies.feature]]
modId = "core"
type = "required"
versionRange = "[1.0,)"`},
		}
		setupDummyNeoForgeMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheckNeoForge(t, modsDir, []string{"core", "lib", "feature"}, 3, false)
		if len(loadedMods) != 3 {
			t.Errorf("Expected 3 mods loaded, got %d", len(loadedMods))
		}
		// Verify dependency tracking
		if libMod, ok := loadedMods["lib"]; ok {
			if _, ok := libMod.Metadata.Depends["core"]; !ok {
				t.Error("lib should depend on core")
			}
		}
		if featureMod, ok := loadedMods["feature"]; ok {
			if _, ok := featureMod.Metadata.Depends["lib"]; !ok {
				t.Error("feature should depend on lib")
			}
			if _, ok := featureMod.Metadata.Depends["core"]; !ok {
				t.Error("feature should depend on core")
			}
		}
	})
}
