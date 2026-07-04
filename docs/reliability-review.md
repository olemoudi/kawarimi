# Reliability & longevity review (2026-07)

kawarimi is mission-critical: a failure means either a family never receives the
vault, or the key is disclosed while the owner is alive. It must also keep working
**unattended for years** with no maintenance. This document records a full review of
the switch engine, the crypto/persistence layer, and the CLI/GUI/recipient/build
surfaces, and the remediation applied in the same change set.

The cryptographic primitives were found sound (age v1 stable format, AES-256-GCM key
wrap, Argon2id with a downgrade floor, header HMAC). The risk was concentrated in
**persistence** (non-atomic writes over irreplaceable state) and a set of
**early-release / never-release** paths. All Critical/High/Medium findings below are
fixed, each with a regression or lifecycle test; the guiding rule was that a change
to disclosure behavior without a test is itself a bug.

Severity: **C**ritical (permanent data loss or wrong disclosure) · **H**igh · **M**edium · **L**ow.
Status: ✅ fixed · 🔒 fixed + test.

## Persistence & data safety

| # | Sev | Finding | Fix | Status |
|---|-----|---------|-----|--------|
| 1 | C | Every durable write was a bare `os.WriteFile`; a torn write of `vault_header.json` (holds all key slots + wrapped identity) permanently bricks the vault. | New `internal/atomicfile` (temp → fsync → rename, + `.bak`). All durable writes routed through it. `LoadHeader` self-heals from `vault_header.json.bak`. | 🔒 |
| 2 | C | Migration renamed v2 files over the v1 originals **before** persisting the header (the only copy of the new identity) → crash orphaned all data. | `internal/vault/migrate.go` now writes the header first, keeps v1 originals as `.v1bak`, verifies the migrated vault opens, then removes backups; rolls back on any failure. | ✅ |
| 3 | C | Seal side did not normalize the passphrase but unseal did; a `rekey` typed with a stray capital/space permanently locks out the family. | `SealMnemonicV4`/`UnsealMnemonicV4` both `NormalizeWords` (idempotent, backward-compatible). | 🔒 |
| 4 | H | Nothing verified the recipient path at runtime; `verify` checked entries only. | `SealAndInstallV4` now unseal-round-trips the payload before trusting it; new `kawarimi verify --recipient` runs the full DMS-key + card path on demand. | ✅ |
| 6 | H | Manifest rewritten non-atomically on every mutation; loss orphaned all entries with no recovery. | Atomic writes (finding 1) + `vault.RebuildManifest` and a `kawarimi repair` command that re-indexes decryptable entry files. | 🔒 |
| 7 | M | A lost/corrupt config could let a re-`init` overwrite an existing header. | `InitVault` refuses if `vault_header.json` already exists at the target (not just when the config loads). | 🔒 |
| 8 | M | `passwd` updates header + device.key as two non-atomic writes. | Both now go through atomic writes; the mismatch window is closed on the durable side (recovery code/mnemonic remain the escape hatch). | ✅ |
| 13 | L | Zip extraction had no size caps (zip-bomb / disk fill). | Per-entry (500 MiB) and total (2 GiB) decompressed-size caps in `ExtractPackage`. | ✅ |
| — | M | Filename sequence was a per-category **count**, so delete-then-add reused a number and overwrote a surviving entry (silent loss). | `NextSeq` now returns `max(existing sequence)+1`. | 🔒 |

## Dead man's switch correctness

| # | Sev | Finding | Fix | Status |
|---|-----|---------|-----|--------|
| S1 | H | `switch rekey` rotated the switch identity but never re-saved `switch-config.age`, silently breaking verify/evaluate afterward. | rekey loads the config before rotating and re-saves it after. | 🔒 |
| S2 | H | `switch disable` removed only local files; the cloud workflow kept firing → release while alive. | `disable` now neutralizes the cloud (removes the workflow, pushes a far-future heartbeat, clears `DMSRemote`) and refuses to claim success if it can't reach the repo. | ✅ |
| S3 | H | CLI `switch setup` silently coerced bad threshold input to `0` → immediate release. | Every threshold parse is checked; `warning1 < warning2 < final` and positivity enforced. | ✅ |
| S4 | H | A forward clock jump drove local `Evaluate` straight to final release (local-release mode). | Clock-jump ratchet: a real-time `first-overdue-at` anchor is required before a local final release; reset when healthy. Cloud path (correct NTP) unaffected. | 🔒 |
| S5 | M | `/alive` auto-checkin refreshed local but swallowed cloud-push failure → split brain. | `autoCheckin` alerts the owner (email/Telegram) when the cloud push fails. | 🔒 |
| S6 | M | `verify` was manual-only; `checkin` exited 0 even when the cloud push failed. | `evaluate` (systemd timer) now runs an auto remote-staleness check and alerts (deduped); a local check-in failure is always fatal, and a cloud-push failure exits non-zero. | 🔒 |

## Network robustness

| # | Sev | Finding | Fix | Status |
|---|-----|---------|-----|--------|
| N1 | M | SMTP: port 465 unsupported, one bad recipient blocked all, no send timeout. | `SendEmail` supports implicit TLS (465) and STARTTLS, sends per-recipient (partial failures logged, not fatal), and bounds the session with a deadline. | 🔒 |
| N2 | M | Telegram used the default HTTP client (no timeout); IMAP had no read deadline → hung evaluate. | Telegram uses a timeout client; IMAP sets a session deadline. IMAP LOGIN/SEARCH now use correct IMAP quoting. | ✅ |
| R9 | M | DMS-key input was strict base64 with only single-line trim; email whitespace/wrapping/zero-width chars broke it. | `DecodeDMSKeyLenient` strips whitespace + zero-width/BOM, accepts wrapping and the URL-safe alphabet. | 🔒 |

## Recipient path & longevity

| # | Sev | Finding | Fix | Status |
|---|-----|---------|-----|--------|
| CP2 | H | On macOS the wizard searched only cwd (`$HOME`), not the binary's dir → couldn't find the package. | `locateVault` searches cwd **and** the executable's directory. | 🔒 |
| P5 | H | Recipient unlock needs ~1 GiB Argon2; an old low-RAM machine OOM-crashes. | Best-effort low-RAM preflight warning (bilingual) + a minimum-RAM note in the recipient instructions. | ✅ |
| CP12 | L | The Windows "no vault" message flashed and vanished (no pause). | `pauseOnWindows` on the no-vault path too. | ✅ |
| L3 | H | The cloud release depended entirely on the third-party `dawidd6/action-send-mail` Action; its deletion or Node-runtime sunset would silently kill all delivery — including the failure alert. | The generated workflow sends email with **`curl`** (STARTTLS/smtps, port templated); the only remaining `uses:` is GitHub-owned `actions/checkout`. | 🔒 |
| L4 | M | Cloud workflow hardcoded SMTP port 587, ignoring the owner's configured port (465 users fail silently). | Port + scheme are templated from `SwitchConfig.SMTPPort`. | 🔒 |
| L11 | M | No vendoring — an owner rebuild years later depended on module proxies. | `go mod vendor` committed; `go build` works fully offline. | ✅ |
| GUI5 | H | The GUI idle watchdog (90 s) could kill the server mid package-build (cross-compile can exceed 90 s). | In-flight request counter: idle shutdown is deferred while any request is being served and the countdown restarts when it finishes (`TestIdleShutdownDeferredWhileRequestInFlight`). | 🔒 |
| B14 | L | Makefile `build`/`install` didn't force `CGO_ENABLED=0`. | Added, matching `cross` and goreleaser. | ✅ |

## Known follow-ups (not in this pass)

- **IMAP** — the minimal client still lacks literal (`{n}`) handling; failures are
  fail-safe (a missed `/alive` only delays release, never discloses).
- **Enrollment** — token acceptance uses the accepting machine's clock (>10 min skew
  falsely expires) and tokens aren't single-use within the window; documented, not
  yet hardened.

## How this is tested

Every fix ships with a test. The mocked-actor harness (`internal/testenv`) and the
end-to-end scenarios (`internal/lifecycle`) exercise the whole switch lifecycle
unattended; notable additions in this pass: header self-heal from backup, manifest
rebuild, migration integrity, the clock-jump ratchet, seal/unseal normalization
symmetry, lenient DMS-key decode, delete-then-add filename safety, SMTP
partial-recipient rejection and hang-timeout, the rekey config-survival regression,
and the curl-workflow golden + invariant checks (`go test` renders the workflow,
validates it as YAML, and `bash -n`-checks every step).
