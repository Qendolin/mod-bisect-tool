package mods

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipWith writes a zip archive containing the given path -> content entries.
func zipWith(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestFallbackWarningOncePerSubtree verifies that the "(Neo)Forge parsing is
// enabled but ... will fall back" warning is emitted at most once per nested
// subtree, even when a Fabric mod contains nested Fabric mods.
func TestFallbackWarningOncePerSubtree(t *testing.T) {
	dir := t.TempDir()

	nestedJar := zipWith(t, map[string][]byte{
		"fabric.mod.json": []byte(`{"id": "nested_mod", "version": "1.0"}`),
	})
	topJar := zipWith(t, map[string][]byte{
		"fabric.mod.json": []byte(`{"id": "top_mod", "version": "1.0", "jars": [{"file": "nested.jar"}]}`),
		"nested.jar":      nestedJar,
	})
	jarPath := filepath.Join(dir, "top_mod.jar")
	if err := os.WriteFile(jarPath, topJar, 0644); err != nil {
		t.Fatal(err)
	}

	p := ModParser{RunLoader: RunLoaderNeoForgeWithFabric}
	var lb logBuffer
	if _, _, _, err := p.ExtractModMetadata(jarPath, "top_mod.jar", &lb); err != nil {
		t.Fatalf("ExtractModMetadata failed: %v", err)
	}

	fallbackCount := 0
	for _, entry := range lb.entries {
		if strings.Contains(entry.Message, "will fall back to") {
			fallbackCount++
		}
	}
	if fallbackCount != 1 {
		t.Fatalf("expected exactly 1 fallback warning for the nested subtree, got %d (entries: %+v)", fallbackCount, lb.entries)
	}
}

// TestMissingNestedJarAcrossLoaders verifies that a (Neo)Forge mod whose jarjar
// metadata references a nested jar that is not actually present still loads, and
// the missing nested jar is skipped without failing the whole jar.
func TestMissingNestedJarAcrossLoaders(t *testing.T) {
	jarBytes := zipWith(t, map[string][]byte{
		"META-INF/neoforge.mods.toml": []byte(`modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "with_missing_nested"
version = "1.0"
displayName = "With Missing Nested"`),
		"META-INF/jarjar/metadata.json": []byte(`{"jars": [{"path": "META-INF/jarjar/missing.jar"}]}`),
	})

	for _, tc := range []struct {
		name   string
		loader RunLoader
	}{
		{"Kilt", RunLoaderFabricWithNeoForge},
		{"Connector", RunLoaderNeoForgeWithFabric},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			jarPath := filepath.Join(dir, "with_missing_nested.jar")
			if err := os.WriteFile(jarPath, jarBytes, 0644); err != nil {
				t.Fatal(err)
			}

			p := ModParser{RunLoader: tc.loader}
			var lb logBuffer
			metadata, nested, _, err := p.ExtractModMetadata(jarPath, "with_missing_nested.jar", &lb)
			if err != nil {
				t.Fatalf("ExtractModMetadata failed: %v", err)
			}
			if metadata.ID != "with_missing_nested" {
				t.Fatalf("expected mod with_missing_nested to load, got %q", metadata.ID)
			}
			if len(nested) != 0 {
				t.Errorf("expected no nested modules for a missing nested jar, got %+v", nested)
			}
		})
	}
}
