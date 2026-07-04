package testenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is a miniature GitHub Actions runner for the generated dead man's
// switch workflow. The release decision — "the owner has been silent long enough,
// email the DMS key" — lives in bash inside that YAML, so golden-testing its text
// is not enough: these helpers parse the generated steps, resolve `${{ secrets.* }}`
// and `${{ steps.checkin.outputs.* }}`, evaluate each step's `if:` guard, and
// execute the `run:` scripts under bash with `curl` replaced by a capturing shim.
// What remains untestable is only GitHub's scheduler itself.
//
// The parser handles the template shapes kawarimi generates (see
// internal/deadswitch/github.go), not arbitrary workflow YAML — if the template
// grows syntax the parser does not know, tests fail loudly rather than skipping
// steps silently.

// WorkflowMail is one email the simulated workflow sent through the curl shim.
type WorkflowMail struct {
	From string
	To   []string
	Raw  string // the .eml exactly as curl would have transmitted it (CRLF)
}

// Subject returns the Subject header of the captured mail.
func (m WorkflowMail) Subject() string {
	for _, line := range strings.Split(m.Raw, "\r\n") {
		if strings.HasPrefix(line, "Subject: ") {
			return strings.TrimPrefix(line, "Subject: ")
		}
	}
	return ""
}

// Body returns the message body (after the blank line), LF-normalized.
func (m WorkflowMail) Body() string {
	_, body, ok := strings.Cut(m.Raw, "\r\n\r\n")
	if !ok {
		return ""
	}
	return strings.ReplaceAll(body, "\r\n", "\n")
}

// WorkflowResult reports what a simulated cron run did.
type WorkflowResult struct {
	Outputs  map[string]string // the checkin step's outputs (status, days_since)
	StepsRun []string          // names of run: steps whose if: matched
	Mails    []WorkflowMail    // every email "sent" during the run
}

// MailsTo returns the captured mails addressed to addr.
func (r *WorkflowResult) MailsTo(addr string) []WorkflowMail {
	var out []WorkflowMail
	for _, m := range r.Mails {
		for _, to := range m.To {
			if to == addr {
				out = append(out, m)
			}
		}
	}
	return out
}

// wfStep is one parsed workflow step.
type wfStep struct {
	name string
	uses string
	id   string
	cond string // the if: expression, "" = always
	env  map[string]string
	run  string
}

// RequireWorkflowRunner skips the test unless bash + GNU date are available (the
// production workflow always runs on ubuntu-latest, so simulating it is only
// meaningful where the same tools exist).
func RequireWorkflowRunner(t testing.TB) {
	t.Helper()
	if err := exec.Command("bash", "-c", "date -d @0 +%s >/dev/null").Run(); err != nil {
		t.Skip("workflow simulation needs bash + GNU date (as on the ubuntu runner)")
	}
}

// RunDMSWorkflow executes the workflow's run: steps in order inside repoDir (a
// stand-in for the checked-out DMS repo), with the given Actions secrets. It fails
// the test if a step that should run exits non-zero.
func RunDMSWorkflow(t testing.TB, workflowYAML string, secrets map[string]string, repoDir string) *WorkflowResult {
	t.Helper()
	RequireWorkflowRunner(t)

	steps, err := parseWorkflowSteps(workflowYAML)
	if err != nil {
		t.Fatalf("parsing generated workflow: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("no steps parsed from the workflow — template shape changed?")
	}

	captureDir := t.TempDir()
	shimDir := t.TempDir()
	writeCurlShim(t, shimDir)

	res := &WorkflowResult{Outputs: map[string]string{}}
	for _, st := range steps {
		if st.run == "" {
			continue // uses: actions/checkout — repoDir already plays that role
		}
		if !evalWorkflowIf(st.cond, res.Outputs) {
			continue
		}
		res.StepsRun = append(res.StepsRun, st.name)

		outFile := filepath.Join(t.TempDir(), "github_output")
		if err := os.WriteFile(outFile, nil, 0600); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command("bash", "-c", st.run)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"GITHUB_OUTPUT="+outFile,
			"CURL_CAPTURE_DIR="+captureDir,
		)
		for k, v := range st.env {
			cmd.Env = append(cmd.Env, k+"="+resolveExpressions(v, secrets, res.Outputs))
		}
		combined, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("workflow step %q failed: %v\n%s", st.name, runErr, combined)
		}

		if st.id != "" {
			data, _ := os.ReadFile(outFile)
			for _, line := range strings.Split(string(data), "\n") {
				if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
					res.Outputs[k] = v
				}
			}
		}
	}

	res.Mails = collectCapturedMails(t, captureDir)
	return res
}

// writeCurlShim installs a fake curl that records its argv and the uploaded .eml.
func writeCurlShim(t testing.TB, dir string) {
	t.Helper()
	shim := `#!/usr/bin/env bash
# curl shim: capture the SMTP submission instead of talking to a network.
stamp=$(date +%s%N)
argf="$CURL_CAPTURE_DIR/call-$stamp.args"
printf '%s\n' "$@" > "$argf"
prev=""
for a in "$@"; do
  if [ "$prev" = "--upload-file" ]; then cp "$a" "$CURL_CAPTURE_DIR/call-$stamp.eml"; fi
  prev="$a"
done
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(shim), 0755); err != nil {
		t.Fatal(err)
	}
}

// collectCapturedMails reads the shim's capture files into WorkflowMails.
func collectCapturedMails(t testing.TB, captureDir string) []WorkflowMail {
	t.Helper()
	entries, err := os.ReadDir(captureDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".args") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // nanosecond stamps sort chronologically

	var mails []WorkflowMail
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(captureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		var m WorkflowMail
		for i, a := range args {
			switch a {
			case "--mail-from":
				if i+1 < len(args) {
					m.From = args[i+1]
				}
			case "--mail-rcpt":
				if i+1 < len(args) {
					m.To = append(m.To, args[i+1])
				}
			}
		}
		if raw, err := os.ReadFile(filepath.Join(captureDir, strings.TrimSuffix(name, ".args")+".eml")); err == nil {
			m.Raw = string(raw)
		}
		mails = append(mails, m)
	}
	return mails
}

// parseWorkflowSteps extracts the steps of the (single-job) generated workflow.
func parseWorkflowSteps(yaml string) ([]wfStep, error) {
	lines := strings.Split(yaml, "\n")
	stepStart := regexp.MustCompile(`^(\s*)- (name|uses):\s*(.*?)\s*(#.*)?$`)
	keyVal := regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*):\s*(.*?)\s*$`)

	var steps []wfStep
	i := 0
	for i < len(lines) {
		m := stepStart.FindStringSubmatch(lines[i])
		if m == nil {
			i++
			continue
		}
		st := wfStep{env: map[string]string{}}
		if m[2] == "name" {
			st.name = m[3]
		} else {
			st.uses = m[3]
		}
		fieldIndent := len(m[1]) + 2 // keys sit two spaces past the "- "
		i++
		for i < len(lines) {
			line := lines[i]
			if stepStart.MatchString(line) {
				break
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				i++
				continue
			}
			indent := len(line) - len(strings.TrimLeft(line, " "))
			if indent < fieldIndent {
				break // dedent: end of this step (and of the steps list)
			}
			kv := keyVal.FindStringSubmatch(line)
			if kv == nil || indent != fieldIndent {
				i++
				continue
			}
			switch kv[1] {
			case "id":
				st.id = kv[2]
			case "if":
				st.cond = kv[2]
			case "uses":
				st.uses = strings.SplitN(kv[2], "#", 2)[0]
			case "env":
				i++
				for i < len(lines) {
					env := keyVal.FindStringSubmatch(lines[i])
					envIndent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
					if env == nil || envIndent <= fieldIndent {
						break
					}
					st.env[env[1]] = env[2]
					i++
				}
				continue
			case "run":
				if kv[2] != "|" {
					return nil, fmt.Errorf("step %q: only block-scalar run: | is supported", st.name)
				}
				i++
				var block []string
				blockIndent := -1
				for i < len(lines) {
					line := lines[i]
					if strings.TrimSpace(line) == "" {
						block = append(block, "")
						i++
						continue
					}
					indent := len(line) - len(strings.TrimLeft(line, " "))
					if indent <= fieldIndent {
						break
					}
					if blockIndent == -1 {
						blockIndent = indent
					}
					if indent < blockIndent {
						break
					}
					block = append(block, line[blockIndent:])
					i++
				}
				st.run = strings.Join(block, "\n")
				continue
			}
			i++
		}
		steps = append(steps, st)
	}
	return steps, nil
}

// resolveExpressions substitutes ${{ secrets.X }} and ${{ steps.checkin.outputs.X }}.
func resolveExpressions(v string, secrets, outputs map[string]string) string {
	expr := regexp.MustCompile(`\$\{\{\s*([A-Za-z0-9_.]+)\s*\}\}`)
	return expr.ReplaceAllStringFunc(v, func(m string) string {
		ref := expr.FindStringSubmatch(m)[1]
		switch {
		case strings.HasPrefix(ref, "secrets."):
			return secrets[strings.TrimPrefix(ref, "secrets.")]
		case strings.HasPrefix(ref, "steps.checkin.outputs."):
			return outputs[strings.TrimPrefix(ref, "steps.checkin.outputs.")]
		default:
			return "" // unknown context: empty, like Actions does for unset values
		}
	})
}

// evalWorkflowIf evaluates the restricted if: grammar the template uses:
// clauses of `term OP term` joined by &&, where OP is ==, !=, >= or <.
func evalWorkflowIf(cond string, outputs map[string]string) bool {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return true
	}
	clause := regexp.MustCompile(`^(\S+)\s*(==|!=|>=|<)\s*(\S+)$`)
	for _, part := range strings.Split(cond, "&&") {
		m := clause.FindStringSubmatch(strings.TrimSpace(part))
		if m == nil {
			panic(fmt.Sprintf("workflow if: clause %q not in the supported grammar", part))
		}
		left := resolveIfTerm(m[1], outputs)
		right := resolveIfTerm(m[3], outputs)
		ln, lerr := strconv.Atoi(left)
		rn, rerr := strconv.Atoi(right)
		numeric := lerr == nil && rerr == nil
		ok := false
		switch m[2] {
		case "==":
			ok = left == right
		case "!=":
			ok = left != right
		case ">=":
			ok = numeric && ln >= rn
		case "<":
			ok = numeric && ln < rn
		}
		if !ok {
			return false
		}
	}
	return true
}

func resolveIfTerm(term string, outputs map[string]string) string {
	if strings.HasPrefix(term, "steps.checkin.outputs.") {
		return outputs[strings.TrimPrefix(term, "steps.checkin.outputs.")]
	}
	return strings.Trim(term, "'")
}
