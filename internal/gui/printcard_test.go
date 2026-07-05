package gui

import (
	"strings"
	"testing"
)

// jsFunc extracts the source of one top-level `function name(...)` from the
// embedded SPA, up to the next top-level function. Same source-pinning style as
// the i18n parity test: these invariants have no other unattended guard.
func jsFunc(t *testing.T, name string) string {
	t.Helper()
	src := string(appJS)
	start := strings.Index(src, "\nfunction "+name+"(")
	if start < 0 {
		t.Fatalf("no function %q in app.js", name)
	}
	rest := src[start+1:]
	end := strings.Index(rest[1:], "\nfunction ")
	if end < 0 {
		return rest
	}
	return rest[:end+1]
}

// The printed card is handed to a recipient: it must carry ONLY the recipient
// passphrase — never the owner's mnemonic or recovery code.
func TestPrintCardContainsOnlyRecipientSecret(t *testing.T) {
	card := jsFunc(t, "printCard")
	if !strings.Contains(card, "passphrase") {
		t.Error("printCard must render the recipient passphrase")
	}
	for _, forbidden := range []string{"mnemonic", "recoveryCode"} {
		if strings.Contains(card, forbidden) {
			t.Errorf("printCard references %q — the card must never carry owner secrets", forbidden)
		}
	}
}

// The card explains itself in BOTH languages: the owner's UI language setting
// cannot know what the recipient will speak.
func TestPrintCardBilingual(t *testing.T) {
	card := jsFunc(t, "printCard")
	for _, want := range []string{"tarjeta", "card", "CLAVE", "KEY", "De / From"} {
		if !strings.Contains(card, want) {
			t.Errorf("printCard text missing %q", want)
		}
	}
}

// Printing must always go through the print-solo isolation: exactly one
// window.print() call, inside printNode, and the CSS that hides everything but
// the target must exist. Without this, a print would put the owner's mnemonic
// and the recipient's card on the same sheet.
func TestPrintIsAlwaysBlockIsolated(t *testing.T) {
	src := string(appJS)
	if got := strings.Count(src, "window.print("); got != 1 {
		t.Fatalf("expected exactly 1 window.print() call, found %d", got)
	}
	if !strings.Contains(jsFunc(t, "printNode"), "window.print()") {
		t.Error("the single window.print() must live inside printNode")
	}
	css := string(appCSS)
	for _, want := range []string{
		"body.print-solo * { visibility: hidden; }",
		"body.print-solo .print-target, body.print-solo .print-target * { visibility: visible; }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing print isolation rule %q", want)
		}
	}
}
