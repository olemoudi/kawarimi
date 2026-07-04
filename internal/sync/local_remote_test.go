package sync

import "testing"

// isLocalRemote decides whether pushes need SSH auth. Misclassifying a local
// path as SSH breaks every test remote on Windows (C:\... bare repos); the
// reverse would try to push to a real remote with no auth.
func TestIsLocalRemote(t *testing.T) {
	local := []string{
		"",
		"/tmp/dms.git",
		"./dms.git",
		"../dms.git",
		"file:///tmp/dms.git",
		`C:\Users\o\AppData\Local\Temp\dms.git`, // Windows drive path
		"C:/Users/o/dms.git",                    // forward-slash drive path
		`c:\lower\drive.git`,
		`\\server\share\dms.git`, // UNC
	}
	for _, u := range local {
		if !isLocalRemote(u) {
			t.Errorf("isLocalRemote(%q) = false, want true", u)
		}
	}

	remote := []string{
		"git@github.com:owner/repo.git", // SCP-style: host before ':', not a drive
		"ssh://git@github.com/owner/repo.git",
		"https://github.com/owner/repo.git",
		"github.com:owner/repo.git",
	}
	for _, u := range remote {
		if isLocalRemote(u) {
			t.Errorf("isLocalRemote(%q) = true, want false", u)
		}
	}
}
