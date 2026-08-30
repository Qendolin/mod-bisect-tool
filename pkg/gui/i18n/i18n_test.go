package i18n

import (
	"strings"
	"testing"
)

func TestTranslatorUsesEmbeddedEnglishAndFallback(t *testing.T) {
	tr := New("en")
	if got := tr.Text("locale_name", "fallback", nil); got != "English (en)" {
		t.Fatalf("embedded English message = %q, want %q", got, "English (en)")
	}
	if got := tr.Text("missing", "fallback", nil); got != "fallback" {
		t.Fatalf("missing message = %q, want %q", got, "fallback")
	}
}

func TestTranslatorPluralFallback(t *testing.T) {
	tr := New("en")
	if got := tr.Plural("items", "{{.Count}} item", "{{.Count}} items", 1, map[string]any{"Count": 1}); got != "1 item" {
		t.Fatalf("singular message = %q, want %q", got, "1 item")
	}
	if got := tr.Plural("items", "{{.Count}} item", "{{.Count}} items", 2, map[string]any{"Count": 2}); got != "2 items" {
		t.Fatalf("plural message = %q, want %q", got, "2 items")
	}
}

func TestGermanTranslations(t *testing.T) {
	tr := New("de")
	if got := tr.TextIn("en", "locale_name", "en", nil); got != "English (en)" {
		t.Fatalf("German locale label for English = %q, want %q", got, "English (en)")
	}
	if got := tr.Text("locale_name", "fallback", nil); got != "Deutsch (de)" {
		t.Fatalf("German locale name = %q, want %q", got, "Deutsch (de)")
	}
	if got := tr.Text("start_bisection", "fallback", nil); got != "Bisektion starten" {
		t.Fatalf("German start button = %q, want %q", got, "Bisektion starten")
	}
	if got := tr.Text("app_name", "fallback", nil); got != "Mod Bisect Tool" {
		t.Fatalf("German app name = %q, want %q", got, "Mod Bisect Tool")
	}
	if got := tr.Text("setup_description", "fallback", nil); got != "Wähle deinen Mod-Ordner aus, um zu beginnen." {
		t.Fatalf("German setup description = %q, want informal form", got)
	}
	description := tr.Text("setup_required_mods_description", "fallback", nil)
	if strings.Contains(description, "wähle ihn") || strings.Contains(description, "So bleibt er") {
		t.Fatalf("German mod references use masculine pronouns: %q", description)
	}
	if got := tr.Text("version", "fallback", map[string]any{"Version": "1.2.3"}); got != "Version: 1.2.3" {
		t.Fatalf("German version = %q, want %q", got, "Version: 1.2.3")
	}
}
