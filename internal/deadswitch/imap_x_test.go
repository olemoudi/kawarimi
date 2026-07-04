package deadswitch_test

import (
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/testenv"
)

func imapConfig(srv *testenv.IMAPServer) *deadswitch.SwitchConfig {
	cfg := deadswitch.DefaultSwitchConfig()
	cfg.IMAPServer = srv.Host
	cfg.IMAPPort = srv.Port
	cfg.SMTPUsername = "owner@test"
	cfg.SMTPPassword = "imap-pw"
	cfg.UserEmail = "owner@test"
	return cfg
}

func TestCheckIMAPForAliveFindsReply(t *testing.T) {
	srv := testenv.StartIMAP(t)
	srv.ScriptAlive("7", "9")

	alive, err := deadswitch.CheckIMAPForAlive(imapConfig(srv), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CheckIMAPForAlive: %v", err)
	}
	if !alive {
		t.Fatal("an ALIVE reply in the inbox must be found")
	}

	// The session must authenticate and scope the search to the owner's address
	// and the ALIVE subject (a random mail must not suppress the switch).
	cmds := strings.Join(srv.Commands(), "\n")
	if !strings.Contains(cmds, `LOGIN "owner@test" "imap-pw"`) {
		t.Errorf("LOGIN not sent as quoted strings:\n%s", cmds)
	}
	if !strings.Contains(cmds, `FROM "owner@test"`) || !strings.Contains(cmds, "SUBJECT ALIVE") {
		t.Errorf("SEARCH must filter FROM owner and SUBJECT ALIVE:\n%s", cmds)
	}
	if !strings.Contains(cmds, "SINCE") {
		t.Errorf("SEARCH must be bounded to since the last check-in:\n%s", cmds)
	}
}

func TestCheckIMAPForAliveNoReply(t *testing.T) {
	srv := testenv.StartIMAP(t) // nothing scripted: empty SEARCH result
	alive, err := deadswitch.CheckIMAPForAlive(imapConfig(srv), time.Now())
	if err != nil || alive {
		t.Fatalf("empty inbox: alive=%v err=%v, want false/nil", alive, err)
	}
}

func TestCheckIMAPForAliveBadCredentials(t *testing.T) {
	srv := testenv.StartIMAP(t)
	srv.RejectLogin()
	alive, err := deadswitch.CheckIMAPForAlive(imapConfig(srv), time.Now())
	if err == nil || alive {
		t.Fatalf("rejected login must error, got alive=%v err=%v", alive, err)
	}
	if !strings.Contains(err.Error(), "login") {
		t.Errorf("error should name the login step: %v", err)
	}
}

// Credentials with IMAP-special characters must be escaped per RFC 3501 — Go's %q
// escaping is not valid IMAP quoting and once broke logins (see imapQuote).
func TestCheckIMAPQuotesSpecialCharacters(t *testing.T) {
	srv := testenv.StartIMAP(t)
	cfg := imapConfig(srv)
	cfg.SMTPPassword = `pa"ss\word`

	if _, err := deadswitch.CheckIMAPForAlive(cfg, time.Now()); err != nil {
		t.Fatalf("CheckIMAPForAlive: %v", err)
	}
	cmds := strings.Join(srv.Commands(), "\n")
	if !strings.Contains(cmds, `"pa\"ss\\word"`) {
		t.Errorf("password was not IMAP-quoted correctly:\n%s", cmds)
	}
}

func TestCheckIMAPNotConfiguredIsSilent(t *testing.T) {
	cfg := deadswitch.DefaultSwitchConfig() // no IMAPServer
	alive, err := deadswitch.CheckIMAPForAlive(cfg, time.Now())
	if alive || err != nil {
		t.Fatalf("no IMAP configured: alive=%v err=%v, want false/nil", alive, err)
	}
}
