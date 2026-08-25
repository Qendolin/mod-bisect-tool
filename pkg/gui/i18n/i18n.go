package i18n

import (
	"embed"
	"os"
	"strings"
	"sync"

	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/text/language"
)

//go:embed active.*.toml
var localeFiles embed.FS

var supportedLocales = []language.Tag{
	language.German,
	language.English,
	language.Spanish,
	language.French,
	language.Italian,
	language.Japanese,
	language.Korean,
	language.Polish,
	language.BrazilianPortuguese,
	language.Portuguese,
	language.Russian,
	language.Turkish,
	language.Ukrainian,
	language.TraditionalChinese,
	language.SimplifiedChinese,
}

// Translator is the GUI's locale-aware text provider. The fallback text is
// kept beside the call site so English remains available if a resource is
// incomplete or a locale is not supported yet.
type Translator struct {
	bundle    *goi18n.Bundle
	mu        sync.RWMutex
	localizer *goi18n.Localizer
	locale    language.Tag
}

func New(locale string) *Translator {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	entries, err := localeFiles.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			if _, err := bundle.LoadMessageFileFS(localeFiles, entry.Name()); err != nil {
				panic(err)
			}
		}
	}

	t := &Translator{bundle: bundle}
	t.SetLocale(locale)
	return t
}

// DetectLocale uses the conventional process locale variables. English is
// returned when the environment does not specify a usable locale.
func DetectLocale() string {
	if locale := platformLocale(); locale != "" {
		logging.Infof("i18n: Using locale from platform implementation: %v", locale)
		return locale
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		value := os.Getenv(key)
		if value == "" {
			continue
		}
		value = strings.Split(value, ":")[0]
		value = strings.Split(value, ".")[0]
		value = strings.ReplaceAll(value, "_", "-")
		if _, err := language.Parse(value); err == nil {
			logging.Infof("i18n: Using locale from %v: %v", key, value)
			return value
		} else {
			logging.Infof("i18n: Failed to parse locale from %v: %s", key, value)
		}
	}
	return language.English.String()
}

func SupportedLocales() []language.Tag {
	return append([]language.Tag(nil), supportedLocales...)
}

func (t *Translator) SetLocale(locale string) {
	requested := language.English
	if locale != "" {
		if parsed, err := language.Parse(locale); err == nil {
			requested = parsed
		}
	}
	logging.Infof("i18n: Set locale: %s", requested)
	t.mu.Lock()
	t.locale = requested
	t.localizer = goi18n.NewLocalizer(t.bundle, requested.String())
	t.mu.Unlock()
}

func (t *Translator) Locale() language.Tag {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.locale
}

func (t *Translator) Text(id, fallback string, data map[string]any) string {
	t.mu.RLock()
	localizer := t.localizer
	t.mu.RUnlock()
	return localize(localizer, id, fallback, data)
}

// TextIn resolves a message using a specific locale instead of the currently
// selected one. It is useful for locale pickers, whose option labels must not
// all be rendered in the current language.
func (t *Translator) TextIn(locale, id, fallback string, data map[string]any) string {
	return localize(goi18n.NewLocalizer(t.bundle, locale), id, fallback, data)
}

func localize(localizer *goi18n.Localizer, id, fallback string, data map[string]any) string {
	text, err := localizer.Localize(&goi18n.LocalizeConfig{
		MessageID: id,
		DefaultMessage: &goi18n.Message{
			ID:    id,
			Other: fallback,
		},
		TemplateData: data,
	})
	if err != nil {
		return fallback
	}
	return text
}

func (t *Translator) Plural(id, one, other string, count int, data map[string]any) string {
	t.mu.RLock()
	localizer := t.localizer
	t.mu.RUnlock()
	text, err := localizer.Localize(&goi18n.LocalizeConfig{
		MessageID: id,
		DefaultMessage: &goi18n.Message{
			ID:    id,
			One:   one,
			Other: other,
		},
		TemplateData: data,
		PluralCount:  count,
	})
	if err != nil {
		return other
	}
	return text
}
