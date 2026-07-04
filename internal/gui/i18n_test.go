package gui

import (
	"regexp"
	"strings"
	"testing"
)

// i18nKeys extracts the translation keys of one language block ("en" or "es") from
// the embedded SPA source. Keys sit at four-space indent as `key: "..."`.
func i18nKeys(t *testing.T, lang string) map[string]bool {
	t.Helper()
	src := string(appJS)
	start := strings.Index(src, "  "+lang+": {")
	if start < 0 {
		t.Fatalf("no %q block in app.js", lang)
	}
	end := strings.Index(src[start:], "\n  },")
	if end < 0 {
		t.Fatalf("unterminated %q block in app.js", lang)
	}
	block := src[start : start+end]

	keyRe := regexp.MustCompile(`(?m)^    ([A-Za-z0-9_]+):`)
	keys := map[string]bool{}
	for _, m := range keyRe.FindAllStringSubmatch(block, -1) {
		keys[m[1]] = true
	}
	return keys
}

// Every UI string must exist in BOTH languages — a key added to one block only
// renders as raw key text for half the users, and nothing else catches that.
func TestI18NKeysParity(t *testing.T) {
	en := i18nKeys(t, "en")
	es := i18nKeys(t, "es")

	if len(en) < 100 {
		t.Fatalf("parsed only %d EN keys — the app.js i18n layout changed and this test needs updating", len(en))
	}

	for k := range en {
		if !es[k] {
			t.Errorf("key %q exists in EN but is missing in ES", k)
		}
	}
	for k := range es {
		if !en[k] {
			t.Errorf("key %q exists in ES but is missing in EN", k)
		}
	}
	if !t.Failed() {
		t.Logf("i18n parity: %d keys in both languages", len(en))
	}
}
