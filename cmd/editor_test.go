package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPromptLine(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("  hello world  \n"))
	if got := promptLine(reader, "> "); got != "hello world" {
		t.Errorf("promptLine = %q, want trimmed input", got)
	}
	// EOF with no input: empty string, no hang.
	if got := promptLine(bufio.NewReader(strings.NewReader("")), "> "); got != "" {
		t.Errorf("promptLine on EOF = %q, want empty", got)
	}
}

// fakeEditor installs a script as $EDITOR that appends a line to the file it is
// given — a stand-in for the user typing into vi. POSIX-only: Windows cannot exec
// shebang scripts.
func fakeEditor(t *testing.T, appendLine string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake $EDITOR is a shell script")
	}
	script := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nprintf '%s\\n' \"" + appendLine + "\" >> \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", script)
}

func TestEditInEditor(t *testing.T) {
	fakeEditor(t, "added by the user")
	got, err := editInEditor([]byte("# template\n"))
	if err != nil {
		t.Fatalf("editInEditor: %v", err)
	}
	if !strings.Contains(string(got), "# template") || !strings.Contains(string(got), "added by the user") {
		t.Errorf("edited content = %q", got)
	}
}

func TestEditInEditorPropagatesEditorFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no `false` binary on Windows")
	}
	t.Setenv("EDITOR", "false") // exits 1: the user's edit must not be silently lost
	if _, err := editInEditor([]byte("x")); err == nil {
		t.Fatal("a failing editor must surface as an error")
	}
}

func TestEditCredentialInEditorPrettyPrints(t *testing.T) {
	fakeEditor(t, "")
	got, err := editCredentialInEditor([]byte(`{"user":"u","pass":"p"}`))
	if err != nil {
		t.Fatalf("editCredentialInEditor: %v", err)
	}
	// The compact JSON must have been pretty-printed for the human.
	if !strings.Contains(string(got), "\n  \"user\"") {
		t.Errorf("credential JSON was not pretty-printed:\n%s", got)
	}
}
