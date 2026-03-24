package deadswitch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const timerUnit = `[Unit]
Description=Kawarimi dead man's switch check

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
`

// GenerateServiceUnit generates the systemd service unit content.
func GenerateServiceUnit(kawarimiBinary string) string {
	// Escape % for systemd (it uses % as a specifier prefix)
	escaped := strings.ReplaceAll(kawarimiBinary, "%", "%%")
	return fmt.Sprintf(`[Unit]
Description=Kawarimi dead man's switch evaluation

[Service]
Type=oneshot
ExecStart=%s switch evaluate
`, escaped)
}

// SystemdDir returns the path to the user's systemd directory.
func SystemdDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// InstallSystemdUnits writes the timer and service files.
func InstallSystemdUnits(kawarimiBinary string) error {
	dir, err := SystemdDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating systemd dir: %w", err)
	}

	timerPath := filepath.Join(dir, "kawarimi-switch.timer")
	if err := os.WriteFile(timerPath, []byte(timerUnit), 0644); err != nil {
		return fmt.Errorf("writing timer: %w", err)
	}

	servicePath := filepath.Join(dir, "kawarimi-switch.service")
	serviceContent := GenerateServiceUnit(kawarimiBinary)
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("writing service: %w", err)
	}

	return nil
}
