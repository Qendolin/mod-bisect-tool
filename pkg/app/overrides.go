package app

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/embeds"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

func loadAndMergeOverrides(modsPath string, cliArgs CLIArgs) *mods.DependencyOverrides {
	var allOverrides []*mods.DependencyOverrides

	cwd, _ := os.Getwd()
	appendUserOverrideFile(filepath.Join(cwd, "dependency_overrides.json"), "current directory", &allOverrides)
	if AppDistribution == AppDistributionDarwinApp {
		home, err := os.UserHomeDir()
		if err != nil {
			logging.Warnf("App: Could not determine user home directory: %v", err)
		} else {
			appConfigDir := filepath.Join(home, "Library", "Application Support", AppCommonName)
			appendUserOverrideFile(filepath.Join(appConfigDir, "dependency_overrides.json"), "app directory", &allOverrides)
		}
	}

	configDir := filepath.Join(modsPath, "..", "config")
	appendUserOverrideFile(filepath.Join(configDir, "dependency_overrides.json"), "config directory", &allOverrides)

	if cliArgs.AdditionalOverridesPath != "" {
		appendUserOverrideFile(cliArgs.AdditionalOverridesPath, "command line", &allOverrides)
	}

	if !cliArgs.NoEmbeddedOverrides {
		if embedded, err := mods.LoadDependencyOverrides(bytes.NewReader(embeds.GetEmbeddedOverrides()), mods.OverrideSourceBuiltin); err != nil {
			logging.Errorf("App: Failed to load embedded dependency overrides: %v", err)
		} else {
			logging.Infof("App: Loaded embedded dependency overrides.")
			allOverrides = append(allOverrides, embedded)
		}
	}

	return mods.MergeDependencyOverrides(allOverrides...)
}

func appendUserOverrideFile(path string, sourceName string, allOverrides *[]*mods.DependencyOverrides) {
	if path == "" {
		return
	}
	overrides, err := mods.LoadDependencyOverridesFromPath(path, mods.OverrideSourceUserProvided)
	if err != nil {
		if !os.IsNotExist(err) {
			logging.Warnf("App: Could not load dependency overrides from '%s': %v", path, err)
		}
		return
	}
	logging.Infof("App: Loaded dependency overrides from %s: %s", sourceName, path)
	*allOverrides = append(*allOverrides, overrides)
}
