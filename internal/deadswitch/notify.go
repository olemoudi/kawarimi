package deadswitch

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// smtpTimeout bounds the whole SMTP session so a server that accepts the TCP
// connection and then stalls cannot hang the (possibly unattended) evaluate run.
// It is a var so tests can shorten it.
var smtpTimeout = 45 * time.Second

// SendEmail sends an email via SMTP using the switch configuration. It supports both
// implicit TLS (port 465) and STARTTLS (port 587/others), bounds the session with a
// timeout, and sends per-recipient so one bad address cannot block delivery to the
// rest (partial failures are logged, not fatal, as long as at least one is accepted).
func SendEmail(cfg *SwitchConfig, to []string, subject, body string) error {
	if cfg.SMTPServer == "" {
		return fmt.Errorf("SMTP server not configured")
	}
	if len(to) == 0 {
		return fmt.Errorf("no recipients")
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPServer, cfg.SMTPPort)
	msg := buildEmailMessage(cfg.SenderEmail, to, subject, body)

	client, err := dialSMTP(cfg, addr)
	if err != nil {
		return fmt.Errorf("connecting to SMTP server: %w", err)
	}
	defer client.Close()

	if cfg.SMTPUsername != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPServer)); err != nil {
				return fmt.Errorf("SMTP authentication: %w", err)
			}
		}
	}

	if err := client.Mail(cfg.SenderEmail); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}

	var accepted int
	var rcptErrs []string
	for _, r := range to {
		if err := client.Rcpt(r); err != nil {
			rcptErrs = append(rcptErrs, fmt.Sprintf("%s (%v)", r, err))
			continue
		}
		accepted++
	}
	if accepted == 0 {
		return fmt.Errorf("no recipient was accepted: %s", strings.Join(rcptErrs, "; "))
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finalizing message: %w", err)
	}
	_ = client.Quit()

	if len(rcptErrs) > 0 {
		// Delivered to at least one recipient — do not fail the whole send, but make
		// the partial failure visible in the journal.
		fmt.Fprintf(os.Stderr, "SMTP: delivered to %d of %d recipients; failed: %s\n",
			accepted, len(to), strings.Join(rcptErrs, "; "))
	}
	return nil
}

// dialSMTP connects and negotiates TLS: implicit TLS for port 465, opportunistic
// STARTTLS otherwise. The connection carries an absolute deadline for the session.
func dialSMTP(cfg *SwitchConfig, addr string) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: smtpTimeout}
	tlsCfg := &tls.Config{ServerName: cfg.SMTPServer}

	if cfg.SMTPPort == 465 {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if err != nil {
			return nil, err
		}
		_ = conn.SetDeadline(time.Now().Add(smtpTimeout))
		return smtp.NewClient(conn, cfg.SMTPServer)
	}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))
	client, err := smtp.NewClient(conn, cfg.SMTPServer)
	if err != nil {
		return nil, err
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(tlsCfg); err != nil {
			client.Close()
			return nil, fmt.Errorf("STARTTLS: %w", err)
		}
	}
	return client, nil
}

// sanitizeHeader strips CR/LF characters to prevent SMTP header injection.
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

func buildEmailMessage(from string, to []string, subject, body string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", sanitizeHeader(from)))
	sanitizedTo := make([]string, len(to))
	for i, t := range to {
		sanitizedTo[i] = sanitizeHeader(t)
	}
	sb.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(sanitizedTo, ", ")))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", sanitizeHeader(subject)))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}
