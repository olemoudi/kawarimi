package gui

import (
	"net/http"
)

// The demo endpoints are thin adapters over demo.World (registered only when the
// server was launched via `kawarimi demo`); all state and story logic lives there.

func (s *server) handleDemoState(w http.ResponseWriter, r *http.Request) {
	snap, err := s.opts.Demo.Snapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleDemoAdvance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Days int `json:"days"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	snap, err := s.opts.Demo.Advance(body.Days)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleDemoCheckin(w http.ResponseWriter, r *http.Request) {
	snap, err := s.opts.Demo.Checkin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleDemoTelegramAlive(w http.ResponseWriter, r *http.Request) {
	snap, err := s.opts.Demo.TelegramAlive()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleDemoRecipientOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key   string `json:"key"`
		Words string `json:"words"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	snap, err := s.opts.Demo.RecipientOpen(body.Key, body.Words)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleDemoReset(w http.ResponseWriter, r *http.Request) {
	snap, err := s.opts.Demo.Reset()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
