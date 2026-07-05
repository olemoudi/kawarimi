package crypto

import (
	"os"
	"strings"
	"testing"
)

// withStdin replaces os.Stdin with a pipe carrying script for the duration of
// the test. PromptPassphrase falls back to line reads off a non-tty stdin.
func withStdin(t *testing.T, script string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		r.Close()
	})
	go func() {
		defer w.Close()
		w.WriteString(script)
	}()
}

const strongPass = "kX9$mQ2#vL5!pR8&"

func TestPromptNewPassphraseStrongAccepted(t *testing.T) {
	t.Setenv("KAWARIMI_STRENGTH_STRICT", "1")
	withStdin(t, strongPass+"\n"+strongPass+"\n")
	got, err := PromptNewPassphrase()
	if err != nil {
		t.Fatal(err)
	}
	if got != strongPass {
		t.Errorf("got %q", got)
	}
}

func TestPromptNewPassphraseWeakAcceptedWithYes(t *testing.T) {
	t.Setenv("KAWARIMI_STRENGTH_STRICT", "1")
	withStdin(t, "password\npassword\nyes\n")
	got, err := PromptNewPassphrase()
	if err != nil {
		t.Fatal(err)
	}
	if got != "password" {
		t.Errorf("got %q", got)
	}
}

func TestPromptNewPassphraseWeakRejectedThenRetry(t *testing.T) {
	t.Setenv("KAWARIMI_STRENGTH_STRICT", "1")
	withStdin(t, "password\npassword\nn\n"+strongPass+"\n"+strongPass+"\n")
	got, err := PromptNewPassphrase()
	if err != nil {
		t.Fatal(err)
	}
	if got != strongPass {
		t.Errorf("weak password must be re-prompted, got %q", got)
	}
}

func TestPromptNewPassphraseWeakEOFFails(t *testing.T) {
	t.Setenv("KAWARIMI_STRENGTH_STRICT", "1")
	withStdin(t, "password\npassword\n")
	if _, err := PromptNewPassphrase(); err == nil {
		t.Error("EOF at the weak-password confirmation must fail, not accept")
	}
}

func TestPromptNewPassphraseNonStrictWarnsAndProceeds(t *testing.T) {
	// Without the strict env and without a terminal (test stdin is a pipe),
	// scripts keep working: weak passwords pass with a warning.
	t.Setenv("KAWARIMI_STRENGTH_STRICT", "0")
	withStdin(t, "password\npassword\n")
	got, err := PromptNewPassphrase()
	if err != nil {
		t.Fatal(err)
	}
	if got != "password" {
		t.Errorf("got %q", got)
	}
}

func TestPromptNewPassphraseMismatchFails(t *testing.T) {
	t.Setenv("KAWARIMI_STRENGTH_STRICT", "1")
	withStdin(t, "one two three\nfour five six\n")
	if _, err := PromptNewPassphrase(); err == nil || !strings.Contains(err.Error(), "match") {
		t.Errorf("mismatched confirmation must fail, got %v", err)
	}
}

func TestRenderStrengthMeter(t *testing.T) {
	weak := RenderStrengthMeter(EstimatePasswordStrength("password"))
	if !strings.Contains(weak, "very weak") || !strings.Contains(weak, "$100k/year") {
		t.Errorf("meter output missing label or budget: %s", weak)
	}
	strong := RenderStrengthMeter(EstimatePasswordStrength(strongPass))
	if !strings.Contains(strong, "excellent") || !strings.Contains(strong, "##########") {
		t.Errorf("meter output for strong password: %s", strong)
	}
}

func TestFormatCrackTime(t *testing.T) {
	cases := []struct {
		years float64
		want  string
	}{
		{0.00001, "less than an hour"},
		{0.002, "hours"},
		{0.05, "days"},
		{0.5, "months"},
		{100, "years"},
		{5e4, "thousand years"},
		{5e7, "million years"},
		{5e12, "over a billion years"},
	}
	for _, tc := range cases {
		if got := FormatCrackTime(tc.years); !strings.Contains(got, tc.want) {
			t.Errorf("FormatCrackTime(%g) = %q, want ~%q", tc.years, got, tc.want)
		}
	}
}
