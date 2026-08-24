// Package probe detects which mod loader a mods directory is set up for, based
// on the presence of bridge mods (Sinytra Connector / Kilt) or the majority of
// detected mod manifests.
package probe

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// IsValidDir reports whether path refers to an existing directory.
func IsValidDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ProbeResult contains the findings of the directory probe.
type ProbeResult struct {
	// PrimaryLoader is the loader the probe recommends based on the detected
	// bridge mods (Sinytra Connector / Kilt) or, failing that, on the majority
	// of detected mod manifests.
	PrimaryLoader mods.RunLoader
}

// connectorServicePath is the service file under which Sinytra Connector
// registers its transformation service (Connector v1 and v2).
const connectorServicePath = "META-INF/services/cpw.mods.modlauncher.api.ITransformationService"

// kiltModID is the fabric.mod.json id of the Kilt bridge mod.
const kiltModID = "kilt"

// ProbeModsDirectory scans the .jar files in the given directory to determine
// the mod loader the mods are intended for, without fully parsing any manifest.
func ProbeModsDirectory(modsPath string) ProbeResult {
	result := ProbeResult{}

	entries, err := os.ReadDir(modsPath)
	if err != nil {
		logging.Errorf("Probe: failed to read directory %s: %v", modsPath, err)
		return result
	}

	fabricMods := 0
	neoForgeMods := 0
	connectorFound := false
	kiltFound := false

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jar") {
			continue
		}

		p := probeJar(filepath.Join(modsPath, entry.Name()), !connectorFound, !kiltFound)
		if p.fabricManifest {
			fabricMods++
		}
		if p.neoForgeManifest {
			neoForgeMods++
		}
		if p.connector {
			connectorFound = true
		}
		if p.kilt {
			kiltFound = true
		}
	}

	switch {
	case connectorFound:
		result.PrimaryLoader = mods.RunLoaderNeoForgeWithFabric
	case kiltFound:
		result.PrimaryLoader = mods.RunLoaderFabricWithNeoForge
	case fabricMods == 0 && neoForgeMods == 0:
		// No mods detected; there is nothing to recommend.
		result.PrimaryLoader = ""
	case neoForgeMods > fabricMods:
		result.PrimaryLoader = mods.RunLoaderNeoForge
	default:
		result.PrimaryLoader = mods.RunLoaderFabric
	}

	logging.Infof("Probe: Finished probing %s. PrimaryLoader=%s, FabricMods=%d, NeoForgeMods=%d, Kilt=%v, Connector=%v",
		modsPath, result.PrimaryLoader, fabricMods, neoForgeMods, kiltFound, connectorFound)
	return result
}

// jarProbe is the per-jar classification result.
type jarProbe struct {
	fabricManifest   bool
	neoForgeManifest bool
	connector        bool
	kilt             bool
}

// probeJar inspects a single jar. It only looks at the zip directory and reads
// a few small files when asked to (the transformation service, MANIFEST.MF and
// fabric.mod.json id). checkConnector/checkKilt disable those reads once the
// corresponding bridge mod has already been found elsewhere.
func probeJar(jarPath string, checkConnector, checkKilt bool) jarProbe {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return jarProbe{}
	}
	defer r.Close()

	var p jarProbe
	for _, f := range r.File {
		switch f.Name {
		case connectorServicePath:
			if checkConnector && !p.connector {
				p.connector = containsConnectorService(f)
			}
		case "META-INF/MANIFEST.MF":
			if checkConnector && !p.connector {
				p.connector = containsConnectorModuleName(f)
			}
		case "fabric.mod.json":
			p.fabricManifest = true
			if checkKilt && !p.kilt {
				p.kilt = isKilt(f)
			}
		case "quilt.mod.json":
			p.fabricManifest = true
		case "META-INF/neoforge.mods.toml", "META-INF/mods.toml":
			p.neoForgeManifest = true
		}
	}
	return p
}

// containsConnectorService reports whether a transformation service file
// references Sinytra Connector (Connector v1 and v2).
func containsConnectorService(f *zip.File) bool {
	rc, err := f.Open()
	if err != nil {
		return false
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		return false
	}
	return bytes.Contains(content, []byte("sinytra.connector"))
}

// containsConnectorModuleName reports whether a MANIFEST.MF declares the
// org.sinytra.connector automatic module (Connector v2 and v3).
func containsConnectorModuleName(f *zip.File) bool {
	rc, err := f.Open()
	if err != nil {
		return false
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		return false
	}
	return bytes.Contains(content, []byte("Automatic-Module-Name: dev.su5ed.sinytra.connector")) || bytes.Contains(content, []byte("Automatic-Module-Name: org.sinytra.connector"))
}

// isKilt reports whether a fabric.mod.json belongs to the Kilt bridge mod.
func isKilt(f *zip.File) bool {
	rc, err := f.Open()
	if err != nil {
		return false
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		return false
	}
	var meta struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(content, &meta); err != nil {
		return false
	}
	return meta.ID == kiltModID
}
