package i18n

import "testing"

// Every key must carry both languages Config.Languages accepts — a message
// added for "fr" only would silently fall back to itself (the raw key) under
// "en" instead of failing loudly, which T's own fallback chain is deliberately
// forgiving about at render time. This is what catches the mistake instead.
func TestMessagesAreComplete(t *testing.T) {
	for key, byLang := range messages {
		for _, lang := range []string{"fr", "en"} {
			if text, ok := byLang[lang]; !ok || text == "" {
				t.Errorf("message %q has no %s translation", key, lang)
			}
		}

		for lang := range byLang {
			if lang != "fr" && lang != "en" {
				t.Errorf("message %q has an unexpected language %q", key, lang)
			}
		}
	}
}

func TestTResolvesLanguage(t *testing.T) {
	fr := New("fr")
	en := New("en")

	if got := fr.T("action.quit"); got != "Quitter lazyshell" {
		t.Errorf("fr T(action.quit) = %q, want %q", got, "Quitter lazyshell")
	}
	if got := en.T("action.quit"); got != "Quit lazyshell" {
		t.Errorf("en T(action.quit) = %q, want %q", got, "Quit lazyshell")
	}
}

func TestNewFallsBackToEnglishForUnknownLanguage(t *testing.T) {
	c := New("es")

	if got := c.T("action.quit"); got != "Quit lazyshell" {
		t.Errorf("New(\"es\") T(action.quit) = %q, want the English fallback", got)
	}
}

func TestNilCatalogFallsBackToFrench(t *testing.T) {
	var c *Catalog

	if got := c.T("action.quit"); got != "Quitter lazyshell" {
		t.Errorf("nil Catalog T(action.quit) = %q, want the French default", got)
	}
}

func TestTFormatsArgs(t *testing.T) {
	c := New("en")

	if got := c.T("action.jump", 3); got != "Jump directly to session 3" {
		t.Errorf("T(action.jump, 3) = %q, want %q", got, "Jump directly to session 3")
	}
}

func TestTFallsBackToKeyForUnknownMessage(t *testing.T) {
	c := New("en")

	if got := c.T("no.such.key"); got != "no.such.key" {
		t.Errorf("T(no.such.key) = %q, want the key itself", got)
	}
}
