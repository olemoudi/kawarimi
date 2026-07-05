package crypto

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestEstimatePasswordStrengthLevels(t *testing.T) {
	cases := []struct {
		password string
		maxLevel int // level must be <= maxLevel (weak things stay weak)
		minLevel int // level must be >= minLevel (strong things stay strong)
		warning  string
	}{
		// Common passwords are floor-level regardless of surface complexity.
		{"password", StrengthVeryWeak, StrengthVeryWeak, "common_password"},
		{"qwerty", StrengthVeryWeak, StrengthVeryWeak, "common_password"},
		{"letmein", StrengthVeryWeak, StrengthVeryWeak, "common_password"},
		{"dragon12", StrengthVeryWeak, StrengthVeryWeak, "common_password"},
		{"PASSWORD", StrengthVeryWeak, StrengthVeryWeak, "common_password"},
		// L33t and suffix decoration must not rescue a common password.
		{"P@ssw0rd", StrengthVeryWeak, StrengthVeryWeak, ""},
		{"P@ssw0rd1985", StrengthVeryWeak, StrengthVeryWeak, ""},
		{"monkey123!", StrengthWeak, StrengthVeryWeak, ""},
		// Patterns.
		{"aaaaaaaaaa", StrengthVeryWeak, StrengthVeryWeak, ""},
		{"abcdefgh", StrengthVeryWeak, StrengthVeryWeak, ""},
		{"12345678", StrengthVeryWeak, StrengthVeryWeak, ""},
		{"19851985", StrengthVeryWeak, StrengthVeryWeak, ""},
		// Dictionary words with light decoration stay below fair.
		{"hola123", StrengthVeryWeak, StrengthVeryWeak, "dictionary"},
		// Short random still fails on length.
		{"xK3!", StrengthVeryWeak, StrengthVeryWeak, ""},
		// Multi-word passphrases from common words: mid-range, not excellent.
		{"correcthorsebatterystaple", StrengthStrong, StrengthWeak, "dictionary"},
		// Long random: excellent.
		{"hFz7#kQp2!wN9x", StrengthExcellent, StrengthExcellent, ""},
		{"kX9$mQ2#vL5!pR8&", StrengthExcellent, StrengthExcellent, ""},
	}
	for _, tc := range cases {
		got := EstimatePasswordStrength(tc.password)
		if got.Level > tc.maxLevel || got.Level < tc.minLevel {
			t.Errorf("%q: level %d (%s, %.1f bits) outside [%d, %d]",
				tc.password, got.Level, got.LevelKey, got.Bits, tc.minLevel, tc.maxLevel)
		}
		if tc.warning != "" && got.Warning != tc.warning {
			t.Errorf("%q: warning %q, want %q", tc.password, got.Warning, tc.warning)
		}
	}
}

func TestEstimatePasswordStrengthEmpty(t *testing.T) {
	got := EstimatePasswordStrength("")
	if got.Bits != 0 || got.Level != StrengthVeryWeak || got.Warning != "too_short" {
		t.Errorf("empty password: got %+v", got)
	}
}

// A password must never score better than brute-forcing its raw charset.
func TestStrengthNeverExceedsCharset(t *testing.T) {
	for _, p := range []string{"password", "aaaa", "P@ssw0rd1985", "abc123abc123", "hola", "a"} {
		got := EstimatePasswordStrength(p)
		raw := charsetBits([]rune(p))
		if got.Bits > raw+0.05 {
			t.Errorf("%q: %.1f bits exceeds charset bound %.1f", p, got.Bits, raw)
		}
	}
}

// Appending characters to a password must not lower its score.
func TestStrengthMonotonicUnderExtension(t *testing.T) {
	base := "kQ7#x"
	prev := EstimatePasswordStrength(base).Bits
	for _, c := range []string{"Z", "9", "!", "m", "W", "4", "%", "t"} {
		base += c
		cur := EstimatePasswordStrength(base).Bits
		if cur+0.05 < prev {
			t.Errorf("extending to %q lowered bits %.1f -> %.1f", base, prev, cur)
		}
		prev = cur
	}
}

func TestStrengthLevelKeyAndCrackYearsConsistency(t *testing.T) {
	for _, p := range []string{"", "password", "hFz7#kQp2!wN9x", "hola123"} {
		got := EstimatePasswordStrength(p)
		if got.LevelKey != strengthLevelKeys[got.Level] {
			t.Errorf("%q: LevelKey %q does not match level %d", p, got.LevelKey, got.Level)
		}
		wantYears := math.Exp2(got.Bits - 1 - AttackerGuessesPerYearLog2)
		// Bits is rounded to 0.1 for display, so allow the matching tolerance.
		if got.CrackYears < wantYears/2 || got.CrackYears > wantYears*2 {
			t.Errorf("%q: CrackYears %g inconsistent with bits %.1f", p, got.CrackYears, got.Bits)
		}
	}
}

// Spanish dictionary coverage: common Spanish words must be recognized as weak
// material, not scored as random characters.
func TestStrengthSpanishDictionary(t *testing.T) {
	plain := EstimatePasswordStrength("porque entonces tiempo")
	random := EstimatePasswordStrength("pqrxzk wvbnms fghjkl")
	if plain.Bits >= random.Bits {
		t.Errorf("spanish words scored %.1f bits, random letters %.1f — dictionary not applied",
			plain.Bits, random.Bits)
	}
}

// The BIP39 list is part of the dictionary (mnemonic words are public knowledge).
func TestStrengthBIP39Dictionary(t *testing.T) {
	// Six alphabetically-early BIP39 words: rank-based scoring must keep this
	// far below six random words' nominal 66 bits.
	got := EstimatePasswordStrength("abandon ability able about above absent")
	if got.Level > StrengthFair {
		t.Errorf("early BIP39 words scored level %d (%.1f bits)", got.Level, got.Bits)
	}
}

func TestStrengthJSONShape(t *testing.T) {
	raw, err := json.Marshal(EstimatePasswordStrength("hola123"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"bits", "level", "levelKey", "crackYears", "warning"} {
		if !strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("JSON missing key %q: %s", key, raw)
		}
	}
}

func TestAcceptableStrengthLevelIsFair(t *testing.T) {
	// The CLI/GUI weak-password gate keys off this constant; moving it is a
	// threat-model change and must be reflected in THREAT_MODEL.md.
	if AcceptableStrengthLevel != StrengthFair {
		t.Errorf("AcceptableStrengthLevel = %d, want StrengthFair", AcceptableStrengthLevel)
	}
}
