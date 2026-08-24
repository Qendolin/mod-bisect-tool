package version_test

import (
	"reflect"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
)

func TestTranslateMavenVersion(t *testing.T) {
	v, err := version.TranslateMavenVersion("1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "1.2.3" {
		t.Fatalf("expected 1.2.3, got %s", v)
	}

	_, err = version.TranslateMavenVersion("[1.0,2.0]")
	if err == nil {
		t.Fatalf("expected error for range input, got nil")
	}
}

func TestTranslateMavenVersionQualifiers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "reported loader qualifier", input: "2.3.1-NEOFORGE-1.21.1", expected: "2.3.1.0.1-neoforge.1.21.1"},
		{name: "unknown post release qualifier", input: "2.3.1-custom-1", expected: "2.3.1.0.1-custom.1"},
		{name: "service pack qualifier", input: "2.3.1-sp", expected: "2.3.1.0.1-sp"},
		{name: "release qualifier", input: "2.3.1-GA", expected: "2.3.1"},
		{name: "alpha prerelease", input: "2.3.1-alpha-1", expected: "2.3.1-alpha.1"},
		{name: "beta prerelease", input: "2.3.1-beta-1", expected: "2.3.1-beta.1"},
		{name: "release candidate", input: "2.3.1-rc-1", expected: "2.3.1-rc.1"},
		{name: "snapshot prerelease", input: "2.3.1-SNAPSHOT", expected: "2.3.1-snapshot"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			translated, err := version.TranslateMavenVersion(test.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if translated != test.expected {
				t.Fatalf("expected %s, got %s", test.expected, translated)
			}
		})
	}
}

func TestTranslateMavenVersionPostReleaseOrdering(t *testing.T) {
	for _, input := range []string{"2.3.1-NEOFORGE-1.21.1", "2.3.1-custom-1", "2.3.1-sp"} {
		translated, err := version.TranslateMavenVersion(input)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", input, err)
		}

		postRelease, err := version.Parse(translated, false)
		if err != nil {
			t.Fatalf("unexpected error parsing translated version %q: %v", translated, err)
		}
		for _, test := range []struct {
			predicate string
			expected  bool
		}{
			{">=2.3.1", true},
			{">2.3.1", true},
			{"<2.3.2", true},
			{"=2.3.1", false},
		} {
			predicate, err := version.ParseVersionPredicate(test.predicate)
			if err != nil {
				t.Fatalf("unexpected error parsing predicate %q: %v", test.predicate, err)
			}
			if got := predicate.Test(postRelease); got != test.expected {
				t.Errorf("predicate %q for %q: expected %v, got %v", test.predicate, input, test.expected, got)
			}
		}
	}
}

func TestMavenVersionAliasContext(t *testing.T) {
	bound, err := version.TranslateMavenVersion("1-foo")
	if err != nil {
		t.Fatalf("unexpected error translating bound: %v", err)
	}
	less, err := version.ParseVersionPredicate("<" + bound)
	if err != nil {
		t.Fatalf("unexpected error parsing predicate: %v", err)
	}
	for _, input := range []string{"1.0.0-ga-foo", "1-ga-foo"} {
		translated, err := version.TranslateMavenVersion(input)
		if err != nil {
			t.Fatalf("unexpected error translating %q: %v", input, err)
		}
		candidate, err := version.Parse(translated, false)
		if err != nil {
			t.Fatalf("unexpected error parsing %q: %v", translated, err)
		}
		if !less.Test(candidate) {
			t.Errorf("Maven version %q should be less than 1-foo after alias canonicalization", input)
		}
	}
}

func TestMavenPlusQualifiedRangesProduceValidPredicates(t *testing.T) {
	for _, input := range []string{
		"[1.0.0-beta.1+1.21.1,)",
		"[1.0.0-alpha.13+1.21.1,)",
	} {
		translated, err := version.TranslateMavenVersionRange(input)
		if err != nil {
			t.Fatalf("unexpected error translating %q: %v", input, err)
		}
		for _, predicateString := range translated {
			if _, err := version.ParseVersionPredicate(predicateString); err != nil {
				t.Errorf("translated predicate %q from %q is invalid: %v", predicateString, input, err)
			}
		}
	}
}

func TestMavenVersionRangesAgainstVersions(t *testing.T) {
	tests := []struct {
		name   string
		range_ string
		match  []string
		miss   []string
	}{
		{
			name:   "exact release aliases and trailing nulls",
			range_: "[1.0]",
			match:  []string{"1", "1.0", "1.ga", "1-final", "1-0"},
			miss:   []string{"1.0.1", "1-sp"},
		},
		{
			name:   "upper inclusive",
			range_: "(,1.0]",
			match:  []string{"0.9", "1-snapshot", "1.0"},
			miss:   []string{"1-sp", "1.1"},
		},
		{
			name:   "lower exclusive",
			range_: "(1.0,)",
			match:  []string{"1.0.1", "1-sp", "1.0.1-alpha", "2.0"},
			miss:   []string{"1-snapshot", "1.0"},
		},
		{
			name:   "bounded range includes prerelease of upper bound",
			range_: "[1.0,2.0)",
			match:  []string{"1.0", "1.0.1", "2.0-rc1", "2.0-alpha"},
			miss:   []string{"0.9", "1.0-SNAPSHOT", "2.0", "2.0.1"},
		},
		{
			name:   "qualifier tokenization and case",
			range_: "[1.foo,1-1)",
			match:  []string{"1.foo", "1-foo", "1.FOO", "1.foo.0.0", "1-foo2"},
			miss:   []string{"1", "1-1", "1.1"},
		},
		{
			name:   "qualifier numeric token ordering",
			range_: "[1-foo2,1-foo10)",
			match:  []string{"1-foo2", "1-foo9"},
			miss:   []string{"1-foo1", "1-foo10", "1-foo11"},
		},
		{
			name:   "post release before next numeric prerelease",
			range_: "[1.0.0-foo,1.0.0.1-alpha)",
			match:  []string{"1.0.0-foo", "1.0.0-foo.1"},
			miss:   []string{"1.0.0", "1.0.0.1-alpha", "1.0.0.1"},
		},
		{
			name:   "release alias trimming before suffix",
			range_: "[1-sp-1,1-ga-1)",
			match:  []string{"1-sp-1"},
			miss:   []string{"1-ga-1", "1-sp", "1-ga"},
		},
		{
			name:   "soft requirement",
			range_: "1.0",
			match:  []string{"0.1", "1.0", "2.0-sp"},
			miss:   nil,
		},
		{
			name:   "disjoint ranges",
			range_: "(,1.0],[2.0,)",
			match:  []string{"0.9", "1.0", "2.0", "2.0-sp"},
			miss:   []string{"1.0.1", "1.9.9"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			predicateStrings, err := version.TranslateMavenVersionRange(test.range_)
			if err != nil {
				t.Fatalf("unexpected range translation error: %v", err)
			}

			predicates := make([]*version.VersionPredicate, 0, len(predicateStrings))
			for _, predicateString := range predicateStrings {
				predicate, err := version.ParseVersionPredicate(predicateString)
				if err != nil {
					t.Fatalf("unexpected predicate parse error for %q: %v", predicateString, err)
				}
				predicates = append(predicates, predicate)
			}

			for _, testVersion := range append(test.match, test.miss...) {
				translated, err := version.TranslateMavenVersion(testVersion)
				if err != nil {
					t.Fatalf("unexpected version translation error for %q: %v", testVersion, err)
				}
				parsed, err := version.Parse(translated, false)
				if err != nil {
					t.Fatalf("unexpected version parse error for %q: %v", testVersion, err)
				}

				matched := false
				for _, predicate := range predicates {
					if predicate.Test(parsed) {
						matched = true
						break
					}
				}
				want := contains(test.match, testVersion)
				if matched != want {
					t.Errorf("range %q and version %q: expected match=%v, got %v", test.range_, testVersion, want, matched)
				}
			}
		})
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func TestTranslateMavenVersionRange(t *testing.T) {
	cases := map[string][]string{
		"1.0":           {"*"},
		"(,1.0]":        {"<=1.0"},
		"(,1.0)":        {"<1.0"},
		"[1.0]":         {"=1.0"},
		"[1.0,)":        {">=1.0"},
		"(1.0,)":        {">1.0"},
		"(1.0,2.0)":     {">1.0 <2.0"},
		"[1.0,2.0]":     {">=1.0 <=2.0"},
		"(,1.0],[1.2,)": {"<=1.0", ">=1.2"},
		"*":             {"*"},
		"[*,)":          {"*"},
		"[*,2.0]":       {"<=2.0"},
		"(1.0,*)":       {">1.0"},
	}

	for input, expected := range cases {
		res, err := version.TranslateMavenVersionRange(input)
		if err != nil {
			t.Errorf("unexpected error for %s: %v", input, err)
			continue
		}
		if !reflect.DeepEqual(res, expected) {
			t.Errorf("for %s expected %v, got %v", input, expected, res)
		}
	}
}

func TestTranslateMavenVersionRangeRejectsMalformedIntervals(t *testing.T) {
	for _, input := range []string{"[1.0", "(1.0", "[1.0,2.0,3.0]", "(1.0,2.0,3.0)"} {
		if _, err := version.TranslateMavenVersionRange(input); err == nil {
			t.Errorf("expected malformed range %q to fail", input)
		}
	}
}

// Additional validation using Parse and ParseVersionPredicate
func TestParseTranslatedResults(t *testing.T) {
	_, err := version.Parse("1.2.3", false)
	if err != nil {
		t.Fatalf("unexpected error parsing version: %v", err)
	}

	pred, err := version.ParseVersionPredicate(">=1.0 <2.0")
	if err != nil {
		t.Fatalf("unexpected error parsing predicate: %v", err)
	}
	if pred == nil {
		t.Fatalf("expected non-nil predicate")
	}
}
