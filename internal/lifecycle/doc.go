// Package lifecycle holds end-to-end vault lifecycle scenario tests. They drive the
// real kawarimi code (init, arm, check-in, evaluate, release, recipient decrypt,
// rekey) through the internal/testenv actor mocks (SMTP, Telegram, GitHub, the DMS
// git repo), so the whole dead man's switch flow is exercised unattended with no
// network and no credentials. All behavior lives in the _test.go files.
package lifecycle
