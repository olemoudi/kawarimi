package crypto

// Password strength estimation calibrated against the threat model in
// THREAT_MODEL.md: an attacker willing to spend $100,000/year on offline
// cracking of the owner slot (Argon2id t=2, 256 MiB — see OwnerSlotParams).
//
// The estimator is a compact zxcvbn-style lower bound: it recognizes common
// passwords (rank-ordered top-10k), dictionary words (English, Spanish and the
// BIP39 list), l33t substitutions, character sequences, repeats and dates, and
// falls back to charset entropy for what remains. The reported bits are the
// MINIMUM over all decompositions found — by design pessimistic, never flattering.

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// AttackerGuessesPerYearLog2 is log2 of the guesses/year a $100k/year attacker
// can buy against the owner slot. Derivation (see THREAT_MODEL.md §Password
// cracking economics): one guess costs ~1 core-second (BenchmarkOwnerSlotKDF),
// cloud compute floors at ~$0.01/vCPU-hour → ~3.6e10 guesses/year ≈ 2^35, plus
// a generous 32× (2^5) advantage for custom memory-hard rigs → 2^40.
const AttackerGuessesPerYearLog2 = 40

// Strength levels, weakest to strongest.
const (
	StrengthVeryWeak  = 0 // < 30 bits: cracked in hours
	StrengthWeak      = 1 // < 45 bits: within a decade of the modeled budget
	StrengthFair      = 2 // < 60 bits: survives the modeled attacker with margin
	StrengthStrong    = 3 // < 75 bits: comfortable against decades of budget growth
	StrengthExcellent = 4 // >= 75 bits
)

// strengthLevelKeys maps a level to a stable machine-readable key used by both
// the CLI and the GUI (which localizes them).
var strengthLevelKeys = [5]string{"very_weak", "weak", "fair", "strong", "excellent"}

// PasswordStrength is the result of EstimatePasswordStrength.
type PasswordStrength struct {
	Bits       float64 `json:"bits"`       // estimated guess-entropy (lower bound)
	Level      int     `json:"level"`      // 0..4, see Strength* constants
	LevelKey   string  `json:"levelKey"`   // "very_weak".."excellent"
	CrackYears float64 `json:"crackYears"` // expected years to crack at $100k/year
	Warning    string  `json:"warning"`    // dominant weakness: "common_password", "dictionary", "sequence", "repeat", "digits", "too_short" or ""
}

// AcceptableStrengthLevel is the minimum level that passes without a
// weak-password confirmation (CLI) or a "use anyway" step (GUI).
const AcceptableStrengthLevel = StrengthFair

// EstimatePasswordStrength returns a pessimistic entropy estimate for a
// user-chosen vault password.
func EstimatePasswordStrength(password string) PasswordStrength {
	if password == "" {
		return packStrength(0, "too_short")
	}
	strengthInit()

	runes := []rune(password)
	norm := normalizeForMatch(runes) // lowercased, deaccented
	deleet := deleetRunes(norm)      // additionally l33t-decoded
	leetSubs := countDiff(norm, deleet)

	bits := charsetBits(runes)
	warning := ""
	if len(runes) < 8 {
		warning = "too_short"
	}

	// Whole-password common-password lookups (with and without a trailing
	// digit/symbol suffix, with and without l33t decoding).
	for _, cand := range wholePasswordCandidates(norm, deleet, runes, leetSubs) {
		if cand.bits < bits {
			bits, warning = cand.bits, cand.warning
		}
	}

	// Pattern segmentation.
	if seg := segmentBits(runes, norm, deleet); seg.bits < bits {
		bits, warning = seg.bits, seg.warning
	}

	return packStrength(bits, warning)
}

func packStrength(bits float64, warning string) PasswordStrength {
	if bits < 0 {
		bits = 0
	}
	level := StrengthExcellent
	switch {
	case bits < 30:
		level = StrengthVeryWeak
	case bits < 45:
		level = StrengthWeak
	case bits < 60:
		level = StrengthFair
	case bits < 75:
		level = StrengthStrong
	}
	return PasswordStrength{
		Bits:       math.Round(bits*10) / 10,
		Level:      level,
		LevelKey:   strengthLevelKeys[level],
		CrackYears: math.Exp2(bits - 1 - AttackerGuessesPerYearLog2),
		Warning:    warning,
	}
}

// --- lookup tables -----------------------------------------------------------

var (
	strengthOnce    sync.Once
	commonPWRank    map[string]int // password -> 1-based rank
	dictWordRank    map[string]int // word -> 1-based rank (best across languages)
	longestDictWord int
)

func strengthInit() {
	strengthOnce.Do(func() {
		commonPWRank = make(map[string]int, len(strengthCommonPasswords))
		for i, p := range strengthCommonPasswords {
			if _, dup := commonPWRank[p]; !dup {
				commonPWRank[p] = i + 1
			}
		}
		dictWordRank = make(map[string]int, len(strengthEnglishWords)+len(strengthSpanishWords)+len(bip39WordList))
		addWords := func(words []string) {
			for i, w := range words {
				if r, ok := dictWordRank[w]; !ok || i+1 < r {
					dictWordRank[w] = i + 1
				}
				if len(w) > longestDictWord {
					longestDictWord = len(w)
				}
			}
		}
		addWords(strengthEnglishWords)
		addWords(strengthSpanishWords)
		addWords(bip39WordList[:])
	})
}

// --- normalization -----------------------------------------------------------

var deaccentMap = map[rune]rune{
	'á': 'a', 'é': 'e', 'í': 'i', 'ó': 'o', 'ú': 'u', 'ü': 'u', 'ñ': 'n',
	'à': 'a', 'è': 'e', 'ì': 'i', 'ò': 'o', 'ù': 'u', 'ä': 'a', 'ë': 'e',
	'ï': 'i', 'ö': 'o', 'â': 'a', 'ê': 'e', 'î': 'i', 'ô': 'o', 'û': 'u', 'ç': 'c',
}

var leetMap = map[rune]rune{
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b',
	'@': 'a', '$': 's', '!': 'i', '+': 't', '€': 'e',
}

// normalizeForMatch lowercases and deaccents rune-for-rune (positions preserved).
func normalizeForMatch(runes []rune) []rune {
	out := make([]rune, len(runes))
	for i, r := range runes {
		r = unicode.ToLower(r)
		if d, ok := deaccentMap[r]; ok {
			r = d
		}
		out[i] = r
	}
	return out
}

func deleetRunes(runes []rune) []rune {
	out := make([]rune, len(runes))
	for i, r := range runes {
		if d, ok := leetMap[r]; ok {
			r = d
		}
		out[i] = r
	}
	return out
}

func countDiff(a, b []rune) int {
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

// --- whole-password candidates ------------------------------------------------

type strengthCandidate struct {
	bits    float64
	warning string
}

// wholePasswordCandidates checks the password (and its core after stripping a
// trailing digit/symbol suffix) against the common-password list.
func wholePasswordCandidates(norm, deleet, orig []rune, leetSubs int) []strengthCandidate {
	var out []strengthCandidate
	variation := capsBits(orig)

	check := func(view []rune, extraBits float64) {
		s := string(view)
		if rank, ok := commonPWRank[s]; ok {
			out = append(out, strengthCandidate{math.Log2(float64(rank)) + extraBits + variation, "common_password"})
			return
		}
		// Strip a short trailing digit/symbol suffix ("password123!").
		core := strings.TrimRightFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
		if suffix := len(s) - len(core); suffix > 0 && suffix <= 6 && core != "" {
			if rank, ok := commonPWRank[core]; ok {
				out = append(out, strengthCandidate{
					math.Log2(float64(rank)) + float64(suffix)*3.5 + extraBits + variation, "common_password"})
			}
		}
	}
	check(norm, 0)
	if leetSubs > 0 {
		check(deleet, math.Min(float64(leetSubs), 3)) // +1 bit per substitution, capped
	}
	return out
}

// capsBits credits capitalization: common patterns (leading cap, all caps) get
// 1 bit; scattered capitals a bit more, capped low — casing is not entropy.
func capsBits(runes []rune) float64 {
	upper := 0
	for _, r := range runes {
		if unicode.IsUpper(r) {
			upper++
		}
	}
	switch {
	case upper == 0:
		return 0
	case upper == 1 || upper == len(runes):
		return 1
	default:
		return math.Min(float64(upper), 4)
	}
}

// --- segmentation ------------------------------------------------------------

// segmentBits greedily decomposes the password into dictionary words, sequences,
// repeats and digit/date runs, scoring leftovers as charset entropy. Greedy
// longest-match is not optimal decomposition, but it is a good, cheap lower bound.
func segmentBits(orig, norm, deleet []rune) strengthCandidate {
	n := len(norm)
	total := 0.0
	warnBits := map[string]float64{}
	var leftover []rune

	flushLeftover := func() {
		if len(leftover) > 0 {
			total += charsetBits(leftover)
			leftover = leftover[:0]
		}
	}

	for i := 0; i < n; {
		// Longest dictionary word at i (on the deleeted view; l33t credit added).
		if wordLen, bits := matchWord(deleet, norm, orig, i); wordLen > 0 {
			flushLeftover()
			total += bits
			warnBits["dictionary"] += bits
			i += wordLen
			continue
		}
		if runLen := sequenceRun(norm, i); runLen >= 3 {
			flushLeftover()
			bits := charBits(norm[i]) + math.Log2(float64(runLen))
			total += bits
			warnBits["sequence"] += bits
			i += runLen
			continue
		}
		if runLen := repeatRun(norm, i); runLen >= 3 {
			flushLeftover()
			bits := charBits(norm[i]) + math.Log2(float64(runLen))
			total += bits
			warnBits["repeat"] += bits
			i += runLen
			continue
		}
		if runLen := digitRun(norm, i); runLen >= 4 {
			flushLeftover()
			bits := digitRunBits(norm[i : i+runLen])
			total += bits
			warnBits["digits"] += bits
			i += runLen
			continue
		}
		leftover = append(leftover, orig[i])
		i++
	}
	flushLeftover()

	warning := ""
	best := 0.0
	for w, b := range warnBits {
		// The dominant pattern is the one that "explains" the most of the
		// password; only patterns covering a meaningful share are reported.
		if b > best {
			best, warning = b, w
		}
	}
	if warning != "" && best < total/3 {
		warning = ""
	}
	return strengthCandidate{total, warning}
}

// matchWord finds the longest dictionary word starting at position i and
// returns its length and entropy contribution (0 length if no match).
func matchWord(deleet, norm, orig []rune, i int) (int, float64) {
	maxLen := longestDictWord
	if rem := len(deleet) - i; rem < maxLen {
		maxLen = rem
	}
	for l := maxLen; l >= 3; l-- {
		slice := string(deleet[i : i+l])
		rank, ok := dictWordRank[slice]
		if !ok {
			// Common passwords double as dictionary tokens ("letmein1985").
			rank, ok = commonPWRank[slice]
		}
		if !ok {
			continue
		}
		bits := math.Log2(float64(rank))
		if bits < 1 {
			bits = 1
		}
		bits += capsBits(orig[i : i+l])
		if subs := countDiff(norm[i:i+l], deleet[i:i+l]); subs > 0 {
			bits += math.Min(float64(subs), 3)
		}
		return l, bits
	}
	return 0, 0
}

// sequenceRun returns the length of an ascending/descending rune sequence
// ("abcd", "9876") starting at i, or 0.
func sequenceRun(runes []rune, i int) int {
	if i+1 >= len(runes) {
		return 0
	}
	dir := runes[i+1] - runes[i]
	if dir != 1 && dir != -1 {
		return 0
	}
	l := 2
	for i+l < len(runes) && runes[i+l]-runes[i+l-1] == dir {
		l++
	}
	return l
}

// repeatRun returns the length of a same-rune run starting at i.
func repeatRun(runes []rune, i int) int {
	l := 1
	for i+l < len(runes) && runes[i+l] == runes[i] {
		l++
	}
	return l
}

func digitRun(runes []rune, i int) int {
	l := 0
	for i+l < len(runes) && runes[i+l] >= '0' && runes[i+l] <= '9' {
		l++
	}
	return l
}

// digitRunBits scores a digit run, discounting plausible dates and years:
// "1985" is a year (~7 bits), not 4 random digits (~13 bits).
func digitRunBits(digits []rune) float64 {
	s := string(digits)
	raw := float64(len(digits)) * math.Log2(10)
	switch len(digits) {
	case 4:
		if s >= "1900" && s <= "2039" {
			return 7 // ~140 plausible years
		}
	case 6, 8:
		if looksLikeDate(s) {
			return 15 // ~366 days x ~100 years
		}
	}
	return raw
}

// looksLikeDate reports whether a 6/8-digit string parses as ddmmyy[yy],
// mmddyy[yy] or yy[yy]mmdd with a plausible day and month.
func looksLikeDate(s string) bool {
	pair := func(i int) int { return int(s[i]-'0')*10 + int(s[i+1]-'0') }
	valid := func(d, m int) bool { return d >= 1 && d <= 31 && m >= 1 && m <= 12 }
	switch len(s) {
	case 6:
		return valid(pair(0), pair(2)) || valid(pair(2), pair(0)) || valid(pair(4), pair(2))
	case 8:
		return valid(pair(0), pair(2)) || valid(pair(2), pair(0)) || valid(pair(6), pair(4))
	}
	return false
}

// --- charset fallback ----------------------------------------------------------

// charBits is log2 of the pool size the rune is drawn from.
func charBits(r rune) float64 {
	switch {
	case r >= '0' && r <= '9':
		return math.Log2(10)
	case unicode.IsLower(r):
		return math.Log2(26)
	case unicode.IsUpper(r):
		return math.Log2(26)
	case r < 128:
		return math.Log2(33) // ASCII symbols and space
	default:
		return math.Log2(40) // other unicode: bounded credit
	}
}

// charsetBits is length x log2(pool), where the pool is the union of the
// character classes actually used.
func charsetBits(runes []rune) float64 {
	if len(runes) == 0 {
		return 0
	}
	var lower, upper, digit, symbol, other bool
	for _, r := range runes {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case r < 128:
			symbol = true
		default:
			other = true
		}
	}
	pool := 0
	if lower {
		pool += 26
	}
	if upper {
		pool += 26
	}
	if digit {
		pool += 10
	}
	if symbol {
		pool += 33
	}
	if other {
		pool += 40
	}
	return float64(len(runes)) * math.Log2(float64(pool))
}
