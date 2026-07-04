package vault

import (
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Bank Accounts", "bank-accounts"},
		{"Google - user@gmail.com", "google-user-gmail-com"},
		{"  Spaces Around  ", "spaces-around"},
		{"UPPERCASE", "uppercase"},
		{"special!@#$chars", "special-chars"},
		{"already-slugified", "already-slugified"},
		{"", ""},
	}

	for _, tt := range tests {
		got := Slugify(tt.input)
		if got != tt.expected {
			t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGenerateID(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()

	if len(id1) != 8 {
		t.Errorf("ID should be 8 hex chars, got %d: %q", len(id1), id1)
	}
	if id1 == id2 {
		t.Error("consecutive IDs should not be equal")
	}
}

func TestBuildFilename(t *testing.T) {
	got := BuildFilename(CategoryNotes, 1, "bank-accounts", ".md")
	want := "notes/001-bank-accounts.md.age"
	if got != want {
		t.Errorf("BuildFilename = %q, want %q", got, want)
	}

	got = BuildFilename(CategoryDocuments, 12, "will-scan", ".pdf")
	want = "documents/012-will-scan.pdf.age"
	if got != want {
		t.Errorf("BuildFilename = %q, want %q", got, want)
	}
}

func TestManifestCRUD(t *testing.T) {
	m := NewManifest()
	if len(m.Entries) != 0 {
		t.Fatalf("new manifest should be empty")
	}

	entry := &Entry{
		ID:       "abc123",
		Category: CategoryNotes,
		Name:     "test",
		Title:    "Test Entry",
		Filename: "notes/001-test.md.age", // real entries carry the NNN- sequence prefix
	}
	m.AddEntry(entry)

	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m.Entries))
	}

	found := m.FindEntry("abc123")
	if found == nil {
		t.Fatal("FindEntry by ID should return the entry")
	}

	found = m.FindEntry("test")
	if found == nil {
		t.Fatal("FindEntry by name should return the entry")
	}

	if seq := m.NextSeq(CategoryNotes); seq != 2 {
		t.Errorf("NextSeq should be 2, got %d", seq)
	}

	removed := m.RemoveEntry("abc123")
	if removed == nil {
		t.Fatal("RemoveEntry should return the removed entry")
	}
	if len(m.Entries) != 0 {
		t.Fatal("entries should be empty after removal")
	}

	removed = m.RemoveEntry("nonexistent")
	if removed != nil {
		t.Fatal("RemoveEntry should return nil for nonexistent")
	}
}

func TestSanitizeExt(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{".pdf", ".pdf"},
		{".PDF", ".PDF"},
		{"pdf", ".pdf"},
		{"", ""},
		{"../../../etc/passwd", "......etcpasswd"},
		{".jpg/../../", ".jpg...."},
		{".tar.gz", ".tar.gz"},
	}

	for _, tt := range tests {
		got := SanitizeExt(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeExt(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
