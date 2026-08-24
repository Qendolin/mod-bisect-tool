package app_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

func TestModLoader(t *testing.T) {
	setupLogger(t) // Ensure logging is set up for these tests

	// Helper to create a unique test directory for each test run
	newTestDir := func(name string) string {
		dir := filepath.Join(testDir, "loader_"+strings.ReplaceAll(name, " ", "_"))
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			t.Fatalf("Failed to clean test directory %s: %v", dir, err)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create test directory %s: %v", dir, err)
		}
		return dir
	}

	implicitProviderCount := len(mods.GetImplicitMods())

	// Helper to load mods and perform common assertions
	loadAndCheck := func(t *testing.T, modsDir string, expectedModIDs []string, expectedProviderCount int, expectError bool) (map[string]*mods.Mod, mods.PotentialProvidersMap) {
		adapter := mods.FileAdapter{BaseDirectory: modsDir}
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderFabric}, Adapter: &adapter}
		allMods, providers, err := loader.LoadMods(modsDir, nil, nil)

		if expectError {
			if err == nil {
				t.Fatalf("Expected LoadMods to return an error, but got none")
			}
			return nil, nil // Error expected, so return nil for mods and providers
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
			if len(providers) != expectedProviderCount+implicitProviderCount {
				t.Errorf("Expected %d potential providers, got %d", expectedProviderCount+implicitProviderCount, len(providers))
			}
			return allMods, providers
		}
	}

	t.Run("Basic_Mod_Loading", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"mymod-1.0.jar": {JSONContent: `{"id": "mymod", "version": "1.0"}`},
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{"mymod"}, 1, false)
	})

	t.Run("Multiple_Independent_Mods", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"mod_a-1.0.jar": {JSONContent: `{"id": "mod_a", "version": "1.0"}`},
			"mod_b-1.0.jar": {JSONContent: `{"id": "mod_b", "version": "1.0"}`},
		}
		setupDummyMods(t, modsDir, specs)
		loadedMods, providers := loadAndCheck(t, modsDir, []string{"mod_a", "mod_b"}, 2, false)
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

	t.Run("Mod_with_Basic_Dependency", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"mod_dep-1.0.jar": {JSONContent: `{"id": "mod_dep", "version": "1.0"}`},
			"mod_req-1.0.jar": {JSONContent: `{"id": "mod_req", "version": "1.0", "depends": {"mod_dep": ">=1.0"}}`},
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{"mod_dep", "mod_req"}, 2, false)
	})

	t.Run("Mod_with_Provides", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"lib_provider-1.0.jar": {JSONContent: `{"id": "lib_provider", "version": "1.0", "provides": ["my_lib"]}`},
		}
		setupDummyMods(t, modsDir, specs)
		loadedMods, providers := loadAndCheck(t, modsDir, []string{"lib_provider"}, 2, false) // lib_provider + my_lib
		if loadedMods != nil {
			if _, ok := loadedMods["lib_provider"]; !ok {
				t.Fatal("lib_provider should be loaded")
			}
			if _, ok := loadedMods["lib_provider"].EffectiveProvides["my_lib"]; !ok {
				t.Error("lib_provider should effectively provide my_lib")
			}
			if _, ok := providers["my_lib"]; !ok {
				t.Error("my_lib should be a potential provider")
			}
		}
	})

	t.Run("Mod_with_Nested_JAR", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"main_mod-1.0.jar": {
				JSONContent: `{"id": "main_mod", "version": "1.0", "jars": [{"file": "libs/nested.jar"}]}`,
				NestedJars: map[string]modSpec{
					"libs/nested.jar": {JSONContent: `{"id": "nested_lib", "version": "1.0"}`},
				},
			},
		}
		setupDummyMods(t, modsDir, specs)
		loadedMods, providers := loadAndCheck(t, modsDir, []string{"main_mod"}, 2, false)
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

	t.Run("Multiple_Mods_Same_ID_Winner", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"conf_mod-1.0.jar": {JSONContent: `{"id": "conf_mod", "version": "1.0"}`},
			"conf_mod-2.0.jar": {JSONContent: `{"id": "conf_mod", "version": "2.0"}`}, // Higher version should win
		}
		setupDummyMods(t, modsDir, specs)
		loadedMods, _ := loadAndCheck(t, modsDir, []string{"conf_mod"}, 1, false)
		if loadedMods != nil {
			if loadedMods["conf_mod"].Metadata.Version.Version.String() != "2.0" {
				t.Errorf("Expected conf_mod v2.0 to win conflict, got v%s", loadedMods["conf_mod"].Metadata.Version.Version.String())
			}
			// The losing active file must have been disabled on disk.
			disabledPath := filepath.Join(modsDir, "conf_mod-1.0.jar.disabled")
			if _, err := os.Stat(disabledPath); os.IsNotExist(err) {
				t.Error("conf_mod-1.0.jar should have been disabled, but the .disabled file was not found.")
			}
			if _, err := os.Stat(filepath.Join(modsDir, "conf_mod-1.0.jar")); !os.IsNotExist(err) {
				t.Error("conf_mod-1.0.jar should no longer exist as an active file.")
			}
		}
	})

	t.Run("Multiple_Mods_Same_ID_Disabled", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			// Loser: active, older version
			"conf_mod-1.0.jar": {JSONContent: `{"id": "conf_mod", "version": "1.0"}`},
			// Winner: disabled, newer version
			"conf_mod-2.0.jar.disabled": {JSONContent: `{"id": "conf_mod", "version": "2.0"}`},
		}
		setupDummyMods(t, modsDir, specs)

		// The loader should only load the winner, v2.0.
		loadedMods, _ := loadAndCheck(t, modsDir, []string{"conf_mod"}, 1, false)

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

	t.Run("Same_Stem_Active_And_Disabled", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		// A disabled twin sharing the same base filename as an active file.
		// The loader must relocate the disabled twin (foo-dup) so the pair is
		// treated as ordinary duplicates instead of clobbering each other.
		specs := map[string]modSpec{
			"conf_mod.jar":          {JSONContent: `{"id": "conf_mod", "version": "1.0"}`},
			"conf_mod.jar.disabled": {JSONContent: `{"id": "conf_mod", "version": "2.0"}`},
		}
		setupDummyMods(t, modsDir, specs)

		loadedMods, _ := loadAndCheck(t, modsDir, []string{"conf_mod"}, 1, false)
		if loadedMods != nil {
			if loadedMods["conf_mod"].Metadata.Version.Version.String() != "2.0" {
				t.Errorf("Expected conf_mod v2.0 to win conflict, got v%s", loadedMods["conf_mod"].Metadata.Version.Version.String())
			}
			// The disabled twin must have been relocated to a distinct -dup stem.
			if _, err := os.Stat(filepath.Join(modsDir, "conf_mod-dup.jar.disabled")); os.IsNotExist(err) {
				t.Error("conf_mod.jar.disabled should have been relocated to conf_mod-dup.jar.disabled.")
			}
			// The active loser must have been disabled (no active file remains).
			if _, err := os.Stat(filepath.Join(modsDir, "conf_mod.jar")); !os.IsNotExist(err) {
				t.Error("conf_mod.jar (active loser) should have been disabled.")
			}
		}
	})

	t.Run("Mod_with_Fabric_Json_Priority", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"quilt_mod-1.0.jar": {
				JSONContent: `{"id": "fabric_mod", "version": "1.0"}`, // fabric.mod.json
				RawFiles: map[string]string{ // Use the new RawFiles field.
					"quilt.mod.json": `{"id": "quilt_mod", "version": "1.1"}`,
				},
			},
		}
		setupDummyMods(t, modsDir, specs)
		adapter := mods.FileAdapter{BaseDirectory: modsDir}
		loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: mods.RunLoaderFabric}, Adapter: &adapter}
		allMods, _, err := loader.LoadMods(modsDir, nil, nil)
		if err != nil {
			t.Fatalf("LoadMods returned an unexpected error: %v", err)
		}
		if len(allMods) != 1 {
			t.Fatalf("Expected 1 mod to be loaded, got %d", len(allMods))
		}
		loadedMod, ok := allMods["fabric_mod"]
		if !ok {
			t.Fatal("Expected mod with ID 'fabric_mod' to be loaded, but it was not found.")
		}
		if loadedMod.Metadata.Version.Version.String() != "1.0" {
			t.Errorf("Expected fabric.mod.json (v1.0) to take priority, got v%s", loadedMod.Metadata.Version.Version.String())
		}
	})

	t.Run("Mod_with_Breaks_Loads_Fine", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"broken_mod-1.0.jar": {JSONContent: `{"id": "broken_mod", "version": "1.0", "breaks": {"another_mod": "1.0"}}`},
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{"broken_mod"}, 1, false) // Breaks handled at resolution, not load time.
	})

	t.Run("Mod_with_Complex_Version_Range", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"range_mod-1.0.jar": {JSONContent: `{"id": "range_mod", "version": "1.0", "depends": {"my_lib": ["^1.0", "<2.0-beta.1"]}}`},
			"my_lib-1.0.jar":    {JSONContent: `{"id": "my_lib", "version": "1.0"}`},
		}
		setupDummyMods(t, modsDir, specs)
		// Should load fine, parsing of range handled by VersionRanges.UnmarshalJSON
		loadAndCheck(t, modsDir, []string{"range_mod", "my_lib"}, 2, false)
	})

	// --- Negative Cases ---

	t.Run("Empty_Mods_Directory", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		if err := os.MkdirAll(modsDir, 0755); err != nil {
			t.Fatalf("Failed to create empty mods directory: %v", err)
		}
		defer os.RemoveAll(modsDir)
		loadAndCheck(t, modsDir, []string{}, 0, false)
	})

	t.Run("Missing_Mod_Json", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		zipBuf := new(bytes.Buffer)
		zipWriter := zip.NewWriter(zipBuf)
		// No fabric.mod.json
		if err := zipWriter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(modsDir, "empty.jar"), zipBuf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
		loadAndCheck(t, modsDir, []string{}, 0, false) // Mod should be skipped with a warning, LoadMods should not error.
	})

	t.Run("Invalid_Mod_Json_Malformed", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"malformed.jar": {JSONContent: `{"id": "malformed", "version": "1.0",`}, // Trailing comma, invalid JSON
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{}, 0, false) // Should warn and skip, not error LoadMods.
	})

	t.Run("Invalid_Mod_Json_Missing_ID", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"no_id.jar": {JSONContent: `{"version": "1.0"}`}, // Missing ID
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{}, 0, false) // Should warn and skip.
	})

	t.Run("Invalid_Mod_Json_Missing_Version", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"no_version.jar": {JSONContent: `{"id": "no_version"}`}, // Missing version
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{}, 0, false) // Should warn and skip due to VersionField.UnmarshalJSON.
	})

	t.Run("Invalid_Mod_Json_Invalid_Version_String", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"bad_version.jar": {JSONContent: `{"id": "bad_version", "version": "a.b.c"}`},
		}
		setupDummyMods(t, modsDir, specs)

		// The loader correctly loads this mod with a StringVersion.
		// We should expect 1 mod to be loaded.
		loadedMods, _ := loadAndCheck(t, modsDir, []string{"bad_version"}, 1, false)

		if loadedMods != nil {
			mod := loadedMods["bad_version"]
			if mod.Metadata.Version.Version.IsSemantic() {
				t.Error("Expected version for 'bad_version' to be a non-semantic StringVersion")
			}
			if mod.Metadata.Version.Version.String() != "a.b.c" {
				t.Errorf("Expected version string 'a.b.c', got '%s'", mod.Metadata.Version.Version.String())
			}
		}
	})

	t.Run("Invalid_Mod_Json_Invalid_Dep_Predicate", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		// The value for a dependency must be a string or array of strings.
		// A number is a structural error that MUST cause a parse failure.
		specs := map[string]modSpec{
			"bad_dep.jar": {JSONContent: `{"id": "bad_dep", "version": "1.0", "depends": {"lib": 123}}`},
		}
		setupDummyMods(t, modsDir, specs)
		// The loader should correctly fail to parse this mod and skip it.
		loadAndCheck(t, modsDir, []string{}, 0, false)
	})

	t.Run("Illegal_Mod_ID_Reserved", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"java_mod-1.0.jar": {JSONContent: `{"id": "java", "version": "1.0"}`}, // Illegal ID
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{}, 0, false) // Should warn and skip.
	})

	t.Run("Illegal_Provides_ID_Reserved", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"provides_mc-1.0.jar": {JSONContent: `{"id": "provides_mc", "version": "1.0", "provides": ["minecraft"]}`}, // Illegal provides ID
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{}, 0, false) // Should warn and skip.
	})

	t.Run("Nested_JAR_Not_Found", func(t *testing.T) {
		logging.Infof("Test: Running test case %q", t.Name())
		modsDir := newTestDir(t.Name())
		defer os.RemoveAll(modsDir)
		specs := map[string]modSpec{
			"main_mod_missing_nested-1.0.jar": {
				JSONContent: `{"id": "main_mod", "version": "1.0", "jars": [{"file": "libs/non_existent.jar"}]}`,
			},
		}
		setupDummyMods(t, modsDir, specs)
		loadAndCheck(t, modsDir, []string{"main_mod"}, 1, false) // main_mod should load, but warning about missing nested.
	})
}
