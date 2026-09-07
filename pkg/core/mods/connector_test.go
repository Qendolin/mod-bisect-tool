package mods

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeJar(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	jarPath := filepath.Join(dir, name)
	if err := os.WriteFile(jarPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	return jarPath
}

// TestConnectorModPropertiesProvides verifies that [modproperties.<modId>]
// "fabric:provides" is merged into the mod's provides under the Connector
// loader, and that it is ignored by a plain (Neo)Forge loader.
func TestConnectorModPropertiesProvides(t *testing.T) {
	tomlContent := `[[mods]]
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
	jar := zipWith(t, map[string][]byte{
		"META-INF/neoforge.mods.toml": []byte(tomlContent),
	})
	dir := t.TempDir()
	jarPath := writeJar(t, dir, "connector.jar", jar)

	connector := ModParser{RunLoader: RunLoaderNeoForgeWithFabric}
	var connectorLogs logBuffer
	connectorMetadata, _, _, err := connector.ExtractModMetadata(jarPath, "connector.jar", &connectorLogs)
	if err != nil {
		t.Fatalf("ExtractModMetadata (Connector) failed: %v", err)
	}
	if !slices.Contains(connectorMetadata.Provides, "fabric_api_base") {
		t.Errorf("Connector: expected provides to contain secondary mod ID fabric_api_base, got %v", connectorMetadata.Provides)
	}
	if !slices.Contains(connectorMetadata.Provides, "fabric-api-base") {
		t.Errorf("Connector: expected provides to contain modproperties fabric:provides fabric-api-base, got %v", connectorMetadata.Provides)
	}

	neoForge := ModParser{RunLoader: RunLoaderNeoForge}
	var neoForgeLogs logBuffer
	neoForgeMetadata, _, _, err := neoForge.ExtractModMetadata(jarPath, "connector.jar", &neoForgeLogs)
	if err != nil {
		t.Fatalf("ExtractModMetadata (NeoForge) failed: %v", err)
	}
	if !slices.Contains(neoForgeMetadata.Provides, "fabric_api_base") {
		t.Errorf("NeoForge: expected provides to contain secondary mod ID fabric_api_base, got %v", neoForgeMetadata.Provides)
	}
	if slices.Contains(neoForgeMetadata.Provides, "fabric-api-base") {
		t.Errorf("NeoForge: did not expect modproperties provides fabric-api-base without the Connector loader, got %v", neoForgeMetadata.Provides)
	}
}

// TestConnectorPlaceholderMod verifies that a Connector placeholder jar (dummy
// forge manifest + real fabric.mod.json) is loaded as its Fabric self under the
// Connector loader without a fallback warning, and stays an FML mod under a
// plain (Neo)Forge loader.
func TestConnectorPlaceholderMod(t *testing.T) {
	placeholderToml := `[properties]
"connector:placeholder" = true

[[mods]]
modId = "dummy_fml"
version = "1.0"
displayName = "Dummy FML"
`
	jar := zipWith(t, map[string][]byte{
		"META-INF/neoforge.mods.toml": []byte(placeholderToml),
		"fabric.mod.json":             []byte(`{"id": "real_mod", "version": "1.0", "name": "Real Mod"}`),
	})
	dir := t.TempDir()
	jarPath := writeJar(t, dir, "placeholder.jar", jar)

	connector := ModParser{RunLoader: RunLoaderNeoForgeWithFabric}
	var connectorLogs logBuffer
	connectorMetadata, _, _, err := connector.ExtractModMetadata(jarPath, "placeholder.jar", &connectorLogs)
	if err != nil {
		t.Fatalf("ExtractModMetadata (Connector) failed: %v", err)
	}
	if connectorMetadata.ID != "real_mod" {
		t.Errorf("Connector: expected placeholder jar to load as its Fabric mod real_mod, got %q", connectorMetadata.ID)
	}
	if connectorMetadata.Loader != ManifestLoaderFabric {
		t.Errorf("Connector: expected Loader Fabric, got %q", connectorMetadata.Loader)
	}
	for _, entry := range connectorLogs.entries {
		if strings.Contains(entry.Message, "will fall back to") {
			t.Errorf("Connector: fallback warning should not fire for an intentional placeholder, got %q", entry.Message)
		}
	}

	neoForge := ModParser{RunLoader: RunLoaderNeoForge}
	var neoForgeLogs logBuffer
	neoForgeMetadata, _, _, err := neoForge.ExtractModMetadata(jarPath, "placeholder.jar", &neoForgeLogs)
	if err != nil {
		t.Fatalf("ExtractModMetadata (NeoForge) failed: %v", err)
	}
	if neoForgeMetadata.ID != "dummy_fml" {
		t.Errorf("NeoForge: expected placeholder jar to stay an FML mod (dummy_fml), got %q", neoForgeMetadata.ID)
	}
}

// TestConnectorPlaceholderWithoutFabricManifestMergesProvides verifies that a
// Connector placeholder jar lacking a Fabric manifest keeps its FML identity
// while still folding modproperties fabric:provides into its provides.
func TestConnectorPlaceholderWithoutFabricManifestMergesProvides(t *testing.T) {
	placeholderToml := `[properties]
"connector:placeholder" = true

[[mods]]
modId = "dummy_fml"
version = "1.0"
displayName = "Dummy FML"

[modproperties.dummy_fml]
"fabric:provides" = ["fabric-api-base"]
`
	jar := zipWith(t, map[string][]byte{
		"META-INF/neoforge.mods.toml": []byte(placeholderToml),
	})
	dir := t.TempDir()
	jarPath := writeJar(t, dir, "placeholder.jar", jar)

	p := ModParser{RunLoader: RunLoaderNeoForgeWithFabric}
	var lb logBuffer
	metadata, _, _, err := p.ExtractModMetadata(jarPath, "placeholder.jar", &lb)
	if err != nil {
		t.Fatalf("ExtractModMetadata failed: %v", err)
	}
	if metadata.ID != "dummy_fml" {
		t.Fatalf("expected the placeholder to keep its FML identity dummy_fml, got %q", metadata.ID)
	}
	if !slices.Contains(metadata.Provides, "fabric-api-base") {
		t.Errorf("expected modproperties fabric:provides fabric-api-base to be folded in, got %v", metadata.Provides)
	}
}

// TestConnectorFallbackWarningForFabricOnlyMod verifies that the relocated
// fallback warning still fires once for a genuine Fabric-only jar.
func TestConnectorFallbackWarningForFabricOnlyMod(t *testing.T) {
	jar := zipWith(t, map[string][]byte{
		"fabric.mod.json": []byte(`{"id": "plain_fabric", "version": "1.0"}`),
	})
	dir := t.TempDir()
	jarPath := writeJar(t, dir, "plain.jar", jar)

	p := ModParser{RunLoader: RunLoaderNeoForgeWithFabric}
	var lb logBuffer
	if _, _, _, err := p.ExtractModMetadata(jarPath, "plain.jar", &lb); err != nil {
		t.Fatalf("ExtractModMetadata failed: %v", err)
	}

	fallbackCount := 0
	for _, entry := range lb.entries {
		if strings.Contains(entry.Message, "will fall back to") {
			fallbackCount++
		}
	}
	if fallbackCount != 1 {
		t.Fatalf("expected exactly 1 fallback warning for a Fabric-only jar, got %d (entries: %+v)", fallbackCount, lb.entries)
	}
}

// TestConnectorModPropertiesProvidesDeduplicated verifies that modproperties
// fabric:provides are not duplicated when the same ID is also declared in the
// [[mods]] provides field.
func TestConnectorModPropertiesProvidesDeduplicated(t *testing.T) {
	tomlContent := `[[mods]]
modId = "connector"
version = "1.0"
displayName = "Connector"

[[mods]]
modId = "fabric_api_base"
version = "1.0"
displayName = "Fabric API Base"
provides = ["fabric-api-base"]

[modproperties.fabric_api_base]
"fabric:provides" = ["fabric-api-base"]
`
	jar := zipWith(t, map[string][]byte{
		"META-INF/neoforge.mods.toml": []byte(tomlContent),
	})
	dir := t.TempDir()
	jarPath := writeJar(t, dir, "connector.jar", jar)

	p := ModParser{RunLoader: RunLoaderNeoForgeWithFabric}
	var lb logBuffer
	metadata, _, _, err := p.ExtractModMetadata(jarPath, "connector.jar", &lb)
	if err != nil {
		t.Fatalf("ExtractModMetadata failed: %v", err)
	}

	count := 0
	for _, provided := range metadata.Provides {
		if provided == "fabric-api-base" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected fabric-api-base to be provided exactly once, got %d (provides: %v)", count, metadata.Provides)
	}
}

// TestForeignManifestPresenceOnly verifies that a pure loader only checks
// presence of a foreign manifest instead of parsing it, so a malformed foreign
// manifest does not break a valid mod of the loader's own family.
func TestForeignManifestPresenceOnly(t *testing.T) {
	badToml := `this is not { valid toml, at all !`
	badJSON := `{"id": not valid json`

	t.Run("Fabric_ignores_malformed_neoForge_manifest", func(t *testing.T) {
		jar := zipWith(t, map[string][]byte{
			"fabric.mod.json":             []byte(`{"id": "plain_fabric", "version": "1.0"}`),
			"META-INF/neoforge.mods.toml": []byte(badToml),
		})
		dir := t.TempDir()
		jarPath := writeJar(t, dir, "plain.jar", jar)

		p := ModParser{RunLoader: RunLoaderFabric}
		var lb logBuffer
		metadata, _, _, err := p.ExtractModMetadata(jarPath, "plain.jar", &lb)
		if err != nil {
			t.Fatalf("ExtractModMetadata failed: %v", err)
		}
		if metadata.ID != "plain_fabric" {
			t.Errorf("expected fabric mod plain_fabric, got %q", metadata.ID)
		}
	})

	t.Run("NeoForge_ignores_malformed_fabric_manifest", func(t *testing.T) {
		jar := zipWith(t, map[string][]byte{
			"fabric.mod.json": []byte(badJSON),
			"META-INF/neoforge.mods.toml": []byte(`modLoader = "javafml"
loaderVersion = "[1,)"
[[mods]]
modId = "plain_nf"
version = "1.0"
displayName = "Plain NF"`),
		})
		dir := t.TempDir()
		jarPath := writeJar(t, dir, "plain.jar", jar)

		p := ModParser{RunLoader: RunLoaderNeoForge}
		var lb logBuffer
		metadata, _, _, err := p.ExtractModMetadata(jarPath, "plain.jar", &lb)
		if err != nil {
			t.Fatalf("ExtractModMetadata failed: %v", err)
		}
		if metadata.ID != "plain_nf" {
			t.Errorf("expected neoforge mod plain_nf, got %q", metadata.ID)
		}
	})
}
