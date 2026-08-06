// Package i18n resolves the interface's user-facing strings against
// pkg/config's Language: "fr" or "en", the two lazyshell ships with.
package i18n

import "fmt"

// Catalog resolves message keys to one language's text.
type Catalog struct {
	lang string
}

// New returns a Catalog for lang. Anything other than "fr" behaves as "en" —
// the same fallback pkg/config's own validation applies to an unrecognised
// Language value, so the two never disagree about what an unknown language
// degrades to.
func New(lang string) *Catalog {
	if lang != "fr" {
		lang = "en"
	}

	return &Catalog{lang: lang}
}

// T resolves key in the catalog's language, formatting it with args when any
// are given (fmt.Sprintf verbs, same convention as the error messages this
// replaces). A nil Catalog resolves as "fr" — every existing test and every
// bare Gui{} literal that predates this package built no Catalog at all, and
// they all assert on French text.
//
// A key missing its own language falls back to "en", and one missing
// entirely falls back to the key itself rather than panicking — T is called
// from render paths that must never crash the interface over a typo in a
// message table; TestMessagesAreComplete is what actually catches that typo.
func (c *Catalog) T(key string, args ...any) string {
	lang := "fr"
	if c != nil {
		lang = c.lang
	}

	tmpl, ok := messages[key][lang]
	if !ok {
		tmpl, ok = messages[key]["en"]
	}
	if !ok {
		tmpl = key
	}

	if len(args) == 0 {
		return tmpl
	}

	return fmt.Sprintf(tmpl, args...)
}
