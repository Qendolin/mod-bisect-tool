package mods

import (
	"testing"
)

// TestProviderLogShowsNestedJarPath verifies that provider log lines identify
// the exact jar providing a dependency, including its path inside the parent
// mod. Two nested providers can share the same top-level mod ID and version,
// so only the nested jar path distinguishes them.
func TestProviderLogShowsNestedJarPath(t *testing.T) {
	jacksonID := "com_fasterxml_jackson_core_jackson-databind"
	jacksonMeta := func() ModMetadata {
		return ModMetadata{
			ID:      jacksonID,
			Name:    "jackson-databind",
			Version: VersionField{Version: mustParseVersion(t, "2.15.2")},
		}
	}

	tierify := &Mod{
		BaseFilename: "Tierify-FABRIC-1.20.1-1.2.0",
		Metadata: ModMetadata{
			ID:      "tiered",
			Name:    "Tierify",
			Version: VersionField{Version: mustParseVersion(t, "1.2.0")},
		},
		NestedModules: []NestedModule{
			// PathInJar includes the top-level jar name, as built by the parser.
			{Info: jacksonMeta(), PathInJar: "Tierify-FABRIC-1.20.1-1.2.0.jar/META-INF/jars/jackson-databind-2.15.2.jar"},
			{
				Info: ModMetadata{
					ID:      "libz",
					Name:    "libz",
					Version: VersionField{Version: mustParseVersion(t, "1.0.2+1.20.1")},
				},
				PathInJar: "Tierify-FABRIC-1.20.1-1.2.0.jar/META-INF/jars/libz-1.0.2+1.20.1.jar",
			},
			// jackson-databind nested inside the nested libz jar.
			{Info: jacksonMeta(), PathInJar: "Tierify-FABRIC-1.20.1-1.2.0.jar/META-INF/jars/libz-1.0.2+1.20.1.jar/META-INF/jars/jackson-databind-2.15.2.jar"},
		},
	}

	standaloneLibz := &Mod{
		BaseFilename: "libz-1.0.3",
		Metadata: ModMetadata{
			ID:      "libz",
			Name:    "libz",
			Version: VersionField{Version: mustParseVersion(t, "1.0.3")},
		},
		NestedModules: []NestedModule{
			{Info: jacksonMeta(), PathInJar: "libz-1.0.3.jar/META-INF/jars/jackson-databind-2.15.2.jar"},
		},
	}

	allMods := map[string]*Mod{
		"tiered": tierify,
		"libz":   standaloneLibz,
	}

	potentialProviders := PotentialProvidersMap{}
	populateProviderMaps(allMods, potentialProviders)

	infos, ok := potentialProviders[jacksonID]
	if !ok {
		t.Fatalf("expected %q in potential providers", jacksonID)
	}
	if len(infos) != 3 {
		t.Fatalf("expected 3 providers for %s, got %d: %+v", jacksonID, len(infos), infos)
	}

	sortProviders(infos)
	lines := formatProviderLog(jacksonID, infos)
	want := []string{
		"  - Dependency " + jacksonID + " provided by:",
		"      - tiered (at v2.15.2) from 'Tierify-FABRIC-1.20.1-1.2.0.jar/META-INF/jars/jackson-databind-2.15.2.jar'",
		"      - tiered (at v2.15.2) from 'Tierify-FABRIC-1.20.1-1.2.0.jar/META-INF/jars/libz-1.0.2+1.20.1.jar/META-INF/jars/jackson-databind-2.15.2.jar'",
		"      - libz (at v2.15.2) from 'libz-1.0.3.jar/META-INF/jars/jackson-databind-2.15.2.jar'",
	}
	if len(lines) != len(want) {
		t.Fatalf("expected %d log lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, wantLine := range want {
		if lines[i] != wantLine {
			t.Errorf("log line %d:\n got  %s\n want %s", i, lines[i], wantLine)
		}
	}
}

// TestProviderLogShowsProvidingJarForProvides verifies that a "provides"
// entry is attributed to the jar whose manifest declares it.
func TestProviderLogShowsProvidingJarForProvides(t *testing.T) {
	provider := &Mod{
		BaseFilename: "mylib",
		Metadata: ModMetadata{
			ID:      "mylib",
			Name:    "My Lib",
			Version: VersionField{Version: mustParseVersion(t, "1.0.0")},
			Provides: []string{
				"some-api",
				"other-api",
			},
		},
	}

	allMods := map[string]*Mod{"mylib": provider}
	potentialProviders := PotentialProvidersMap{}
	populateProviderMaps(allMods, potentialProviders)

	for _, depID := range []string{"some-api", "other-api"} {
		infos, ok := potentialProviders[depID]
		if !ok {
			t.Fatalf("expected %q in potential providers", depID)
		}
		sortProviders(infos)
		lines := formatProviderLog(depID, infos)
		want := "  - Dependency " + depID + " provided by mylib (at v1.0.0) from 'mylib.jar'"
		if len(lines) != 1 || lines[0] != want {
			t.Errorf("expected single line %q, got %v", want, lines)
		}
	}
}
