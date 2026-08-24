package app_test

import (
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

const (
	kiltFabricJSON = `{"id": "kilt_fabric", "version": "1.0", "name": "Kilt Fabric"}`
	kiltNFTOML     = `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "kilt_nf"
version = "1.0"
displayName = "Kilt NF"`
)

func loadWith(t *testing.T, modsDir string, loader mods.RunLoader) (map[string]*mods.Mod, mods.PotentialProvidersMap, error) {
	t.Helper()
	adapter := mods.FileAdapter{BaseDirectory: modsDir}
	ml := mods.ModLoader{ModParser: mods.ModParser{RunLoader: loader}, Adapter: &adapter}
	allMods, providers, err := ml.LoadMods(modsDir, nil, nil)
	return allMods, providers, err
}

func assertLoaded(t *testing.T, allMods map[string]*mods.Mod, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, ok := allMods[id]; !ok {
			t.Errorf("expected mod %q to be loaded, got %v", id, loadedModIDs(allMods))
		}
	}
}

func assertNotLoaded(t *testing.T, allMods map[string]*mods.Mod, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, ok := allMods[id]; ok {
			t.Errorf("mod %q should not be loaded, got %v", id, loadedModIDs(allMods))
		}
	}
}

// ============================================================================
// KILT (Fabric with (Neo)Forge via Kilt)
// ============================================================================

// TestKiltPrefersFabricManifest verifies that when a single jar declares both a
// Fabric and a (Neo)Forge manifest, Kilt loads it from its Fabric manifest.
func TestKiltPrefersFabricManifest(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"dual-1.0.jar": {
			TOMLContent: kiltNFTOML,
			RawFiles: map[string]string{
				"fabric.mod.json": kiltFabricJSON,
			},
		},
	})

	allMods, _, err := loadWith(t, modsDir, mods.RunLoaderFabricWithNeoForge)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "kilt_fabric")
	assertNotLoaded(t, allMods, "kilt_nf")
}

// TestKiltLoadsQuiltManifest verifies that Kilt loads Quilt manifests.
func TestKiltLoadsQuiltManifest(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyMods(t, modsDir, map[string]modSpec{
		"quilt_only-1.0.jar": {RawFiles: map[string]string{
			"quilt.mod.json": `{"id": "kilt_quilt", "version": "1.0"}`,
		}},
	})

	allMods, _, err := loadWith(t, modsDir, mods.RunLoaderFabricWithNeoForge)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "kilt_quilt")
}

// TestKiltLoadsFabricNestedJars verifies that a Fabric mod's declared nested
// jars are loaded as nested modules under Kilt.
func TestKiltLoadsFabricNestedJars(t *testing.T) {
	modsDir := t.TempDir()
	spec := modSpec{
		JSONContent: `{"id": "kilt_fabric", "version": "1.0", "jars": [{"file": "nested.jar"}]}`,
		NestedJars: map[string]modSpec{
			"nested.jar": {JSONContent: `{"id": "kilt_nested", "version": "1.0"}`},
		},
	}
	setupDummyMods(t, modsDir, map[string]modSpec{"kilt_fabric-1.0.jar": spec})

	allMods, providers, err := loadWith(t, modsDir, mods.RunLoaderFabricWithNeoForge)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	fabricMod, ok := allMods["kilt_fabric"]
	if !ok {
		t.Fatalf("expected kilt_fabric to be loaded, got %v", loadedModIDs(allMods))
	}
	if len(fabricMod.NestedModules) != 1 || fabricMod.NestedModules[0].Info.ID != "kilt_nested" {
		t.Errorf("expected nested module kilt_nested, got %+v", fabricMod.NestedModules)
	}
	if _, ok := providers["kilt_nested"]; !ok {
		t.Error("kilt_nested should be a potential provider")
	}
}

// TestKiltLoadsNeoForgeNestedJars verifies that (Neo)Forge jarjar nested jars
// are loaded as nested modules under Kilt.
func TestKiltLoadsNeoForgeNestedJars(t *testing.T) {
	modsDir := t.TempDir()
	spec := neoForgeModSpec{
		TOMLContent: kiltNFTOML,
		NestedJars: map[string]neoForgeModSpec{
			"nested.jar": {
				TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "kilt_nf_nested"
version = "1.0"
displayName = "Kilt NF Nested"`,
			},
		},
	}
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{"kilt_nf-1.0.jar": spec})

	allMods, providers, err := loadWith(t, modsDir, mods.RunLoaderFabricWithNeoForge)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	nfMod, ok := allMods["kilt_nf"]
	if !ok {
		t.Fatalf("expected kilt_nf to be loaded, got %v", loadedModIDs(allMods))
	}
	if len(nfMod.NestedModules) != 1 || nfMod.NestedModules[0].Info.ID != "kilt_nf_nested" {
		t.Errorf("expected nested module kilt_nf_nested, got %+v", nfMod.NestedModules)
	}
	if _, ok := providers["kilt_nf_nested"]; !ok {
		t.Error("kilt_nf_nested should be a potential provider")
	}
}

// TestKiltFabricDepSatisfiedByNeoForgeProvides verifies that a Fabric mod can
// depend on an API provided by a (Neo)Forge mod under Kilt.
func TestKiltFabricDepSatisfiedByNeoForgeProvides(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"kilt_api-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "kilt_api"
version = "1.0"
displayName = "Kilt API"
provides = ["kilt_shared_api"]`,
		},
	})
	setupDummyMods(t, modsDir, map[string]modSpec{
		"kilt_client-1.0.jar": {JSONContent: `{"id": "kilt_client", "version": "1.0", "depends": {"kilt_shared_api": "*"}}`},
	})

	allMods, providers, err := loadWith(t, modsDir, mods.RunLoaderFabricWithNeoForge)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "kilt_client", "kilt_api")
	if _, ok := providers["kilt_shared_api"]; !ok {
		t.Error("kilt_shared_api should be a potential provider")
	}
	if _, ok := allMods["kilt_api"].EffectiveProvides["kilt_shared_api"]; !ok {
		t.Error("kilt_api should effectively provide kilt_shared_api")
	}
}

// ============================================================================
// SINYTRA CONNECTOR ((Neo)Forge with Fabric via Connector)
// ============================================================================

const (
	connectorFabricJSON = `{"id": "conn_fabric", "version": "1.0", "name": "Conn Fabric"}`
	connectorNFTOML     = `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "conn_nf"
version = "1.0"
displayName = "Conn NF"`
)

// TestConnectorPrefersNeoForgeManifest verifies that when a single jar declares
// both a (Neo)Forge and a Fabric manifest, Connector loads it from its
// (Neo)Forge manifest.
func TestConnectorPrefersNeoForgeManifest(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"dual-1.0.jar": {
			TOMLContent: connectorNFTOML,
			RawFiles: map[string]string{
				"fabric.mod.json": connectorFabricJSON,
			},
		},
	})

	allMods, _, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "conn_nf")
	assertNotLoaded(t, allMods, "conn_fabric")
}

// TestConnectorLoadsQuiltManifest verifies that Connector loads Quilt manifests.
func TestConnectorLoadsQuiltManifest(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyMods(t, modsDir, map[string]modSpec{
		"quilt_only-1.0.jar": {RawFiles: map[string]string{
			"quilt.mod.json": `{"id": "conn_quilt", "version": "1.0"}`,
		}},
	})

	allMods, _, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "conn_quilt")
}

// TestConnectorLoadsFabricNestedJars verifies that a Fabric mod's declared
// nested jars are loaded as nested modules under Connector.
func TestConnectorLoadsFabricNestedJars(t *testing.T) {
	modsDir := t.TempDir()
	spec := modSpec{
		JSONContent: `{"id": "conn_fabric", "version": "1.0", "jars": [{"file": "nested.jar"}]}`,
		NestedJars: map[string]modSpec{
			"nested.jar": {JSONContent: `{"id": "conn_nested", "version": "1.0"}`},
		},
	}
	setupDummyMods(t, modsDir, map[string]modSpec{"conn_fabric-1.0.jar": spec})

	allMods, providers, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	fabricMod, ok := allMods["conn_fabric"]
	if !ok {
		t.Fatalf("expected conn_fabric to be loaded, got %v", loadedModIDs(allMods))
	}
	if len(fabricMod.NestedModules) != 1 || fabricMod.NestedModules[0].Info.ID != "conn_nested" {
		t.Errorf("expected nested module conn_nested, got %+v", fabricMod.NestedModules)
	}
	if _, ok := providers["conn_nested"]; !ok {
		t.Error("conn_nested should be a potential provider")
	}
}

// TestConnectorLoadsNeoForgeNestedJars verifies that (Neo)Forge jarjar nested
// jars are loaded as nested modules under Connector.
func TestConnectorLoadsNeoForgeNestedJars(t *testing.T) {
	modsDir := t.TempDir()
	spec := neoForgeModSpec{
		TOMLContent: connectorNFTOML,
		NestedJars: map[string]neoForgeModSpec{
			"nested.jar": {
				TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "conn_nf_nested"
version = "1.0"
displayName = "Conn NF Nested"`,
			},
		},
	}
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{"conn_nf-1.0.jar": spec})

	allMods, providers, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	nfMod, ok := allMods["conn_nf"]
	if !ok {
		t.Fatalf("expected conn_nf to be loaded, got %v", loadedModIDs(allMods))
	}
	if len(nfMod.NestedModules) != 1 || nfMod.NestedModules[0].Info.ID != "conn_nf_nested" {
		t.Errorf("expected nested module conn_nf_nested, got %+v", nfMod.NestedModules)
	}
	if _, ok := providers["conn_nf_nested"]; !ok {
		t.Error("conn_nf_nested should be a potential provider")
	}
}

// TestConnectorFabricDepSatisfiedByNeoForgeProvides verifies that a Fabric mod
// can depend on an API provided by a (Neo)Forge mod under Connector.
func TestConnectorFabricDepSatisfiedByNeoForgeProvides(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"conn_api-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "conn_api"
version = "1.0"
displayName = "Conn API"
provides = ["conn_shared_api"]`,
		},
	})
	setupDummyMods(t, modsDir, map[string]modSpec{
		"conn_client-1.0.jar": {JSONContent: `{"id": "conn_client", "version": "1.0", "depends": {"conn_shared_api": "*"}}`},
	})

	allMods, providers, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "conn_client", "conn_api")
	if _, ok := providers["conn_shared_api"]; !ok {
		t.Error("conn_shared_api should be a potential provider")
	}
	if _, ok := allMods["conn_api"].EffectiveProvides["conn_shared_api"]; !ok {
		t.Error("conn_api should effectively provide conn_shared_api")
	}
}

// TestConnectorLoadsModPropertiesEndToEnd verifies that a Connector jar's
// [modproperties] fabric:provides show up as potential providers, satisfying a
// Fabric client mod that depends on them.
func TestConnectorLoadsModPropertiesEndToEnd(t *testing.T) {
	modsDir := t.TempDir()

	connectorToml := `[[mods]]
modId = "connector"
version = "1.0"
displayName = "Connector"

[[mods]]
modId = "fabric_api_base"
version = "1.0"
displayName = "Fabric API Base"

[modproperties.fabric_api_base]
"fabric:provides" = ["fabric-api-base"]
`
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"connector-1.0.jar": {TOMLContent: connectorToml},
	})
	setupDummyMods(t, modsDir, map[string]modSpec{
		"client_mod-1.0.jar": {JSONContent: `{"id": "client_mod", "version": "1.0", "depends": {"fabric-api-base": "*"}}`},
	})

	allMods, providers, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}

	connectorMod, ok := allMods["connector"]
	if !ok {
		t.Fatalf("expected connector mod to be loaded, got %v", loadedModIDs(allMods))
	}
	if _, ok := connectorMod.EffectiveProvides["fabric-api-base"]; !ok {
		t.Error("connector should effectively provide fabric-api-base")
	}
	if _, ok := providers["fabric-api-base"]; !ok {
		t.Error("fabric-api-base should be a potential provider")
	}
	if _, ok := allMods["client_mod"]; !ok {
		t.Error("client_mod should be loaded")
	}
}

// TestConnectorLoadsPlaceholderAsFabric verifies that a Connector placeholder
// jar is loaded under its real Fabric mod ID via LoadMods.
func TestConnectorLoadsPlaceholderAsFabric(t *testing.T) {
	modsDir := t.TempDir()

	placeholderToml := `[properties]
"connector:placeholder" = true
`
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"placeholder-1.0.jar": {
			TOMLContent: placeholderToml,
			RawFiles: map[string]string{
				"fabric.mod.json": `{"id": "real_mod", "version": "1.0", "name": "Real Mod"}`,
			},
		},
	})

	allMods, _, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}

	if _, ok := allMods["real_mod"]; !ok {
		t.Fatalf("expected placeholder jar to load as its Fabric mod real_mod, got %v", loadedModIDs(allMods))
	}
}

// ============================================================================
// CROSS-MANIFEST PRIORITY
// ============================================================================

const (
	kiltQuiltJSON  = `{"id": "kilt_quilt", "version": "1.0"}`
	connQuiltJSON  = `{"id": "conn_quilt", "version": "1.0"}`
	legacyKiltTOML = `modLoader = "javafml"
loaderVersion = "[1.20, 1.21)"
[[mods]]
modId = "legacy_kilt"
version = "1.0"
displayName = "Legacy Kilt"`
	legacyConnTOML = `modLoader = "javafml"
loaderVersion = "[1.20, 1.21)"
[[mods]]
modId = "legacy_conn"
version = "1.0"
displayName = "Legacy Conn"`
)

// TestKiltPrefersQuiltOverNeoForge verifies that within the Fabric family, Kilt
// prefers a Quilt manifest over a (Neo)Forge manifest in the same jar.
func TestKiltPrefersQuiltOverNeoForge(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"dual-1.0.jar": {
			TOMLContent: kiltNFTOML,
			RawFiles:    map[string]string{"quilt.mod.json": kiltQuiltJSON},
		},
	})

	allMods, _, err := loadWith(t, modsDir, mods.RunLoaderFabricWithNeoForge)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "kilt_quilt")
	assertNotLoaded(t, allMods, "kilt_nf")
}

// TestConnectorPrefersNeoForgeOverQuilt verifies that Connector prefers the
// (Neo)Forge manifest over a Quilt manifest in the same jar.
func TestConnectorPrefersNeoForgeOverQuilt(t *testing.T) {
	modsDir := t.TempDir()
	setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
		"dual-1.0.jar": {
			TOMLContent: connectorNFTOML,
			RawFiles:    map[string]string{"quilt.mod.json": connQuiltJSON},
		},
	})

	allMods, _, err := loadWith(t, modsDir, mods.RunLoaderNeoForgeWithFabric)
	if err != nil {
		t.Fatalf("LoadMods failed: %v", err)
	}
	assertLoaded(t, allMods, "conn_nf")
	assertNotLoaded(t, allMods, "conn_quilt")
}

// TestPrefersFabricOverQuilt verifies that both loaders prefer fabric.mod.json
// over quilt.mod.json when a jar declares both.
func TestPrefersFabricOverQuilt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		loader   mods.RunLoader
		fabricID string
		quiltID  string
	}{
		{"Kilt", mods.RunLoaderFabricWithNeoForge, "kilt_fabric", "kilt_quilt"},
		{"Connector", mods.RunLoaderNeoForgeWithFabric, "conn_fabric", "conn_quilt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			setupDummyMods(t, modsDir, map[string]modSpec{
				"dual-1.0.jar": {
					JSONContent: `{"id": "` + tc.fabricID + `", "version": "1.0"}`,
					RawFiles:    map[string]string{"quilt.mod.json": `{"id": "` + tc.quiltID + `", "version": "1.0"}`},
				},
			})

			allMods, _, err := loadWith(t, modsDir, tc.loader)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}
			assertLoaded(t, allMods, tc.fabricID)
			assertNotLoaded(t, allMods, tc.quiltID)
		})
	}
}

// TestLoadsLegacyForgeManifest verifies that both loaders accept a legacy Forge
// mods.toml (1.20.1 era) alongside the modern neoforge.mods.toml.
func TestLoadsLegacyForgeManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		loader   mods.RunLoader
		expected string
		toml     string
	}{
		{"Kilt", mods.RunLoaderFabricWithNeoForge, "legacy_kilt", legacyKiltTOML},
		{"Connector", mods.RunLoaderNeoForgeWithFabric, "legacy_conn", legacyConnTOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
				"legacy-1.0.jar": {RawFiles: map[string]string{"META-INF/mods.toml": tc.toml}},
			})

			allMods, _, err := loadWith(t, modsDir, tc.loader)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}
			assertLoaded(t, allMods, tc.expected)
		})
	}
}

// ============================================================================
// LIBRARIES AND CONTAINERS
// ============================================================================

// TestLoadsLibraryJar verifies that a manifest-less jar is accepted as a
// (Neo)Forge library under both loaders.
func TestLoadsLibraryJar(t *testing.T) {
	for _, tc := range []struct {
		name   string
		loader mods.RunLoader
	}{
		{"Kilt", mods.RunLoaderFabricWithNeoForge},
		{"Connector", mods.RunLoaderNeoForgeWithFabric},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
				"empty.jar": {ManifestContent: "Manifest-Version: 1.0\n"},
			})

			allMods, _, err := loadWith(t, modsDir, tc.loader)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}
			libMod, ok := allMods["library-empty.jar"]
			if !ok {
				t.Fatalf("expected manifest-less jar to be loaded as a library, got %v", loadedModIDs(allMods))
			}
			if !libMod.Metadata.IsJavaLibrary {
				t.Error("expected IsJavaLibrary to be true")
			}
		})
	}
}

// TestLoadsJarjarContainerLibrary verifies that a manifest-less jar bundling
// jarjar nested mods is loaded as a container library with nested modules.
func TestLoadsJarjarContainerLibrary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		loader   mods.RunLoader
		nestedID string
	}{
		{"Kilt", mods.RunLoaderFabricWithNeoForge, "kilt_container_nested"},
		{"Connector", mods.RunLoaderNeoForgeWithFabric, "conn_container_nested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			spec := neoForgeModSpec{
				NestedJars: map[string]neoForgeModSpec{
					"nested.jar": {
						TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "` + tc.nestedID + `"
version = "1.0"
displayName = "Container Nested"`,
					},
				},
			}
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{"container.jar": spec})

			allMods, providers, err := loadWith(t, modsDir, tc.loader)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}
			containerMod, ok := allMods["library-container.jar"]
			if !ok {
				t.Fatalf("expected manifest-less jarjar container to be loaded as a library, got %v", loadedModIDs(allMods))
			}
			if !containerMod.Metadata.IsJavaLibrary {
				t.Error("expected IsJavaLibrary to be true for a container")
			}
			if len(containerMod.NestedModules) != 1 || containerMod.NestedModules[0].Info.ID != tc.nestedID {
				t.Errorf("expected nested module %s, got %+v", tc.nestedID, containerMod.NestedModules)
			}
			if _, ok := providers[tc.nestedID]; !ok {
				t.Error(tc.nestedID + " should be a potential provider")
			}
		})
	}
}

// ============================================================================
// CROSS-LOADER DEPENDENCIES
// ============================================================================

// TestNeoForgeDepOnFabricMod verifies that a (Neo)Forge mod can require a mod
// provided by a Fabric mod under both loaders.
func TestNeoForgeDepOnFabricMod(t *testing.T) {
	for _, tc := range []struct {
		name     string
		loader   mods.RunLoader
		nfID     string
		fabricID string
	}{
		{"Kilt", mods.RunLoaderFabricWithNeoForge, "kilt_nf_source", "kilt_fab_target"},
		{"Connector", mods.RunLoaderNeoForgeWithFabric, "conn_nf_source", "conn_fab_target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			setupDummyMods(t, modsDir, map[string]modSpec{
				"target-1.0.jar": {JSONContent: `{"id": "` + tc.fabricID + `", "version": "1.0"}`},
			})
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
				"source-1.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "` + tc.nfID + `"
version = "1.0"
displayName = "NF Source"
[[dependencies.` + tc.nfID + `]]
modId = "` + tc.fabricID + `"
type = "required"
versionRange = "[1.0,)"`},
			})

			allMods, providers, err := loadWith(t, modsDir, tc.loader)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}
			assertLoaded(t, allMods, tc.nfID, tc.fabricID)
			if _, ok := providers[tc.fabricID]; !ok {
				t.Error(tc.fabricID + " should be a potential provider")
			}
			if deps, ok := allMods[tc.nfID].Metadata.Depends[tc.fabricID]; !ok || len(deps) == 0 {
				t.Errorf("%s should depend on %s", tc.nfID, tc.fabricID)
			}
		})
	}
}

// ============================================================================
// CONFLICT RESOLUTION ACROSS MANIFEST FAMILIES
// ============================================================================

// TestConflictingIDAcrossFamilies verifies that when the same mod ID is provided
// by both a Fabric and a (Neo)Forge jar, the higher version wins under both
// loaders.
func TestConflictingIDAcrossFamilies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		loader mods.RunLoader
	}{
		{"Kilt", mods.RunLoaderFabricWithNeoForge},
		{"Connector", mods.RunLoaderNeoForgeWithFabric},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			setupDummyMods(t, modsDir, map[string]modSpec{
				"shared-1.0.jar": {JSONContent: `{"id": "shared_mod", "version": "1.0"}`},
			})
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
				"shared-2.0.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "shared_mod"
version = "2.0"
displayName = "Shared Mod"`},
			})

			allMods, _, err := loadWith(t, modsDir, tc.loader)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}
			winner, ok := allMods["shared_mod"]
			if !ok {
				t.Fatalf("expected shared_mod to be loaded, got %v", loadedModIDs(allMods))
			}
			if winner.Metadata.Version.Version.String() != "2.0" {
				t.Errorf("expected the higher version (2.0) to win the conflict, got %s", winner.Metadata.Version.Version.String())
			}
			if winner.Metadata.Loader != mods.ManifestLoaderNeoForge {
				t.Errorf("expected the winner to be the (Neo)Forge mod, got %q", winner.Metadata.Loader)
			}
		})
	}
}

// TestModIDDashUnderscoreNormalizationInCrossLoaders verifies that when cross-loading is enabled,
// mod IDs with '-' or '_' are normalized (stripped of '-' and '_') for dependency resolution in potentialProviders,
// while actual parsed mod IDs remain unchanged.
func TestModIDDashUnderscoreNormalizationInCrossLoaders(t *testing.T) {
	for _, tc := range []struct {
		name   string
		loader mods.RunLoader
	}{
		{"Kilt", mods.RunLoaderFabricWithNeoForge},
		{"Connector", mods.RunLoaderNeoForgeWithFabric},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			setupDummyMods(t, modsDir, map[string]modSpec{
				"mod_a.jar": {JSONContent: `{"id": "my_mod_a", "version": "1.0", "depends": {"other-mod-b": ">=1.0"}}`},
			})
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
				"mod_b.jar": {TOMLContent: `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "othermodb"
version = "1.0"
displayName = "Other Mod B"`},
			})

			allMods, providers, err := loadWith(t, modsDir, tc.loader)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}
			assertLoaded(t, allMods, "my_mod_a", "othermodb")

			resolver := mods.NewDependencyResolver(allMods, providers, tc.loader)
			stateMgr := mods.NewStateManager(allMods, resolver)
			res := stateMgr.ResolveEffectiveSet(sets.MakeSet([]string{"my_mod_a"}))

			if _, ok := res.EffectiveSet["my_mod_a"]; !ok {
				t.Errorf("expected my_mod_a in effective set, got %v", res.EffectiveSet)
			}
			if _, ok := res.EffectiveSet["othermodb"]; !ok {
				t.Errorf("expected othermodb in effective set, got %v", res.EffectiveSet)
			}
		})
	}
}
