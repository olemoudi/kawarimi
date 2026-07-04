package deadswitch

import (
	"net"
	"strconv"
	"testing"
	"time"
)

// A server that accepts the connection but never speaks must not hang the (possibly
// unattended) send forever — the session deadline must abort it.
func TestSendEmailTimesOutOnSilentServer(t *testing.T) {
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
			_ = c // accept but never respond
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	old := smtpTimeout
	smtpTimeout = 400 * time.Millisecond
	defer func() { smtpTimeout = old }()

	cfg := &SwitchConfig{
		SMTPServer: host, SMTPPort: port,
		SMTPUsername: "u", SMTPPassword: "p", SenderEmail: "u@test",
	}
	start := time.Now()
	if err := SendEmail(cfg, []string{"a@test"}, "subject", "body"); err == nil {
		t.Fatal("expected a timeout error from a silent server")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("SendEmail hung too long before timing out: %v", d)
	}
}
