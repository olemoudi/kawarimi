package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	cases := []struct {
		in     string
		minLen int
		want   string
		ok     bool
	}{
		{"  Hello ", 3, "hello", true},
		{"ab", 3, "", false},                    // too short
		{"has space", 3, "", false},             // inner space
		{`back\slash`, 3, "", false},            // would break the generated source
		{`qu"ote`, 3, "", false},                //
		{strings.Repeat("x", 31), 3, "", false}, // too long
		{"Añadir", 3, "añadir", true},           // deaccent happens later, clean only lowercases
	}
	for _, tc := range cases {
		got, ok := clean(tc.in, tc.minLen)
		if got != tc.want || ok != tc.ok {
			t.Errorf("clean(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDeaccent(t *testing.T) {
	if got := deaccent("añádír órgüllo"); got != "anadir orgullo" {
		t.Errorf("deaccent = %q", got)
	}
}

func TestReadListDedupesAndRanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "words.txt")
	// "México" deaccents to "mexico" which then collides with the later plain
	// "mexico" — the first (better-ranked) occurrence must win.
	content := "the 100\nof 90\nMéxico 80\nab 70\nmexico 60\nzz 50\nvalid 40\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readList(path, 3, true, 3)
	want := []string{"the", "mexico", "valid"} // "of"/"ab"/"zz" filtered (len<3), dupe dropped, limit 3
	if len(got) != len(want) {
		t.Fatalf("readList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("readList = %v, want %v", got, want)
		}
	}
}

func TestEmitProducesValidGoLiterals(t *testing.T) {
	var b strings.Builder
	emit(&b, "testList", "comment line", []string{"casa", "perro"})
	out := b.String()
	for _, want := range []string{"var testList = []string{", `"casa"`, `"perro"`, "// comment line"} {
		if !strings.Contains(out, want) {
			t.Errorf("emit output missing %q:\n%s", want, out)
		}
	}
}
