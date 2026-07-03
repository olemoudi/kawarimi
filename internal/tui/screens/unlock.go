package screens

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// VaultUnlockedMsg is sent when the vault is successfully unlocked.
type VaultUnlockedMsg struct {
	Vault  *vault.Vault
	Header *vault.Header
	Config *config.Config
}

// UnlockErrorMsg is sent when unlock fails.
type UnlockErrorMsg struct{ Err error }

// Unlock is the password entry screen.
type Unlock struct {
	input   textinput.Model
	err     string
	loading bool
	cfg     *config.Config
	hasV2   bool
	width   int
	height  int
}

// NewUnlock creates the unlock screen.
func NewUnlock() Unlock {
	ti := textinput.New()
	ti.Placeholder = "Enter password..."
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	ti.Focus()
	ti.Width = 40

	return Unlock{input: ti}
}

func (u Unlock) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, u.detectVault())
}

type vaultDetected struct {
	cfg   *config.Config
	hasV2 bool
	err   error
}

func (u Unlock) detectVault() tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return vaultDetected{err: err}
		}
		hasV2 := vault.IsV2Vault(cfg.VaultDir)
		return vaultDetected{cfg: cfg, hasV2: hasV2}
	}
}

func (u Unlock) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		u.width = msg.Width
		u.height = msg.Height
		return u, nil

	case vaultDetected:
		if msg.err != nil {
			u.err = fmt.Sprintf("No vault configured: %v", msg.err)
			return u, nil
		}
		u.cfg = msg.cfg
		u.hasV2 = msg.hasV2
		return u, nil

	case VaultUnlockedMsg:
		return u, nil // handled by parent

	case UnlockErrorMsg:
		u.loading = false
		u.err = msg.Err.Error()
		u.input.SetValue("")
		u.input.Focus()
		return u, nil

	case tea.KeyMsg:
		if u.loading {
			return u, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return u, tea.Quit
		case "enter":
			if u.cfg == nil {
				return u, nil
			}
			password := u.input.Value()
			if password == "" {
				u.err = "Password cannot be empty"
				return u, nil
			}
			u.loading = true
			u.err = ""
			return u, u.tryUnlock(password)
		}
	}

	var cmd tea.Cmd
	u.input, cmd = u.input.Update(msg)
	return u, cmd
}

func (u Unlock) tryUnlock(password string) tea.Cmd {
	return func() tea.Msg {
		if u.hasV2 {
			return tryUnlockV2(u.cfg, password)
		}
		return tryUnlockV1(u.cfg, password)
	}
}

func tryUnlockV2(cfg *config.Config, password string) tea.Msg {
	header, err := vault.LoadHeader(cfg.VaultDir)
	if err != nil {
		return UnlockErrorMsg{Err: fmt.Errorf("loading header: %w", err)}
	}

	appDir, err := config.AppDirPath()
	if err != nil {
		return UnlockErrorMsg{Err: err}
	}

	deviceKeyPath := filepath.Join(appDir, "device.key")
	dkf, err := crypto.LoadDeviceKeyFile(deviceKeyPath)
	if err != nil {
		return UnlockErrorMsg{Err: fmt.Errorf("no device key found: %w", err)}
	}

	deviceKey, err := crypto.DecryptDeviceKey(dkf, password)
	if err != nil {
		return UnlockErrorMsg{Err: fmt.Errorf("wrong password")}
	}
	defer crypto.ZeroBytes(deviceKey)

	_, ageIdentity, err := header.OpenWithOwner(password, deviceKey)
	if err != nil {
		return UnlockErrorMsg{Err: fmt.Errorf("unlock failed: %w", err)}
	}

	v, err := vault.OpenV2(cfg.VaultDir, ageIdentity, header.AgeRecipient)
	if err != nil {
		return UnlockErrorMsg{Err: fmt.Errorf("opening vault: %w", err)}
	}

	return VaultUnlockedMsg{Vault: v, Header: header, Config: cfg}
}

func tryUnlockV1(cfg *config.Config, password string) tea.Msg {
	v, err := vault.Open(cfg.VaultDir, password)
	if err != nil {
		return UnlockErrorMsg{Err: fmt.Errorf("wrong passphrase")}
	}
	return VaultUnlockedMsg{Vault: v, Config: cfg}
}

func (u Unlock) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1).
		Render("KAWARIMI")

	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginBottom(2).
		Render("Encrypted End-of-Life Vault")

	var vaultInfo string
	if u.cfg != nil {
		vaultType := "v1"
		if u.hasV2 {
			vaultType = "v2"
		}
		vaultInfo = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			MarginBottom(1).
			Render(fmt.Sprintf("Vault: %s (%s)", u.cfg.VaultDir, vaultType))
	}

	var status string
	if u.loading {
		status = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#06B6D4")).
			Render("Unlocking vault...")
	} else if u.err != "" {
		status = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")).
			Render(u.err)
	}

	inputLabel := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB")).
		MarginBottom(0).
		Render("Password:")

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		vaultInfo,
		"",
		inputLabel,
		u.input.View(),
		"",
		status,
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(2, 4).
		Render(content)

	if u.width > 0 && u.height > 0 {
		return lipgloss.Place(u.width, u.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}
