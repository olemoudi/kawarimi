package deadswitch

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"
)

// imapTimeout bounds the whole IMAP session so a server that accepts the connection
// and then stalls cannot hang an unattended evaluate run.
const imapTimeout = 30 * time.Second

// imapQuote produces an IMAP quoted string (RFC 3501), escaping backslash and quote.
// Go's %q is NOT valid IMAP quoting (it escapes control chars in ways IMAP rejects).
func imapQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// CheckIMAPForAlive connects to the IMAP server and checks for recent emails
// with subject containing "ALIVE" from the user's email address.
// This is a minimal IMAP client — only does LOGIN, SELECT, SEARCH, LOGOUT.
func CheckIMAPForAlive(cfg *SwitchConfig, since time.Time) (bool, error) {
	if cfg.IMAPServer == "" {
		return false, nil
	}

	port := cfg.IMAPPort
	if port == 0 {
		port = 993
	}

	addr := fmt.Sprintf("%s:%d", cfg.IMAPServer, port)

	// Connect with TLS
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{
		ServerName: cfg.IMAPServer,
	})
	if err != nil {
		return false, fmt.Errorf("IMAP connect: %w", err)
	}
	defer conn.Close()
	// Bound every read/write in the session; without this a stalled server hangs
	// readLine indefinitely.
	_ = conn.SetDeadline(time.Now().Add(imapTimeout))

	reader := bufio.NewReader(conn)

	// Read server greeting
	if _, err := readLine(reader); err != nil {
		return false, fmt.Errorf("IMAP greeting: %w", err)
	}

	// LOGIN
	if err := imapCommand(conn, reader, "A001", fmt.Sprintf("LOGIN %s %s", imapQuote(cfg.SMTPUsername), imapQuote(cfg.SMTPPassword))); err != nil {
		return false, fmt.Errorf("IMAP login: %w", err)
	}

	// SELECT INBOX
	if err := imapCommand(conn, reader, "A002", "SELECT INBOX"); err != nil {
		return false, fmt.Errorf("IMAP select: %w", err)
	}

	// SEARCH for emails with subject ALIVE since the last check-in, from the
	// owner's own address. FROM only matches the header, which is spoofable, but a
	// spoofed ALIVE merely delays release (erring toward not disclosing); the
	// filter stops anyone who can just drop an ALIVE email into the inbox from
	// suppressing the switch.
	sinceStr := since.Format("02-Jan-2006")
	searchCmd := fmt.Sprintf("SEARCH SINCE %s SUBJECT ALIVE", sinceStr)
	if cfg.UserEmail != "" {
		searchCmd = fmt.Sprintf("SEARCH SINCE %s FROM %s SUBJECT ALIVE", sinceStr, imapQuote(cfg.UserEmail))
	}
	searchResult, err := imapCommandWithResult(conn, reader, "A003", searchCmd)
	if err != nil {
		// Non-fatal — logout and return false
		imapCommand(conn, reader, "A004", "LOGOUT")
		return false, fmt.Errorf("IMAP search: %w", err)
	}

	// LOGOUT
	imapCommand(conn, reader, "A099", "LOGOUT")

	// Parse search results — look for any message IDs
	return len(searchResult) > 0, nil
}

func imapCommand(conn net.Conn, reader *bufio.Reader, tag, command string) error {
	_, err := fmt.Fprintf(conn, "%s %s\r\n", tag, command)
	if err != nil {
		return err
	}

	// Read until we get the tagged response
	for {
		line, err := readLine(reader)
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, tag+" OK") {
			return nil
		}
		if strings.HasPrefix(line, tag+" NO") || strings.HasPrefix(line, tag+" BAD") {
			return fmt.Errorf("IMAP error: %s", line)
		}
	}
}

func imapCommandWithResult(conn net.Conn, reader *bufio.Reader, tag, command string) ([]string, error) {
	_, err := fmt.Fprintf(conn, "%s %s\r\n", tag, command)
	if err != nil {
		return nil, err
	}

	var results []string
	for {
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, "* SEARCH") {
			// Parse message IDs from "* SEARCH 1 2 3"
			parts := strings.Fields(line)
			if len(parts) > 2 {
				results = parts[2:] // message IDs
			}
		}
		if strings.HasPrefix(line, tag+" OK") {
			return results, nil
		}
		if strings.HasPrefix(line, tag+" NO") || strings.HasPrefix(line, tag+" BAD") {
			return nil, fmt.Errorf("IMAP error: %s", line)
		}
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
