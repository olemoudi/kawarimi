package screens

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olemoudi/kawarimi/internal/tui/components"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// EntrySavedMsg is sent when an entry is saved.
type EntrySavedMsg struct {
	Entry *vault.Entry
	IsNew bool
}

// EditorMode is the type of entry being edited.
type EditorMode int

const (
	ModeNote EditorMode = iota
	ModeCredential
)

// Editor is the entry create/edit form.
type Editor struct {
	mode     EditorMode
	entry    *vault.Entry // nil for new entries
	v        *vault.Vault

	// Note fields
	titleInput textinput.Model
	bodyInput  textarea.Model

	// Credential fields
	serviceInput  textinput.Model
	urlInput      textinput.Model
	usernameInput textinput.Model
	passwordInput textinput.Model
	totpInput     textinput.Model
	recoveryInput textinput.Model
	notesInput    textarea.Model

	focusIdx  int
	statusBar components.StatusBar
	err       string
	width     int
	height    int

	// Category selection for new entries
	selectingCategory bool
	categoryIdx       int
}

// NewNoteEditor creates an editor for a new or existing note.
func NewNoteEditor(v *vault.Vault, entry *vault.Entry, existingContent string, width, height int) Editor {
	ti := textinput.New()
	ti.Placeholder = "Note title"
	ti.Width = 50
	ti.Focus()

	ta := textarea.New()
	ta.Placeholder = "Write your note content here..."
	ta.SetWidth(width - 4)
	ta.SetHeight(height - 14)

	if entry != nil {
		ti.SetValue(entry.Title)
		ta.SetValue(existingContent)
	}

	return Editor{
		mode:       ModeNote,
		entry:      entry,
		v:          v,
		titleInput: ti,
		bodyInput:  ta,
		statusBar: components.NewStatusBar(
			components.Hint("tab", "next field"),
			components.Hint("ctrl+s", "save"),
			components.Hint("esc", "cancel"),
		),
		width:  width,
		height: height,
	}
}

// NewCredentialEditor creates an editor for a new or existing credential.
func NewCredentialEditor(v *vault.Vault, entry *vault.Entry, existingData []byte, width, height int) Editor {
	fields := []struct {
		placeholder string
		value       string
		echoMode    textinput.EchoMode
	}{
		{"Service name", "", textinput.EchoNormal},
		{"URL", "", textinput.EchoNormal},
		{"Username", "", textinput.EchoNormal},
		{"Password", "", textinput.EchoPassword},
		{"TOTP Secret", "", textinput.EchoPassword},
		{"Recovery codes (comma-separated)", "", textinput.EchoNormal},
	}

	if entry != nil && len(existingData) > 0 {
		var cred vault.Credential
		if json.Unmarshal(existingData, &cred) == nil {
			fields[0].value = cred.Service
			fields[1].value = cred.URL
			fields[2].value = cred.Username
			fields[3].value = cred.Password
			fields[4].value = cred.TOTPSecret
			fields[5].value = strings.Join(cred.RecoveryCodes, ", ")
		}
	}

	makeInput := func(placeholder, value string, echo textinput.EchoMode) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Width = 50
		ti.EchoMode = echo
		if echo == textinput.EchoPassword {
			ti.EchoCharacter = '*'
		}
		ti.SetValue(value)
		return ti
	}

	notesTA := textarea.New()
	notesTA.Placeholder = "Additional notes..."
	notesTA.SetWidth(width - 4)
	notesTA.SetHeight(4)
	if entry != nil && len(existingData) > 0 {
		var cred vault.Credential
		if json.Unmarshal(existingData, &cred) == nil {
			notesTA.SetValue(cred.Notes)
		}
	}

	e := Editor{
		mode:          ModeCredential,
		entry:         entry,
		v:             v,
		serviceInput:  makeInput(fields[0].placeholder, fields[0].value, fields[0].echoMode),
		urlInput:      makeInput(fields[1].placeholder, fields[1].value, fields[1].echoMode),
		usernameInput: makeInput(fields[2].placeholder, fields[2].value, fields[2].echoMode),
		passwordInput: makeInput(fields[3].placeholder, fields[3].value, fields[3].echoMode),
		totpInput:     makeInput(fields[4].placeholder, fields[4].value, fields[4].echoMode),
		recoveryInput: makeInput(fields[5].placeholder, fields[5].value, fields[5].echoMode),
		notesInput:    notesTA,
		statusBar: components.NewStatusBar(
			components.Hint("tab", "next field"),
			components.Hint("ctrl+s", "save"),
			components.Hint("esc", "cancel"),
		),
		width:  width,
		height: height,
	}
	e.serviceInput.Focus()
	return e
}

// NewCategorySelector creates an editor in category selection mode.
func NewCategorySelector(v *vault.Vault, width, height int) Editor {
	return Editor{
		selectingCategory: true,
		v:                 v,
		width:             width,
		height:            height,
		statusBar: components.NewStatusBar(
			components.Hint("enter", "select"),
			components.Hint("esc", "cancel"),
		),
	}
}

func (e Editor) Init() tea.Cmd { return nil }

func (e Editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if e.selectingCategory {
		return e.updateCategorySelect(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width = msg.Width
		e.height = msg.Height
		return e, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return e, func() tea.Msg { return BackToListMsg{} }
		case "ctrl+s":
			return e, e.save()
		case "tab":
			e.nextField()
			return e, nil
		case "shift+tab":
			e.prevField()
			return e, nil
		}
	}

	return e.updateActiveField(msg)
}

func (e Editor) updateCategorySelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return e, func() tea.Msg { return BackToListMsg{} }
		case "up", "k":
			if e.categoryIdx > 0 {
				e.categoryIdx--
			}
		case "down", "j":
			if e.categoryIdx < 1 {
				e.categoryIdx++
			}
		case "enter":
			switch e.categoryIdx {
			case 0:
				return NewNoteEditor(e.v, nil, "", e.width, e.height), nil
			case 1:
				return NewCredentialEditor(e.v, nil, nil, e.width, e.height), nil
			}
		}
	}
	return e, nil
}

func (e Editor) updateActiveField(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if e.mode == ModeNote {
		if e.focusIdx == 0 {
			e.titleInput, cmd = e.titleInput.Update(msg)
		} else {
			e.bodyInput, cmd = e.bodyInput.Update(msg)
		}
	} else {
		switch e.focusIdx {
		case 0:
			e.serviceInput, cmd = e.serviceInput.Update(msg)
		case 1:
			e.urlInput, cmd = e.urlInput.Update(msg)
		case 2:
			e.usernameInput, cmd = e.usernameInput.Update(msg)
		case 3:
			e.passwordInput, cmd = e.passwordInput.Update(msg)
		case 4:
			e.totpInput, cmd = e.totpInput.Update(msg)
		case 5:
			e.recoveryInput, cmd = e.recoveryInput.Update(msg)
		case 6:
			e.notesInput, cmd = e.notesInput.Update(msg)
		}
	}
	return e, cmd
}

func (e *Editor) nextField() {
	maxIdx := 1
	if e.mode == ModeCredential {
		maxIdx = 6
	}
	e.focusIdx = (e.focusIdx + 1) % (maxIdx + 1)
	e.updateFocus()
}

func (e *Editor) prevField() {
	maxIdx := 1
	if e.mode == ModeCredential {
		maxIdx = 6
	}
	e.focusIdx = (e.focusIdx - 1 + maxIdx + 1) % (maxIdx + 1)
	e.updateFocus()
}

func (e *Editor) updateFocus() {
	if e.mode == ModeNote {
		e.titleInput.Blur()
		e.bodyInput.Blur()
		if e.focusIdx == 0 {
			e.titleInput.Focus()
		} else {
			e.bodyInput.Focus()
		}
	} else {
		inputs := []*textinput.Model{
			&e.serviceInput, &e.urlInput, &e.usernameInput,
			&e.passwordInput, &e.totpInput, &e.recoveryInput,
		}
		for _, inp := range inputs {
			inp.Blur()
		}
		e.notesInput.Blur()
		if e.focusIdx < 6 {
			inputs[e.focusIdx].Focus()
		} else {
			e.notesInput.Focus()
		}
	}
}

func (e Editor) save() tea.Cmd {
	return func() tea.Msg {
		if e.mode == ModeNote {
			return e.saveNote()
		}
		return e.saveCredential()
	}
}

func (e Editor) saveNote() tea.Msg {
	title := strings.TrimSpace(e.titleInput.Value())
	if title == "" {
		return UnlockErrorMsg{Err: fmt.Errorf("title is required")}
	}
	body := e.bodyInput.Value()

	if e.entry != nil {
		if err := e.v.UpdateEntry(e.entry, []byte(body)); err != nil {
			return UnlockErrorMsg{Err: err}
		}
		return EntrySavedMsg{Entry: e.entry, IsNew: false}
	}

	entry, err := e.v.AddNote(title, []byte(body), nil)
	if err != nil {
		return UnlockErrorMsg{Err: err}
	}
	return EntrySavedMsg{Entry: entry, IsNew: true}
}

func (e Editor) saveCredential() tea.Msg {
	service := strings.TrimSpace(e.serviceInput.Value())
	if service == "" {
		return UnlockErrorMsg{Err: fmt.Errorf("service name is required")}
	}

	var codes []string
	for _, c := range strings.Split(e.recoveryInput.Value(), ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			codes = append(codes, c)
		}
	}

	cred := &vault.Credential{
		Service:       service,
		URL:           strings.TrimSpace(e.urlInput.Value()),
		Username:      strings.TrimSpace(e.usernameInput.Value()),
		Password:      e.passwordInput.Value(),
		TOTPSecret:    e.totpInput.Value(),
		RecoveryCodes: codes,
		Notes:         e.notesInput.Value(),
	}

	if e.entry != nil {
		data, err := json.MarshalIndent(cred, "", "  ")
		if err != nil {
			return UnlockErrorMsg{Err: err}
		}
		if err := e.v.UpdateEntry(e.entry, data); err != nil {
			return UnlockErrorMsg{Err: err}
		}
		return EntrySavedMsg{Entry: e.entry, IsNew: false}
	}

	entry, err := e.v.AddCredential(cred, nil)
	if err != nil {
		return UnlockErrorMsg{Err: err}
	}
	return EntrySavedMsg{Entry: entry, IsNew: true}
}

func (e Editor) View() string {
	if e.selectingCategory {
		return e.viewCategorySelect()
	}

	title := "New Entry"
	if e.entry != nil {
		title = "Edit: " + e.entry.Title
	}
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1).
		Render(title)

	var form string
	if e.mode == ModeNote {
		form = e.viewNoteForm()
	} else {
		form = e.viewCredentialForm()
	}

	var errView string
	if e.err != "" {
		errView = "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(e.err)
	}

	return header + "\n" + form + errView + "\n\n" + e.statusBar.View()
}

func (e Editor) viewCategorySelect() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1).
		Render("New Entry - Select Type")

	categories := []string{"Note", "Credential"}
	var items string
	for i, c := range categories {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
		if i == e.categoryIdx {
			cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Render("> ")
			style = style.Bold(true).Foreground(lipgloss.Color("#7C3AED"))
		}
		items += cursor + style.Render(c) + "\n"
	}

	return title + "\n\n" + items + "\n" + e.statusBar.View()
}

func (e Editor) viewNoteForm() string {
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06B6D4")).Width(10)
	return label.Render("Title:") + "\n" + e.titleInput.View() + "\n\n" +
		label.Render("Content:") + "\n" + e.bodyInput.View()
}

func (e Editor) viewCredentialForm() string {
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#06B6D4")).Width(18)

	fields := []struct {
		name  string
		input string
	}{
		{"Service:", e.serviceInput.View()},
		{"URL:", e.urlInput.View()},
		{"Username:", e.usernameInput.View()},
		{"Password:", e.passwordInput.View()},
		{"TOTP Secret:", e.totpInput.View()},
		{"Recovery Codes:", e.recoveryInput.View()},
		{"Notes:", e.notesInput.View()},
	}

	var b strings.Builder
	for _, f := range fields {
		b.WriteString(label.Render(f.name) + "\n" + f.input + "\n\n")
	}
	return b.String()
}
