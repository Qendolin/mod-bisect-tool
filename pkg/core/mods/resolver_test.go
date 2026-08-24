package mods

import (
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// parsePred is a test helper to parse a version predicate string.
func parsePred(t *testing.T, s string) *version.VersionPredicate {
	t.Helper()
	p, err := version.ParseVersionPredicate(s)
	if err != nil {
		t.Fatalf("failed to parse predicate %q: %v", s, err)
	}
	return p
}

// mustParseVersion is a test helper to parse a version string.
func mustParseVersion(t *testing.T, s string) version.Version {
	t.Helper()
	v, err := version.Parse(s, false)
	if err != nil {
		t.Fatalf("failed to parse version %q: %v", s, err)
	}
	return v
}

// TestDependencyWithORPredicates verifies that a dependency declared as an OR
// list of predicates (e.g. "sodium": ["=0.8.11", "=0.8.12"]) is satisfiable as
// long as the provider matches at least one of them.
func TestDependencyWithORPredicates(t *testing.T) {
	// nvidium requires sodium 0.8.11 OR 0.8.12.
	nvidium := &Mod{
		BaseFilename: "nvidium.jar",
		Metadata: ModMetadata{
			ID:   "nvidium",
			Name: "Nvidium",
			Depends: VersionRanges{
				"sodium": {parsePred(t, "=0.8.11"), parsePred(t, "=0.8.12")},
			},
		},
	}
	// sodium provides the "sodium" dependency at 0.8.11.
	sodium := &Mod{
		BaseFilename: "sodium.jar",
		Metadata: ModMetadata{
			ID:      "sodium",
			Name:    "Sodium",
			Version: VersionField{Version: mustParseVersion(t, "0.8.11")},
		},
	}

	allMods := map[string]*Mod{
		"nvidium": nvidium,
		"sodium":  sodium,
	}
	potentialProviders := PotentialProvidersMap{
		"sodium": {{
			TopLevelModID:         "sodium",
			VersionOfProvidedItem: mustParseVersion(t, "0.8.11"),
			IsDirectProvide:       true,
		}},
	}

	dr := NewDependencyResolver(allMods, potentialProviders, RunLoaderFabric)

	// Only nvidium is initially targeted; sodium should be pulled in as a
	// dependency even though it only matches one of the two predicates.
	target := sets.MakeSet([]string{"nvidium"})
	statuses := map[string]ModStatus{
		"nvidium": {ID: "nvidium", Mod: nvidium},
		"sodium":  {ID: "sodium", Mod: sodium},
	}

	result := dr.ResolveEffectiveSet(target, statuses)
	if len(result.Path) == 0 {
		t.Fatalf("expected resolution to succeed, got no resolution path")
	}
	if len(result.UnresolvableDeps) != 0 {
		t.Errorf("expected no unresolvable dependencies, got %v", result.UnresolvableDeps)
	}

	for _, id := range []string{"nvidium", "sodium"} {
		if _, ok := result.EffectiveSet[id]; !ok {
			t.Errorf("expected %q to be in the effective set, got %v", id, sets.MakeSlice(result.EffectiveSet))
		}
	}
	// The same setup must not be reported as directly unresolvable.
	available := sets.MakeSet([]string{"nvidium", "sodium"})
	unresolvable := dr.CalculateTransitivelyUnresolvableMods(available)
	if _, ok := unresolvable["nvidium"]; ok {
		t.Errorf("nvidium should be resolvable with sodium 0.8.11, but it is marked unresolvable: %v", sets.MakeSlice(unresolvable))
	}
}

// TestDependencyWithUnmatchedORPredicates verifies that an OR list still fails
// when the provider matches none of the predicates.
func TestDependencyWithUnmatchedORPredicates(t *testing.T) {
	nvidium := &Mod{
		BaseFilename: "nvidium.jar",
		Metadata: ModMetadata{
			ID:   "nvidium",
			Name: "Nvidium",
			Depends: VersionRanges{
				"sodium": {parsePred(t, "=0.8.11"), parsePred(t, "=0.8.12")},
			},
		},
	}
	sodium := &Mod{
		BaseFilename: "sodium.jar",
		Metadata: ModMetadata{
			ID:      "sodium",
			Name:    "Sodium",
			Version: VersionField{Version: mustParseVersion(t, "0.8.10")},
		},
	}

	allMods := map[string]*Mod{
		"nvidium": nvidium,
		"sodium":  sodium,
	}
	potentialProviders := PotentialProvidersMap{
		"sodium": {{
			TopLevelModID:         "sodium",
			VersionOfProvidedItem: mustParseVersion(t, "0.8.10"),
			IsDirectProvide:       true,
		}},
	}

	dr := NewDependencyResolver(allMods, potentialProviders, RunLoaderFabric)

	target := sets.MakeSet([]string{"nvidium"})
	statuses := map[string]ModStatus{
		"nvidium": {ID: "nvidium", Mod: nvidium},
		"sodium":  {ID: "sodium", Mod: sodium},
	}

	result := dr.ResolveEffectiveSet(target, statuses)
	if _, ok := result.EffectiveSet["nvidium"]; ok {
		t.Errorf("nvidium should NOT be resolvable with sodium 0.8.10, but it was activated")
	}
	if len(result.UnresolvableDeps) != 1 || result.UnresolvableDeps[0].DepID != "sodium" || result.UnresolvableDeps[0].RequiringModID != "nvidium" {
		t.Errorf("expected 'sodium' (required by 'nvidium') to be reported as unresolvable, got %v", result.UnresolvableDeps)
	}

	available := sets.MakeSet([]string{"nvidium", "sodium"})
	unresolvable := dr.CalculateTransitivelyUnresolvableMods(available)
	if _, ok := unresolvable["nvidium"]; !ok {
		t.Errorf("nvidium should be marked unresolvable with an unmatched dependency, got %v", sets.MakeSlice(unresolvable))
	}
}

func TestNestedModuleMissingDependencyMakesContainerUnresolvable(t *testing.T) {
	container := &Mod{
		Metadata: ModMetadata{ID: "container"},
		NestedModules: []NestedModule{{Info: ModMetadata{
			ID: "nested",
			Depends: VersionRanges{
				"missing": {parsePred(t, ">=1.0")},
			},
		}}},
	}

	dr := NewDependencyResolver(
		map[string]*Mod{"container": container},
		PotentialProvidersMap{},
		RunLoaderFabric,
	)
	result := dr.ResolveEffectiveSet(
		sets.MakeSet([]string{"container"}),
		map[string]ModStatus{"container": {ID: "container", Mod: container}},
	)

	if _, ok := result.EffectiveSet["container"]; ok {
		t.Fatalf("container with an unresolved nested dependency was activated: %v", result.EffectiveSet)
	}
	if len(result.UnresolvableDeps) != 1 ||
		result.UnresolvableDeps[0].DepID != "missing" ||
		result.UnresolvableDeps[0].RequiringModID != "nested" {
		t.Fatalf("expected nested dependency failure, got %v", result.UnresolvableDeps)
	}
}

func TestNestedModuleDependencyActivatesTopLevelProvider(t *testing.T) {
	container := &Mod{
		Metadata: ModMetadata{ID: "container"},
		NestedModules: []NestedModule{{Info: ModMetadata{
			ID: "nested",
			Depends: VersionRanges{
				"dependency": {parsePred(t, ">=1.0")},
			},
		}}},
	}
	dependency := &Mod{
		Metadata: ModMetadata{
			ID:      "dependency",
			Version: VersionField{Version: mustParseVersion(t, "1.0")},
		},
	}

	dr := NewDependencyResolver(
		map[string]*Mod{"container": container, "dependency": dependency},
		PotentialProvidersMap{
			"dependency": {{
				TopLevelModID:         "dependency",
				VersionOfProvidedItem: mustParseVersion(t, "1.0"),
				TopLevelModVersion:    mustParseVersion(t, "1.0"),
				IsDirectProvide:       true,
			}},
		},
		RunLoaderFabric,
	)
	result := dr.ResolveEffectiveSet(
		sets.MakeSet([]string{"container"}),
		map[string]ModStatus{
			"container":  {ID: "container", Mod: container},
			"dependency": {ID: "dependency", Mod: dependency},
		},
	)

	for _, id := range []string{"container", "dependency"} {
		if _, ok := result.EffectiveSet[id]; !ok {
			t.Errorf("expected %q in effective set, got %v", id, result.EffectiveSet)
		}
	}
	if _, ok := result.EffectiveSet["nested"]; ok {
		t.Error("nested module must not be added as a top-level effective-set entry")
	}
}
