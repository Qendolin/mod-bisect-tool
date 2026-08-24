package app_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

const testDir = "testdata"

// modSpec defines the structure for creating a dummy mod.
type modSpec struct {
	JSONContent string
	NestedJars  map[string]modSpec
	RawFiles    map[string]string
}

// setupDummyMods creates a temporary mods directory and files.
func setupDummyMods(t *testing.T, modsDir string, specs map[string]modSpec) {
	t.Helper()
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		t.Fatalf("failed to create mods dir '%s': %v", modsDir, err)
	}
	for filename, spec := range specs {
		jarPath := filepath.Join(modsDir, filename)
		jarBytes, err := createJarFromSpec(t, spec)
		if err != nil {
			t.Fatalf("failed to create JAR data for %s: %v", filename, err)
		}
		if err := os.WriteFile(jarPath, jarBytes, 0644); err != nil {
			t.Fatalf("failed to write dummy mod file %s: %v", jarPath, err)
		}
	}
}

// createJarFromSpec is a recursive helper to build a JAR file from a spec.
func createJarFromSpec(t *testing.T, spec modSpec) ([]byte, error) {
	t.Helper()
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	if spec.JSONContent != "" {
		modJsonFile, err := zipWriter.Create("fabric.mod.json")
		if err != nil {
			return nil, err
		}
		if _, err = modJsonFile.Write([]byte(spec.JSONContent)); err != nil {
			return nil, err
		}
	}
	// Add raw files.
	for path, content := range spec.RawFiles {
		rawFile, err := zipWriter.Create(path)
		if err != nil {
			return nil, err
		}
		if _, err = rawFile.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	// Add nested JARs.
	for nestedFilename, nestedSpec := range spec.NestedJars {
		nestedJarBytes, err := createJarFromSpec(t, nestedSpec)
		if err != nil {
			return nil, err
		}
		nestedJarFile, err := zipWriter.Create(nestedFilename)
		if err != nil {
			return nil, err
		}
		if _, err := nestedJarFile.Write(nestedJarBytes); err != nil {
			return nil, err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return zipBuf.Bytes(), nil
}

func setupLogger(t *testing.T) *os.File {
	mainLogger := logging.NewLogger()
	mainLogger.SetDebug(true)
	filename := fmt.Sprintf("%s-latest.log", t.Name())
	logFile, err := os.OpenFile(filepath.Join(testDir, filename), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		t.Fatalf("Failed to open log file: %v", err)
	}
	mainLogger.SetWriter(logFile)
	logging.SetDefault(mainLogger)
	return logFile
}

func copySets(original []sets.Set) []sets.Set {
	setsCopy := make([]sets.Set, len(original))
	for i, s := range original {
		setsCopy[i] = sets.Copy(s)
	}
	return setsCopy
}

func deepCopySpecs(original map[string]modSpec) map[string]modSpec {
	cpy := make(map[string]modSpec)
	for k, v := range original {
		cpy[k] = v // This is a shallow copy of the inner map, but sufficient for these tests
	}
	return cpy
}
