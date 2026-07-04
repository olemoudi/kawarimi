package gui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/olemoudi/kawarimi/internal/vault"
)

type entrySummary struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
}

func summarize(e *vault.Entry) entrySummary {
	return entrySummary{
		ID: e.ID, Category: string(e.Category), Name: e.Name, Title: e.Title, UpdatedAt: e.UpdatedAt,
	}
}

// handleEntriesList returns all entry summaries (no decrypted content).
func (s *server) handleEntriesList(w http.ResponseWriter, r *http.Request) {
	var out []entrySummary
	err := s.sess.withVault(func(v *vault.Vault) error {
		for _, e := range v.Manifest.Entries {
			out = append(out, summarize(e))
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if out == nil {
		out = []entrySummary{}
	}
	writeJSON(w, http.StatusOK, out)
}

type credentialBody struct {
	Service  string `json:"service"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Notes    string `json:"notes"`
}

func (c credentialBody) toVault() *vault.Credential {
	return &vault.Credential{
		Service: strings.TrimSpace(c.Service), URL: strings.TrimSpace(c.URL),
		Username: c.Username, Password: c.Password, Notes: c.Notes,
	}
}

type entryWriteRequest struct {
	Category   string          `json:"category"`
	Title      string          `json:"title"`
	Content    string          `json:"content"`
	Credential *credentialBody `json:"credential"`
}

// handleEntryCreate adds a note or credential. Documents are managed via the CLI.
func (s *server) handleEntryCreate(w http.ResponseWriter, r *http.Request) {
	var req entryWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var created *vault.Entry
	err := s.sess.withVault(func(v *vault.Vault) error {
		switch req.Category {
		case "notes":
			if strings.TrimSpace(req.Title) == "" {
				return errBadRequest("a title is required")
			}
			e, err := v.AddNote(req.Title, []byte(req.Content), nil)
			created = e
			return err
		case "credentials":
			if req.Credential == nil || strings.TrimSpace(req.Credential.Service) == "" {
				return errBadRequest("a service name is required")
			}
			e, err := v.AddCredential(req.Credential.toVault(), nil)
			created = e
			return err
		default:
			return errBadRequest("unsupported category (add documents with the CLI)")
		}
	})
	if err != nil {
		writeEntryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summarize(created))
}

// handleEntryGet returns an entry's decrypted content.
func (s *server) handleEntryGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var resp map[string]any
	err := s.sess.withVault(func(v *vault.Vault) error {
		entry := v.Manifest.FindEntry(id)
		if entry == nil {
			return errNotFound
		}
		content, err := v.ShowEntry(entry)
		if err != nil {
			return err
		}
		resp = map[string]any{
			"id": entry.ID, "category": string(entry.Category),
			"title": entry.Title, "name": entry.Name, "updatedAt": entry.UpdatedAt,
		}
		switch entry.Category {
		case vault.CategoryNotes:
			resp["content"] = string(content)
		case vault.CategoryCredentials:
			var c vault.Credential
			if err := json.Unmarshal(content, &c); err != nil {
				return err
			}
			resp["credential"] = c
		default: // documents: do not stream raw bytes into the browser
			resp["binary"] = true
			resp["size"] = len(content)
			resp["contentType"] = entry.ContentType
		}
		return nil
	})
	if err != nil {
		writeEntryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleEntryUpdate replaces a note's or credential's content.
func (s *server) handleEntryUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req entryWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	err := s.sess.withVault(func(v *vault.Vault) error {
		entry := v.Manifest.FindEntry(id)
		if entry == nil {
			return errNotFound
		}
		switch entry.Category {
		case vault.CategoryNotes:
			return v.UpdateEntry(entry, []byte(req.Content))
		case vault.CategoryCredentials:
			if req.Credential == nil {
				return errBadRequest("credential fields are required")
			}
			data, err := json.Marshal(req.Credential.toVault())
			if err != nil {
				return err
			}
			return v.UpdateEntry(entry, data)
		default:
			return errBadRequest("documents can only be replaced via the CLI")
		}
	})
	if err != nil {
		writeEntryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleEntryDelete removes an entry.
func (s *server) handleEntryDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.sess.withVault(func(v *vault.Vault) error {
		_, err := v.RemoveEntry(id)
		return err
	})
	if err != nil {
		writeEntryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- small typed errors so handlers can map to status codes ---

type statusError struct {
	code int
	msg  string
}

func (e statusError) Error() string { return e.msg }

func errBadRequest(msg string) error { return statusError{http.StatusBadRequest, msg} }

var errNotFound = statusError{http.StatusNotFound, "entry not found"}

func writeEntryError(w http.ResponseWriter, err error) {
	if se, ok := err.(statusError); ok {
		writeError(w, se.code, se.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
