package vault

import (
	"testing"
)

// Delete-then-add must NOT reuse a sequence number, or the new entry's ciphertext
// would overwrite a surviving entry's file and silently lose data.
func TestNextSeqNoReuseAfterDelete(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	v, err := CreateV2(dir, res.AgeIdentity, res.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("CreateV2: %v", err)
	}

	first, err := v.AddNote("Bank", []byte("one"), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := v.AddNote("Bank", []byte("two"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Filename == second.Filename {
		t.Fatalf("two entries share a filename: %s", first.Filename)
	}

	// Remove the first, then add a third with the same title.
	if _, err := v.RemoveEntry(first.ID); err != nil {
		t.Fatal(err)
	}
	third, err := v.AddNote("Bank", []byte("three"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if third.Filename == second.Filename {
		t.Fatalf("re-added entry reused the surviving entry's filename %s — data loss", second.Filename)
	}

	// The surviving entry's content must be intact.
	got, err := v.ShowEntry(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two" {
		t.Errorf("surviving entry content = %q, want 'two' (was it overwritten?)", got)
	}
}

func TestSeqFromFilename(t *testing.T) {
	cases := map[string]int{
		"notes/001-bank.md.age":          1,
		"credentials/042-email.json.age": 42,
		"documents/no-seq.pdf.age":       0,
		"weird":                          0,
	}
	for in, want := range cases {
		if got := seqFromFilename(in); got != want {
			t.Errorf("seqFromFilename(%q) = %d, want %d", in, got, want)
		}
	}
}
