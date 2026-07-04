package deadswitch

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestImapQuote(t *testing.T) {
	cases := map[string]string{
		`plain`:      `"plain"`,
		`with space`: `"with space"`,
		`qu"ote`:     `"qu\"ote"`,
		`back\slash`: `"back\\slash"`,
		`bo"th\`:     `"bo\"th\\"`,
	}
	for in, want := range cases {
		if got := imapQuote(in); got != want {
			t.Errorf("imapQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

// A server that accepts TCP but never completes the TLS handshake must not hang an
// unattended evaluate run — the dial timeout bounds it.
func TestCheckIMAPTimesOutOnSilentServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c // accept and stay silent: the TLS handshake never completes
		}
	}()

	old := imapDialTimeout
	imapDialTimeout = 400 * time.Millisecond
	defer func() { imapDialTimeout = old }()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	cfg := DefaultSwitchConfig()
	cfg.IMAPServer = host
	cfg.IMAPPort = port

	start := time.Now()
	_, cerr := CheckIMAPForAlive(cfg, time.Now())
	if cerr == nil || !strings.Contains(cerr.Error(), "connect") {
		t.Fatalf("silent server must fail the connect step, got %v", cerr)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("CheckIMAPForAlive hung too long before timing out: %v", d)
	}
}
