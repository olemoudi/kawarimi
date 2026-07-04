package gui

import (
	"os/exec"
	"runtime"
)

// openBrowser best-effort opens url in the user's default browser. It never blocks.
func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		name, args = "open", []string{url}
	default: // linux, *bsd
		name, args = "xdg-open", []string{url}
	}
	return exec.Command(name, args...).Start()
}
