package testenv

import (
	"testing"

	"github.com/olemoudi/kawarimi/internal/simenv"
)

// The miniature GitHub Actions runner for the generated dead man's switch workflow
// lives in internal/simenv (it also powers `kawarimi demo`); these wrappers keep
// the loud-failure semantics tests rely on.

// WorkflowMail is one email the simulated workflow sent through the curl shim.
type WorkflowMail = simenv.WorkflowMail

// WorkflowResult reports what a simulated cron run did.
type WorkflowResult = simenv.WorkflowResult

// RequireWorkflowRunner skips the test unless this machine can faithfully mirror
// the production runner (linux + bash + GNU date, like ubuntu-latest).
func RequireWorkflowRunner(t testing.TB) {
	t.Helper()
	if err := simenv.WorkflowRunnerSupported(); err != nil {
		t.Skip(err.Error())
	}
}

// RunDMSWorkflow executes the workflow's run: steps in order inside repoDir (a
// stand-in for the checked-out DMS repo), with the given Actions secrets. It fails
// the test if the workflow cannot be parsed or a step that should run exits
// non-zero.
func RunDMSWorkflow(t testing.TB, workflowYAML string, secrets map[string]string, repoDir string) *WorkflowResult {
	t.Helper()
	RequireWorkflowRunner(t)
	res, err := simenv.RunDMSWorkflow(workflowYAML, secrets, repoDir)
	if err != nil {
		t.Fatalf("running DMS workflow: %v", err)
	}
	return res
}
