// Package testenv provides in-process mocks of every external actor kawarimi talks
// to — SMTP, Telegram, the GitHub API, and the cloud DMS git repo — plus an
// isolated environment, so the full vault lifecycle can be tested end to end with
// no network, no credentials, and no manual steps. See internal/lifecycle for the
// scenarios these support.
//
// The actors themselves live in internal/simenv (a non-test package, also powering
// `kawarimi demo`); this package wraps them with testing.TB conveniences.
package testenv

import (
	"testing"

	"github.com/olemoudi/kawarimi/internal/simenv"
)

// Mail is one captured message.
type Mail = simenv.Mail

// MailServer is a minimal in-process SMTP server that captures every message.
type MailServer = simenv.MailServer

// StartMail starts a mail server on a random loopback port; it is closed on cleanup.
func StartMail(t testing.TB) *MailServer {
	t.Helper()
	m, err := simenv.StartMail()
	if err != nil {
		t.Fatalf("start mail server: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
