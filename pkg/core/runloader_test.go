package app_test

import (
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
)

// nfModTOML is a minimal neoforge.mods.toml for the "nf_mod" test mod.
const nfModTOML = `modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "nf_mod"
version = "1.0"
displayName = "NF Mod"`

// TestRunLoaderManifestPriority verifies that the loader selection decides which
// mod manifests are recognized and in which order, i.e. that (Neo)Forge mods are
// ignored by the Fabric loader, Fabric mods are ignored by the (Neo)Forge
// loader, and both are accepted (with their respective fallbacks) by Connector
// and Kilt.
func TestRunLoaderManifestPriority(t *testing.T) {
	fabricModSpec := modSpec{JSONContent: `{"id": "fabric_mod", "version": "1.0"}`}
	nfModSpec := neoForgeModSpec{TOMLContent: nfModTOML}

	tests := []struct {
		name           string
		loader         mods.RunLoader
		expectedMods   []string
		unexpectedMods []string
	}{
		{
			name:           "Fabric_ignores_NeoForge_mods",
			loader:         mods.RunLoaderFabric,
			expectedMods:   []string{"fabric_mod"},
			unexpectedMods: []string{"nf_mod"},
		},
		{
			name:           "NeoForge_ignores_Fabric_mods",
			loader:         mods.RunLoaderNeoForge,
			expectedMods:   []string{"nf_mod"},
			unexpectedMods: []string{"fabric_mod"},
		},
		{
			name:           "Connector_loads_both",
			loader:         mods.RunLoaderNeoForgeWithFabric,
			expectedMods:   []string{"fabric_mod", "nf_mod"},
			unexpectedMods: []string{},
		},
		{
			name:           "Kilt_loads_both",
			loader:         mods.RunLoaderFabricWithNeoForge,
			expectedMods:   []string{"fabric_mod", "nf_mod"},
			unexpectedMods: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()

			setupDummyMods(t, modsDir, map[string]modSpec{
				"fabric_mod-1.0.jar": fabricModSpec,
			})
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
				"nf_mod-1.0.jar": nfModSpec,
			})

			adapter := mods.FileAdapter{BaseDirectory: modsDir}
			loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: tc.loader}, Adapter: &adapter}
			allMods, _, err := loader.LoadMods(modsDir, nil, nil)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}

			for _, id := range tc.expectedMods {
				if _, ok := allMods[id]; !ok {
					t.Errorf("expected mod %q to be loaded with loader %s; got %v", id, tc.loader, loadedModIDs(allMods))
				}
			}
			for _, id := range tc.unexpectedMods {
				if _, ok := allMods[id]; ok {
					t.Errorf("mod %q should not be loaded with loader %s; got %v", id, tc.loader, loadedModIDs(allMods))
				}
			}
		})
	}
}

// TestRunLoaderSkipsForeignMods verifies that under a pure loader, jars whose
// manifests target the other loader are skipped with a warning rather than being
// loaded or misclassified as (Neo)Forge libraries.
func TestRunLoaderSkipsForeignMods(t *testing.T) {
	fabricModSpec := modSpec{JSONContent: `{"id": "fabric_mod", "version": "1.0"}`}
	nfModSpec := neoForgeModSpec{TOMLContent: nfModTOML}

	tests := []struct {
		name         string
		loader       mods.RunLoader
		expectedMods []string
	}{
		{
			name:         "Fabric_skips_NeoForge_mod",
			loader:       mods.RunLoaderFabric,
			expectedMods: []string{"fabric_mod"},
		},
		{
			name:         "NeoForge_skips_Fabric_mod",
			loader:       mods.RunLoaderNeoForge,
			expectedMods: []string{"nf_mod"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()

			setupDummyMods(t, modsDir, map[string]modSpec{
				"fabric_mod-1.0.jar": fabricModSpec,
			})
			setupDummyNeoForgeMods(t, modsDir, map[string]neoForgeModSpec{
				"nf_mod-1.0.jar": nfModSpec,
			})

			adapter := mods.FileAdapter{BaseDirectory: modsDir}
			loader := mods.ModLoader{ModParser: mods.ModParser{RunLoader: tc.loader}, Adapter: &adapter}
			allMods, _, err := loader.LoadMods(modsDir, nil, nil)
			if err != nil {
				t.Fatalf("LoadMods failed: %v", err)
			}

			if len(allMods) != len(tc.expectedMods) {
				t.Fatalf("expected %d mods to be loaded, got %d: %v", len(tc.expectedMods), len(allMods), loadedModIDs(allMods))
			}
			for _, id := range tc.expectedMods {
				if _, ok := allMods[id]; !ok {
					t.Errorf("expected mod %q to be loaded with loader %s; got %v", id, tc.loader, loadedModIDs(allMods))
				}
			}
		})
	}
}

func loadedModIDs(allMods map[string]*mods.Mod) []string {
	ids := make([]string, 0, len(allMods))
	for id := range allMods {
		ids = append(ids, id)
	}
	return ids
}
