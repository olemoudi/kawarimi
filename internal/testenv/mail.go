// Package testenv provides in-process mocks of every external actor kawarimi talks
// to — SMTP, Telegram, the GitHub API, and the cloud DMS git repo — plus an
// isolated environment, so the full vault lifecycle can be tested end to end with
// no network, no credentials, and no manual steps. See internal/lifecycle for the
// scenarios these support.
package testenv

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

// Mail is one captured message.
type Mail struct {
	From    string
	To      []string
	Subject string
	Body    string
	Raw     string
}

// MailServer is a minimal in-process SMTP server that net/smtp connects to and that
// captures every message. It speaks just enough ESMTP (EHLO, AUTH PLAIN/LOGIN,
// MAIL/RCPT/DATA/QUIT) for the standard library client with PlainAuth over loopback.
type MailServer struct {
	Host string
	Port int

	ln     net.Listener
	mu     sync.Mutex
	msgs   []Mail
	reject map[string]bool
}

// Reject makes the server refuse RCPT TO for addr (a 550), so tests can exercise
// partial-recipient failure.
func (m *MailServer) Reject(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reject == nil {
		m.reject = map[string]bool{}
	}
	m.reject[addr] = true
}

func (m *MailServer) rejected(addr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reject[addr]
}

// StartMail starts a mail server on a random loopback port; it is closed on cleanup.
func StartMail(t testing.TB) *MailServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start mail server: %v", err)
	}
	m := &MailServer{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, ln: ln}
	go m.serve()
	t.Cleanup(func() { ln.Close() })
	return m
}

// Messages returns a copy of every captured message.
func (m *MailServer) Messages() []Mail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Mail(nil), m.msgs...)
}

// Count returns how many messages have been captured.
func (m *MailServer) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.msgs)
}

// Last returns the most recent message (or the zero Mail if none).
func (m *MailServer) Last() Mail {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.msgs) == 0 {
		return Mail{}
	}
	return m.msgs[len(m.msgs)-1]
}

// SentTo reports whether any captured message was addressed to addr.
func (m *MailServer) SentTo(addr string) bool {
	for _, msg := range m.Messages() {
		for _, to := range msg.To {
			if to == addr {
				return true
			}
		}
	}
	return false
}

func (m *MailServer) serve() {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return
		}
		go m.handle(conn)
	}
}

func (m *MailServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	line := func(s string) { fmt.Fprint(w, s, "\r\n"); w.Flush() }

	line("220 mock.testenv ESMTP ready")
	var from string
	var rcpts []string

	for {
		raw, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimRight(raw, "\r\n")
		up := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(up, "EHLO"):
			fmt.Fprint(w, "250-mock.testenv\r\n250-AUTH PLAIN LOGIN\r\n250 OK\r\n")
			w.Flush()
		case strings.HasPrefix(up, "HELO"):
			line("250 mock.testenv")
		case strings.HasPrefix(up, "AUTH"):
			// PlainAuth sends credentials inline with the AUTH command.
			line("235 2.7.0 Authentication successful")
		case strings.HasPrefix(up, "MAIL FROM:"):
			from = extractAddr(cmd)
			line("250 2.1.0 Ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			addr := extractAddr(cmd)
			if m.rejected(addr) {
				line("550 5.1.1 recipient rejected")
				break
			}
			rcpts = append(rcpts, addr)
			line("250 2.1.5 Ok")
		case strings.HasPrefix(up, "DATA"):
			line("354 End data with <CR><LF>.<CR><LF>")
			body := readDATA(r)
			m.record(from, rcpts, body)
			from, rcpts = "", nil
			line("250 2.0.0 Ok: queued")
		case strings.HasPrefix(up, "RSET"):
			from, rcpts = "", nil
			line("250 2.0.0 Ok")
		case strings.HasPrefix(up, "NOOP"):
			line("250 2.0.0 Ok")
		case strings.HasPrefix(up, "QUIT"):
			line("221 2.0.0 Bye")
			return
		default:
			line("250 OK")
		}
	}
}

func (m *MailServer) record(from string, to []string, raw string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, parseMail(from, append([]string(nil), to...), raw))
}

// readDATA reads message lines until the terminating "." line, undoing dot-stuffing.
func readDATA(r *bufio.Reader) string {
	var b strings.Builder
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			break
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "." {
			break
		}
		if strings.HasPrefix(l, "..") {
			l = l[1:]
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

func parseMail(from string, to []string, raw string) Mail {
	subject, body := "", raw
	if idx := strings.Index(raw, "\n\n"); idx >= 0 {
		headers := raw[:idx]
		body = raw[idx+2:]
		for _, h := range strings.Split(headers, "\n") {
			if strings.HasPrefix(strings.ToLower(h), "subject:") {
				subject = strings.TrimSpace(h[len("subject:"):])
			}
		}
	}
	return Mail{From: from, To: to, Subject: subject, Body: body, Raw: raw}
}

// extractAddr pulls the address out of "MAIL FROM:<a@b>" / "RCPT TO:<a@b>".
func extractAddr(cmd string) string {
	if lt := strings.IndexByte(cmd, '<'); lt >= 0 {
		if gt := strings.IndexByte(cmd[lt:], '>'); gt >= 0 {
			return cmd[lt+1 : lt+gt]
		}
	}
	if c := strings.IndexByte(cmd, ':'); c >= 0 {
		return strings.TrimSpace(cmd[c+1:])
	}
	return strings.TrimSpace(cmd)
}
