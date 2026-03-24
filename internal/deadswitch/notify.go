package deadswitch

import (
	"fmt"
	"net/smtp"
	"strings"
)

// SendEmail sends an email via SMTP using the switch configuration.
func SendEmail(cfg *SwitchConfig, to []string, subject, body string) error {
	if cfg.SMTPServer == "" {
		return fmt.Errorf("SMTP server not configured")
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPServer, cfg.SMTPPort)

	msg := buildEmailMessage(cfg.SenderEmail, to, subject, body)

	auth := smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPServer)

	err := smtp.SendMail(addr, auth, cfg.SenderEmail, to, []byte(msg))
	if err != nil {
		return fmt.Errorf("sending email: %w", err)
	}

	return nil
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
