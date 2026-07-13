package i18n

import "testing"

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
