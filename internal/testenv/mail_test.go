package testenv

import (
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/deadswitch"
)

// TestMailServerCaptures proves the in-process SMTP mock speaks enough ESMTP for the
// standard library's smtp.SendMail + PlainAuth and captures the message faithfully.
func TestMailServerCaptures(t *testing.T) {
	m := StartMail(t)
	cfg := &deadswitch.SwitchConfig{
		SMTPServer:   m.Host,
		SMTPPort:     m.Port,
		SMTPUsername: "bot@test",
		SMTPPassword: "pw",
		SenderEmail:  "bot@test",
	}
	if err := deadswitch.SendEmail(cfg, []string{"a@test", "b@test"}, "Hello There", "line one\nline two"); err != nil {
		t.Fatalf("SendEmail: %v", err)
	}
	if m.Count() != 1 {
		t.Fatalf("expected 1 captured message, got %d", m.Count())
	}
	msg := m.Last()
	if msg.Subject != "Hello There" {
		t.Errorf("subject = %q, want 'Hello There'", msg.Subject)
	}
	if !strings.Contains(msg.Body, "line two") {
		t.Errorf("body missing content: %q", msg.Body)
	}
	if !m.SentTo("a@test") || !m.SentTo("b@test") {
		t.Errorf("recipients not captured: %+v", msg.To)
	}
}

// TestMailServerPartialRejection proves one bad recipient does not block delivery to
// the good ones — a stale family address must not stop the release reaching the rest.
func TestMailServerPartialRejection(t *testing.T) {
	m := StartMail(t)
	m.Reject("bad@test")
	cfg := &deadswitch.SwitchConfig{
		SMTPServer: m.Host, SMTPPort: m.Port,
		SMTPUsername: "bot@test", SMTPPassword: "pw", SenderEmail: "bot@test",
	}

	if err := deadswitch.SendEmail(cfg, []string{"good@test", "bad@test"}, "Release", "body"); err != nil {
		t.Fatalf("SendEmail should tolerate a partial rejection: %v", err)
	}
	if m.Count() != 1 {
		t.Fatalf("expected 1 delivered message, got %d", m.Count())
	}
	if !m.SentTo("good@test") {
		t.Error("good recipient did not receive the message")
	}
	if m.SentTo("bad@test") {
		t.Error("rejected recipient should not be in the delivered message")
	}

	// When every recipient is rejected, it is a hard error.
	m.Reject("good@test")
	if err := deadswitch.SendEmail(cfg, []string{"good@test", "bad@test"}, "S", "b"); err == nil {
		t.Error("expected an error when no recipient is accepted")
	}
}
