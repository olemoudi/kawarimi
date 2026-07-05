package simenv

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// This file is a miniature GitHub Actions runner for the generated dead man's
// switch workflow. The release decision — "the owner has been silent long enough,
// email the DMS key" — lives in bash inside that YAML, so golden-testing its text
// is not enough: these helpers parse the generated steps, resolve `${{ secrets.* }}`
// and `${{ steps.checkin.outputs.* }}`, evaluate each step's `if:` guard, and
// execute the `run:` scripts under bash with `curl` replaced by a capturing shim.
// What remains unsimulatable is only GitHub's scheduler itself.
//
// The parser handles the template shapes kawarimi generates (see
// internal/deadswitch/github.go), not arbitrary workflow YAML — if the template
// grows syntax the parser does not know, callers get a loud error rather than
// silently skipped steps.

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

// TelegramPing is one Telegram Bot API sendMessage the simulated workflow issued.
type TelegramPing struct {
	ChatID string
	Text   string
}

// WorkflowResult reports what a simulated cron run did.
type WorkflowResult struct {
	Outputs  map[string]string // the checkin step's outputs (status, days_since)
	StepsRun []string          // names of run: steps whose if: matched
	Mails    []WorkflowMail    // every email "sent" during the run
	Pings    []TelegramPing    // every Telegram message "sent" during the run
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

// WorkflowRunnerSupported reports whether this machine can faithfully mirror the
// production runner. The workflow always executes on ubuntu-latest, so the
// simulation is linux-only by declaration — Windows machines with Git Bash or
// WSL would pass a tool probe but run the scripts under a shell whose PATH and
// path semantics differ from the runner (and from the curl shim's assumptions).
func WorkflowRunnerSupported() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("workflow simulation mirrors the ubuntu runner; linux-only")
	}
	if err := exec.Command("bash", "-c", "date -d @0 +%s >/dev/null").Run(); err != nil {
		return fmt.Errorf("workflow simulation needs bash + GNU date (as on the ubuntu runner)")
	}
	return nil
}

// RunDMSWorkflow executes the workflow's run: steps in order inside repoDir (a
// stand-in for the checked-out DMS repo), with the given Actions secrets. It
// returns an error if the workflow cannot be parsed or a step that should run
// exits non-zero.
func RunDMSWorkflow(workflowYAML string, secrets map[string]string, repoDir string) (*WorkflowResult, error) {
	steps, err := parseWorkflowSteps(workflowYAML)
	if err != nil {
		return nil, fmt.Errorf("parsing generated workflow: %w", err)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("no steps parsed from the workflow — template shape changed?")
	}

	scratch, err := os.MkdirTemp("", "kawarimi-wf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	captureDir := filepath.Join(scratch, "capture")
	shimDir := filepath.Join(scratch, "shim")
	for _, d := range []string{captureDir, shimDir} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, err
		}
	}
	if err := writeCurlShim(shimDir); err != nil {
		return nil, err
	}

	res := &WorkflowResult{Outputs: map[string]string{}}
	for i, st := range steps {
		if st.run == "" {
			continue // uses: actions/checkout — repoDir already plays that role
		}
		match, err := evalWorkflowIf(st.cond, res.Outputs)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		res.StepsRun = append(res.StepsRun, st.name)

		outFile := filepath.Join(scratch, fmt.Sprintf("github_output_%d", i))
		if err := os.WriteFile(outFile, nil, 0600); err != nil {
			return nil, err
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
			return nil, fmt.Errorf("workflow step %q failed: %v\n%s", st.name, runErr, combined)
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

	res.Mails, res.Pings, err = collectCaptures(captureDir)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// writeCurlShim installs a fake curl that records its argv and the uploaded .eml.
func writeCurlShim(dir string) error {
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
	return os.WriteFile(filepath.Join(dir, "curl"), []byte(shim), 0755)
}

// collectCaptures reads the shim's capture files and classifies each curl call:
// SMTP submissions (--mail-from present) become WorkflowMails, Telegram Bot API
// calls (URL contains api.telegram.org) become TelegramPings.
func collectCaptures(captureDir string) ([]WorkflowMail, []TelegramPing, error) {
	entries, err := os.ReadDir(captureDir)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".args") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // nanosecond stamps sort chronologically

	var mails []WorkflowMail
	var pings []TelegramPing
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(captureDir, name))
		if err != nil {
			return nil, nil, err
		}
		args := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

		if isTelegramCall(args) {
			pings = append(pings, parseTelegramPing(args))
			continue
		}

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
		if m.From == "" && len(m.To) == 0 {
			continue // not an SMTP submission; unknown curl call
		}
		if raw, err := os.ReadFile(filepath.Join(captureDir, strings.TrimSuffix(name, ".args")+".eml")); err == nil {
			m.Raw = string(raw)
		}
		mails = append(mails, m)
	}
	return mails, pings, nil
}

func isTelegramCall(args []string) bool {
	for _, a := range args {
		if strings.Contains(a, "api.telegram.org") {
			return true
		}
	}
	return false
}

// parseTelegramPing extracts chat_id and text from a captured Telegram curl's
// --data-urlencode arguments.
func parseTelegramPing(args []string) TelegramPing {
	var p TelegramPing
	for i, a := range args {
		if a != "--data-urlencode" || i+1 >= len(args) {
			continue
		}
		k, v, ok := strings.Cut(args[i+1], "=")
		if !ok {
			continue
		}
		if dec, err := url.QueryUnescape(v); err == nil {
			v = dec
		}
		switch k {
		case "chat_id":
			p.ChatID = v
		case "text":
			p.Text = v
		}
	}
	return p
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
// clauses of `term OP term` (OP in ==, !=, >=, <) combined with &&, || and
// parentheses — exactly the shapes internal/deadswitch/github.go generates.
func evalWorkflowIf(cond string, outputs map[string]string) (bool, error) {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return true, nil
	}
	return evalIfExpr(cond, outputs)
}

func evalIfExpr(expr string, outputs map[string]string) (bool, error) {
	expr = strings.TrimSpace(expr)

	// || binds loosest.
	if parts := splitTopLevel(expr, "||"); len(parts) > 1 {
		for _, p := range parts {
			ok, err := evalIfExpr(p, outputs)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if parts := splitTopLevel(expr, "&&"); len(parts) > 1 {
		for _, p := range parts {
			ok, err := evalIfExpr(p, outputs)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
	if inner, ok := stripOuterParens(expr); ok {
		return evalIfExpr(inner, outputs)
	}
	return evalIfClause(expr, outputs)
}

// splitTopLevel splits expr on op occurrences outside any parentheses.
func splitTopLevel(expr, op string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(expr[i:], op) {
			parts = append(parts, expr[start:i])
			i += len(op) - 1
			start = i + 1
		}
	}
	return append(parts, expr[start:])
}

// stripOuterParens removes one pair of parentheses if they wrap the whole expression.
func stripOuterParens(expr string) (string, bool) {
	if !strings.HasPrefix(expr, "(") || !strings.HasSuffix(expr, ")") {
		return "", false
	}
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && i < len(expr)-1 {
			return "", false // the opening paren closes before the end
		}
	}
	return expr[1 : len(expr)-1], true
}

func evalIfClause(part string, outputs map[string]string) (bool, error) {
	clause := regexp.MustCompile(`^(\S+)\s*(==|!=|>=|<)\s*(\S+)$`)
	m := clause.FindStringSubmatch(strings.TrimSpace(part))
	if m == nil {
		return false, fmt.Errorf("workflow if: clause %q not in the supported grammar", part)
	}
	left := resolveIfTerm(m[1], outputs)
	right := resolveIfTerm(m[3], outputs)
	ln, lerr := strconv.Atoi(left)
	rn, rerr := strconv.Atoi(right)
	numeric := lerr == nil && rerr == nil
	switch m[2] {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	case ">=":
		return numeric && ln >= rn, nil
	case "<":
		return numeric && ln < rn, nil
	}
	return false, nil
}

func resolveIfTerm(term string, outputs map[string]string) string {
	if strings.HasPrefix(term, "steps.checkin.outputs.") {
		return outputs[strings.TrimPrefix(term, "steps.checkin.outputs.")]
	}
	return strings.Trim(term, "'")
}
