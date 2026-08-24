package probe

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
)

// writeJar writes a jar containing the given files (path -> content).
func writeJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	jar, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating jar %s: %v", path, err)
	}
	defer jar.Close()
	zw := zip.NewWriter(jar)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("creating %s in %s: %v", name, path, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("writing %s in %s: %v", name, path, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing zip %s: %v", path, err)
	}
}

func fabricJar(t *testing.T, modsDir, filename, id string) {
	t.Helper()
	writeJar(t, filepath.Join(modsDir, filename), map[string]string{
		"fabric.mod.json": `{"id": "` + id + `", "version": "1.0"}`,
	})
}

func neoForgeJar(t *testing.T, modsDir, filename string) {
	t.Helper()
	writeJar(t, filepath.Join(modsDir, filename), map[string]string{
		"META-INF/neoforge.mods.toml": `modLoader = "javafml"
[[mods]]
modId = "nf_mod"
version = "1.0"`,
	})
}

func kiltJar(t *testing.T, modsDir, filename string) {
	t.Helper()
	writeJar(t, filepath.Join(modsDir, filename), map[string]string{
		"fabric.mod.json": `{"id": "kilt", "version": "1.0"}`,
	})
}

func connectorJar(t *testing.T, modsDir, filename string) {
	t.Helper()
	writeJar(t, filepath.Join(modsDir, filename), map[string]string{
		"META-INF/neoforge.mods.toml": `modLoader = "javafml"
[[mods]]
modId = "connector"
version = "1.0"`,
		connectorServicePath: "sinytra.connector.ConnectorTransformationService\n",
	})
}

// connectorJarViaManifest builds a Connector jar that is only identifiable by
// its Automatic-Module-Name (v2/v3 jars no longer ship the transformation
// service file).
func connectorJarViaManifest(t *testing.T, modsDir, filename string) {
	t.Helper()
	writeJar(t, filepath.Join(modsDir, filename), map[string]string{
		"META-INF/neoforge.mods.toml": `modLoader = "javafml"
[[mods]]
modId = "connector"
version = "1.0"`,
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nAutomatic-Module-Name: org.sinytra.connector\n",
	})
}

func TestProbeModsDirectory(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, modsDir string)
		expected mods.RunLoader
	}{
		{
			name:     "Empty_directory_returns_no_loader",
			setup:    func(*testing.T, string) {},
			expected: "",
		},
		{
			name:     "Only_Fabric_mods",
			setup:    func(t *testing.T, dir string) { fabricJar(t, dir, "a.jar", "mod_a") },
			expected: mods.RunLoaderFabric,
		},
		{
			name:     "Only_NeoForge_mods",
			setup:    func(t *testing.T, dir string) { neoForgeJar(t, dir, "b.jar") },
			expected: mods.RunLoaderNeoForge,
		},
		{
			name: "Fabric_majority",
			setup: func(t *testing.T, dir string) {
				fabricJar(t, dir, "a.jar", "mod_a")
				fabricJar(t, dir, "b.jar", "mod_b")
				neoForgeJar(t, dir, "c.jar")
			},
			expected: mods.RunLoaderFabric,
		},
		{
			name: "NeoForge_majority",
			setup: func(t *testing.T, dir string) {
				fabricJar(t, dir, "a.jar", "mod_a")
				neoForgeJar(t, dir, "b.jar")
				neoForgeJar(t, dir, "c.jar")
			},
			expected: mods.RunLoaderNeoForge,
		},
		{
			name: "Kilt_present",
			setup: func(t *testing.T, dir string) {
				kiltJar(t, dir, "kilt.jar")
				neoForgeJar(t, dir, "nf.jar")
			},
			expected: mods.RunLoaderFabricWithNeoForge,
		},
		{
			name: "Connector_present",
			setup: func(t *testing.T, dir string) {
				connectorJar(t, dir, "connector.jar")
				fabricJar(t, dir, "fabric.jar", "mod_fabric")
			},
			expected: mods.RunLoaderNeoForgeWithFabric,
		},
		{
			name: "Connector_present_via_manifest",
			setup: func(t *testing.T, dir string) {
				connectorJarViaManifest(t, dir, "connector.jar")
				neoForgeJar(t, dir, "nf.jar")
			},
			expected: mods.RunLoaderNeoForgeWithFabric,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modsDir := t.TempDir()
			tc.setup(t, modsDir)

			result := ProbeModsDirectory(modsDir)
			if result.PrimaryLoader != tc.expected {
				t.Errorf("expected primary loader %s, got %s", tc.expected, result.PrimaryLoader)
			}
		})
	}
}
