package i18n

import (
	"regexp"
	"strings"
	"testing"
)

func TestResolveUsesExplicitLanguageBeforeLocale(t *testing.T) {
	translator, err := Resolve("zh", map[string]string{"LANG": "en_US.UTF-8"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := translator.Language(), Chinese; got != want {
		t.Fatalf("Language() = %q, want %q", got, want)
	}
	if got, want := translator.Text(Ready), "就绪"; got != want {
		t.Fatalf("Text(Ready) = %q, want %q", got, want)
	}
}

func TestResolveDetectsChineseLocale(t *testing.T) {
	translator, err := Resolve("auto", map[string]string{"LC_MESSAGES": "zh_CN.UTF-8"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := translator.Language(), Chinese; got != want {
		t.Fatalf("Language() = %q, want %q", got, want)
	}
}

func TestResolveFallsBackToEnglish(t *testing.T) {
	translator, err := Resolve("", map[string]string{"LANG": "C"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := translator.Text(Ready), "Ready"; got != want {
		t.Fatalf("Text(Ready) = %q, want %q", got, want)
	}
}

func TestEveryEnglishMessageHasChineseTranslation(t *testing.T) {
	for key := range messages[English] {
		if messages[Chinese][key] == "" {
			t.Errorf("missing Chinese translation for %s", key)
		}
	}
}

// TestNoPOSIXPositionalVerbs guards the whole message catalog against the C/POSIX
// `%2$d` positional syntax, which Go's fmt does not support (it needs `%[2]d`)
// and silently renders as garbage.
func TestNoPOSIXPositionalVerbs(t *testing.T) {
	posix := regexp.MustCompile(`%\d+\$`)
	for language, table := range messages {
		for key, value := range table {
			if posix.MatchString(value) {
				t.Errorf("%s/%s uses POSIX positional verbs (use %%[n]d): %q", language, key, value)
			}
		}
	}
}

// TestAllClientToggleStringsRender exercises the (name, count) all-client toggle
// messages in both languages so a broken positional verb is caught by output,
// not just by the non-empty parity check.
func TestAllClientToggleStringsRender(t *testing.T) {
	keys := []Key{
		EnabledSkillAllClients, DisabledSkillAllClients,
		EnabledSourceAllClients, DisabledSourceAllClients,
		EnabledMCPAllClients, DisabledMCPAllClients,
	}
	for _, language := range []Language{English, Chinese} {
		translator := New(language)
		for _, key := range keys {
			got := translator.Text(key, "context7", 3)
			if strings.ContainsAny(got, "%$[]") {
				t.Errorf("%s/%s rendered with format leftovers: %q", language, key, got)
			}
			if !strings.Contains(got, "context7") || !strings.Contains(got, "3") {
				t.Errorf("%s/%s dropped an argument: %q", language, key, got)
			}
		}
	}
}
