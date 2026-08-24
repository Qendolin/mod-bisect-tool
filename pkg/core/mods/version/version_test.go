package version_test

import (
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
)

func TestVersionParsing(t *testing.T) {
	tests := []struct {
		input    string
		storeX   bool
		expected string
		err      bool
	}{
		// Test: Semantic version creation.
		{"0.3.5", false, "0.3.5", false},
		{"0.3.5-beta.2", false, "0.3.5-beta.2", false},
		{"0.3.5-alpha.6+build.120", false, "0.3.5-alpha.6+build.120", false},
		{"0.3.5+build.3000", false, "0.3.5+build.3000", false},
		{"0.0.-1", false, "", true},
		{"0.-1.0", false, "", true},
		{"-1.0.0", false, "", true},
		{"", false, "", true},
		{"0.0.a", false, "", true},
		{"0.a.0", false, "", true},
		{"a.0.0", false, "", true},
		{"x", true, "", true},
		{"2.x", true, "2.x", false},
		{"2.x.x", true, "2.x", false},
		{"2.X", true, "2.x", false},
		{"2.*", true, "2.x", false},
		{"2.x.1", true, "", true},
		{"2.*.1", true, "", true},
		{"2.x-alpha.1", true, "", true},
		{"2.*-alpha.1", true, "", true},
		{"*-alpha.1", true, "", true},
		{"2.x", false, "", true},
		{"2.X", false, "", true},
		{"2.*", false, "", true},

		// Test: Semantic version creation (spec).
		{"1.0.0-0.3.7", false, "1.0.0-0.3.7", false},
		{"1.0.0-x.7.z.92", false, "1.0.0-x.7.z.92", false},
		{"1.0.0+20130313144700", false, "1.0.0+20130313144700", false},
		{"1.0.0-beta+exp.sha.5114f85", false, "1.0.0-beta+exp.sha.5114f85", false},
	}

	for _, test := range tests {
		v, err := version.ParseSemantic(test.input, test.storeX)
		if test.err {
			if err == nil {
				t.Errorf("Expected error for input %q (storeX=%v) but got none", test.input, test.storeX)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for input %q (storeX=%v): %v", test.input, test.storeX, err)
			continue
		}
		if v.String() != test.expected {
			t.Errorf("For input %q (storeX=%v), expected %q but got %q", test.input, test.storeX, test.expected, v.String())
		}
	}
}

func TestVersionPredicates(t *testing.T) {
	tests := []struct {
		predicate string
		version   string
		expected  bool
	}{
		// Test: comparator range with pre-releases.
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.2", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.2.1", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.3", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.4+build.125", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.7", true},
		{">=0.3.1-beta.2 <0.4.0", "0.4.0-alpha.1", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.4-beta.7", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.11", true},
		{">=0.3.1-beta.2 <0.4.0", "0.3.0", false},
		{">=0.3.1-beta.2 <0.4.0", "0.3.1-beta.1", false},
		{">=0.3.1-beta.2 <0.4.0", "0.4.0", false},
		//
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.2", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.2.1", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.3", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.4+build.125", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.7", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.4-beta.7", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.11", true},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.0", false},
		{">=0.3.1-beta.2 <0.4.0-", "0.3.1-beta.1", false},
		{">=0.3.1-beta.2 <0.4.0-", "0.4.0-alpha.1", false},
		{">=0.3.1-beta.2 <0.4.0-", "0.4.0", false},
		//
		{">=1.4-", "1.4-beta.2", true},
		{">=1.4-", "1.4+build.125", true},
		{">=1.4-", "1.4", true},
		{">=1.4-", "1.4.2", true},
		{">=1.4-", "1.3", false},
		{">=1.4-", "1.3.5", false},
		{">=1.4-", "1.3-alpha.1", false},
		//
		{"<1.4", "1.3", true},
		{"<1.4", "1.3.5", true},
		{"<1.4", "1.3-alpha.1", true},
		{"<1.4", "1.4-beta.2", true},
		{"<1.4", "1.4+build.125", false},
		{"<1.4", "1.4", false},
		//
		{"<1.4-", "1.3", true},
		{"<1.4-", "1.3.5", true},
		{"<1.4-", "1.3-alpha.1", true},
		{"<1.4-", "1.4-beta.2", false},
		{"<1.4-", "1.4+build.125", false},
		{"<1.4-", "1.4", false},

		// Test: pre-release parts
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.9", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.11", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.e", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.d.10", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.9.d.5", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.final", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.-final-", true},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.7", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.d", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.a", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-alpha.9", false},
		{">=0.3.1-beta.8.d.10", "0.3.1-beta.8.8", false},

		// Test: x-range. "a.b.x" = ">=a.b.0- <a.(b+1).0-" (same major+minor, pre allowed)
		{"1.3.x", "1.3.0", true},
		{"1.3.x", "1.3.0-alpha.1", true},
		{"1.3.x", "1.3.99", true},
		{"1.3.x", "1.4.0", false},
		{"1.3.x", "1.2.9", false},
		{"1.3.x", "1.2.9-rc.6", false},
		{"1.3.x", "1.4.0-alpha.1", false},
		{"1.3.x", "2.0.0", false},

		// Test: smaller x-range. "a.x" = ">=a.0.0- <(a+1).0.0-" (same major, pre allowed)
		{"2.x", "2.0.0", true},
		{"2.x", "2.0.0-alpha.1", true},
		{"2.x", "2.9.0-beta.2", true},
		{"2.x", "2.2.4", true},
		{"2.x", "1.99.99", false},
		{"2.x", "3.0.0", false},
		{"2.x", "3.0.0-alpha.1", false},

		// Test: tilde-ranges. "~a" = ">=a <(a[0]).(a[1]+1).0-" (at least a, same major+minor)
		{"~1.2.3", "1.2.3", true},
		{"~1.2.3", "1.2.4", true},
		{"~1.2.3", "1.2.4-alpha.1", true},
		{"~1.2.3", "1.2.2", false},
		{"~1.2.3", "1.2.3-rc.7", false},
		{"~1.2.3", "1.3.0", false},
		{"~1.2.3", "2.2.0", false},
		//
		{"~1.2", "1.2.0", true},
		{"~1.2", "1.2.1-alpha.3", true},
		{"~1.2", "1.2.6", true},
		{"~1.2", "1.1.9", false},
		{"~1.2", "1.3.0", false},
		{"~1.2", "1.2.0-rc.2", false},
		{"~1.2", "1.3.0-alpha.3", false},
		//
		{"~1.2-", "1.2.0", true},
		{"~1.2-", "1.2.1-alpha.3", true},
		{"~1.2-", "1.2.6", true},
		{"~1.2-", "1.2.0-rc.2", true},
		{"~1.2-", "1.1.9", false},
		{"~1.2-", "1.3.0", false},
		{"~1.2-", "1.3.0-alpha.3", false},
		//
		{"~1", "1.0.0", true},
		{"~1", "1.0.4", true},
		{"~1", "0.9.9", false},
		{"~1", "1.1.5", false},
		{"~1", "3.0.5", false},
		//
		{"~1.2.3-beta.2", "1.2.3-beta.2", true},
		{"~1.2.3-beta.2", "1.2.3-beta.2.1", true},
		{"~1.2.3-beta.2", "1.2.3-beta.3", true},
		{"~1.2.3-beta.2", "1.2.3-beta.11", true},
		{"~1.2.3-beta.2", "1.2.3-rc.7", true},
		{"~1.2.3-beta.2", "1.2.3", true},
		{"~1.2.3-beta.2", "1.2.5", true},
		{"~1.2.3-beta.2", "1.2.4-alpha.4", true},
		{"~1.2.3-beta.2", "1.3.0", false},
		{"~1.2.3-beta.2", "1.2.2", false},
		{"~1.2.3-beta.2", "1.2.3-beta.1", false},
		{"~1.2.3-beta.2", "1.2.3-beta.1.9", false},
		{"~1.2.3-beta.2", "1.2.3-alpha.4", false},

		// Test: caret-range. "^a" = ">=a <(a[0]+1).0.0-" (at least a, same major)
		{"^1.2.3", "1.2.3", true},
		{"^1.2.3", "1.2.4", true},
		{"^1.2.3", "1.3.0", true},
		{"^1.2.3", "1.2.4-beta.2", true},
		{"^1.2.3", "1.2.2", false},
		{"^1.2.3", "1.2.3-beta.2", false},
		{"^1.2.3", "2.0.0", false},
		//
		{"^0.2.3", "0.2.3", true},
		{"^0.2.3", "0.2.4", true},
		{"^0.2.3", "0.2.8-beta.2", true},
		{"^0.2.3", "0.3.0", true},
		{"^0.2.3", "0.2.0", false},
		{"^0.2.3", "0.2.3-rc.8", false},
		{"^0.2.3", "1.2.0", false},
		//
		{"^1.2.3-beta.2", "1.2.3-beta.2", true},
		{"^1.2.3-beta.2", "1.2.3-beta.3", true},
		{"^1.2.3-beta.2", "1.2.3-rc.7", true},
		{"^1.2.3-beta.2", "1.2.3", true},
		{"^1.2.3-beta.2", "1.2.5", true},
		{"^1.2.3-beta.2", "1.3.0", true},
		{"^1.2.3-beta.2", "1.2.4-alpha.4", true},
		{"^1.2.3-beta.2", "1.2.2", false},
		{"^1.2.3-beta.2", "2.0.0", false},
		{"^1.2.3-beta.2", "1.2.3-alpha.4", false},
		//
		{"^1", "1.0.0", true},
		{"^1", "1.2.4", true},
		{"^1", "1.99.99", true},
		{"^1", "1.2.4-beta.2", true},
		{"^1", "0.9.6", false},
		{"^1", "1.0.0-rc.5", false},
		{"^1", "2.0.0", false},
		{"^1", "2.0.0-beta.2", false},
		//
		{"^1-", "1.0.0", true},
		{"^1-", "1.0.0-rc.5", true},
		{"^1-", "1.2.4", true},
		{"^1-", "1.99.99", true},
		{"^1-", "1.2.4-beta.2", true},
		{"^1-", "0.9.0", false},
		{"^1-", "0.9.0-rc.5", false},
		{"^1-", "2.0.0", false},
		{"^1-", "2.0.0-beta.2", false},
	}

	for _, test := range tests {
		pred, err := version.ParseVersionPredicate(test.predicate)
		if err != nil {
			t.Errorf("Error parsing predicate %q: %v", test.predicate, err)
			continue
		}

		v, err := version.Parse(test.version, false)
		if err != nil {
			t.Errorf("Error parsing version %q: %v", test.version, err)
			continue
		}

		result := pred.Test(v)
		if result != test.expected {
			t.Errorf("Predicate %q test for version %q: expected %v, got %v", test.predicate, test.version, test.expected, result)
		}
	}
}

func TestExtendedCoverage(t *testing.T) {
	t.Run("VersionInterval Stringer", func(t *testing.T) {
		v1, _ := version.ParseSemantic("1.0.0", false)
		v2, _ := version.ParseSemantic("2.0.0-rc1", false)

		tests := []struct {
			name     string
			interval version.VersionInterval
			expected string
		}{
			{"Infinite", version.VersionInterval{}, "(-∞,∞)"},
			{"Fully Closed", version.VersionInterval{Min: v1, MinInclusive: true, Max: v2, MaxInclusive: true}, "[1.0.0,2.0.0-rc1]"},
			{"Fully Open", version.VersionInterval{Min: v1, MinInclusive: false, Max: v2, MaxInclusive: false}, "(1.0.0,2.0.0-rc1)"},
			{"Half-Open Min", version.VersionInterval{Min: v1, MinInclusive: true}, "[1.0.0,∞)"},
			{"Half-Open Max", version.VersionInterval{Max: v2, MaxInclusive: false}, "(-∞,2.0.0-rc1)"},
		}

		for _, tt := range tests {
			if got := tt.interval.String(); got != tt.expected {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.expected)
			}
		}
	})

	t.Run("VersionPredicate Stringer", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"*", "*"},
			{"1.2.3", "1.2.3"},
			{">=1.2.3", ">=1.2.3"},
			{">=1.0.0 <2.0.0", ">=1.0.0 <2.0.0"},
			{"~1.2.3", "~1.2.3"},
		}

		for _, tt := range tests {
			p, err := version.ParseVersionPredicate(tt.input)
			if err != nil {
				t.Fatalf("Failed to parse predicate %q: %v", tt.input, err)
			}
			if got := p.String(); got != tt.expected {
				t.Errorf("For input %q, got string %q, want %q", tt.input, got, tt.expected)
			}
		}
	})

	t.Run("VersionPredicate Interval Calculation", func(t *testing.T) {
		tests := []struct {
			predicate string
			expected  string
		}{
			{"^1.2.3", "[1.2.3,2.0.0-)"},
			{"~1.2", "[1.2.0,1.3.0-)"},
			{"1.x", "[1.0.0-,2.0.0-)"},
			{"2.4.x", "[2.4.0-,2.5.0-)"},
			{">=1.0.0 <2.0.0", "[1.0.0,2.0.0)"},
			{"*", "(-∞,∞)"},
			{">1.0 <1.0", "(empty)"}, // Test an impossible interval
		}

		for _, tt := range tests {
			p, err := version.ParseVersionPredicate(tt.predicate)
			if err != nil {
				t.Fatalf("Failed to parse predicate %q: %v", tt.predicate, err)
			}
			interval := p.Interval()
			var got string
			if interval == nil {
				got = "(empty)"
			} else {
				got = interval.String()
			}
			if got != tt.expected {
				t.Errorf("For predicate %q, got interval %q, want %q", tt.predicate, got, tt.expected)
			}
		}
	})

	t.Run("Non-Semantic Version Handling", func(t *testing.T) {
		p, err := version.ParseVersionPredicate("latest")
		if err != nil {
			t.Fatalf("Failed to parse 'latest': %v", err)
		}

		vLatest, _ := version.Parse("latest", false)
		vStable, _ := version.Parse("stable", false)

		if !p.Test(vLatest) {
			t.Error("Predicate 'latest' should match version 'latest'")
		}
		if p.Test(vStable) {
			t.Error("Predicate 'latest' should not match version 'stable'")
		}

		// Ensure non-equality operators fail for non-semantic versions
		if _, err := version.ParseVersionPredicate(">latest"); err == nil {
			t.Error("Expected error when parsing '>latest', but got nil")
		}
	})
}

func TestMultiComponentVersions(t *testing.T) {
	t.Run("Direct Comparison", func(t *testing.T) {
		v1, _ := version.ParseSemantic("1.2.3.4", false)
		v2, _ := version.ParseSemantic("1.2.3.5", false)
		v3, _ := version.ParseSemantic("1.2.3", false)

		if v1.Compare(v2) != -1 {
			t.Errorf("Expected %s to be less than %s", v1, v2)
		}
		if v2.Compare(v1) != 1 {
			t.Errorf("Expected %s to be greater than %s", v2, v1)
		}
		if v1.Compare(v3) != 1 {
			t.Errorf("Expected %s to be greater than %s (padded with .0)", v1, v3)
		}
	})

	t.Run("Range Testing", func(t *testing.T) {
		tests := []struct {
			predicate string
			version   string
			expected  bool
		}{
			// Tilde-range on 4-component version
			{"~1.2.3.4", "1.2.3.4", true},
			{"~1.2.3.4", "1.2.3.5", true},
			{"~1.2.3.4", "1.2.99.99", true},
			{"~1.2.3.4", "1.3.0.0", false},
			{"~1.2.3.4", "1.2.3.3", false},
			// Caret-range on 4-component version
			{"^1.2.3.4", "1.2.3.4", true},
			{"^1.2.3.4", "1.3.0.0", true},
			{"^1.2.3.4", "1.99.99.99", true},
			{"^1.2.3.4", "2.0.0.0", false},
			{"^1.2.3.4", "1.2.3.3", false},
			// X-range with 3+ components
			{"1.2.3.x", "1.2.3.0-alpha", true},
			{"1.2.3.x", "1.2.3.5", true},
			// CORRECTED TEST CASE: 1.2.4.0 is < 1.3.0, so it should match the tilde range.
			{"1.2.3.x", "1.2.4.0", true},
			// This case ensures the upper bound of the tilde range is working correctly.
			{"1.2.3.x", "1.3.0.0", false},
		}

		for _, tt := range tests {
			p, err := version.ParseVersionPredicate(tt.predicate)
			if err != nil {
				t.Errorf("Error parsing predicate %q: %v", tt.predicate, err)
				continue
			}

			v, err := version.Parse(tt.version, false)
			if err != nil {
				t.Errorf("Error parsing version %q: %v", tt.version, err)
				continue
			}

			if result := p.Test(v); result != tt.expected {
				t.Errorf("Predicate %q test for version %q: expected %v, got %v", tt.predicate, tt.version, tt.expected, result)
			}
		}
	})
}
