package simenv_test

import (
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/simenv"
)

// The actors must be constructible with no testing.TB plumbing — that is the whole
// point of this package (kawarimi demo builds them at runtime).
func TestMailServerWorksWithoutTestingTB(t *testing.T) {
	m, err := simenv.StartMail()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// The PRODUCTION send path, straight through the mock.
	sc := deadswitch.DefaultSwitchConfig()
	sc.SMTPServer = m.Host
	sc.SMTPPort = m.Port
	sc.SMTPUsername = "bot@simenv"
	sc.SMTPPassword = "pw"
	sc.SenderEmail = "bot@simenv"
	if err := deadswitch.SendEmail(sc, []string{"heir@simenv"}, "hello", "body line"); err != nil {
		t.Fatalf("SendEmail through the mock: %v", err)
	}
	if !m.SentTo("heir@simenv") || m.Last().Subject != "hello" {
		t.Errorf("mock did not capture the message: %+v", m.Last())
	}
}

func TestRepoFileEmptyRemote(t *testing.T) {
	dir := t.TempDir()
	if err := simenv.InitBareRepo(dir); err != nil {
		t.Fatal(err)
	}
	content, ok, err := simenv.RepoFile(dir, "anything")
	if err != nil || ok || content != "" {
		t.Errorf("empty remote: got (%q, %v, %v), want (\"\", false, nil)", content, ok, err)
	}
}

// An unsupported if: clause must surface as an error, never a panic — the runner
// now executes inside a long-lived demo server.
func TestRunDMSWorkflowUnsupportedIfErrors(t *testing.T) {
	if err := simenv.WorkflowRunnerSupported(); err != nil {
		t.Skip(err.Error())
	}
	yaml := `
jobs:
  check:
    steps:
      - name: Weird step
        if: fancy(expression) || other
        run: |
          true
`
	_, err := simenv.RunDMSWorkflow(yaml, nil, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not in the supported grammar") {
		t.Fatalf("expected a grammar error, got %v", err)
	}
}

// The Telegram alert step's real if: clause uses || and parentheses; the evaluator
// must handle the exact grammar the template generates.
func TestEvalWorkflowIfSupportsTemplateGrammar(t *testing.T) {
	if err := simenv.WorkflowRunnerSupported(); err != nil {
		t.Skip(err.Error())
	}
	yaml := `
jobs:
  check:
    steps:
      - name: Telegram-style guard
        if: steps.checkin.outputs.status != 'ok' || (steps.checkin.outputs.days_since >= 14 && steps.checkin.outputs.days_since < 30)
        env:
          MARK: hit
        run: |
          echo "ran" > "$GITHUB_OUTPUT.marker"
          true
`
	// With no checkin outputs, status resolves empty != 'ok' -> the step runs.
	res, err := simenv.RunDMSWorkflow(yaml, nil, t.TempDir())
	if err != nil {
		t.Fatalf("RunDMSWorkflow: %v", err)
	}
	if len(res.StepsRun) != 1 {
		t.Fatalf("guard with || and parens must run the step, ran: %v", res.StepsRun)
	}
}
