# Threat model

This document records the security threat-modelling decisions behind kawarimi:
who the adversaries are, what each one can and cannot achieve, why the design is
the way it is, and — just as important — the caveats and accepted risks. It
complements [ARCHITECTURE.md](ARCHITECTURE.md), which describes the mechanisms;
this file describes the adversaries the mechanisms were chosen against.
User-facing summaries stay in [README.md](README.md#threat-model-summary).

**Keeping it current:** any change to cryptography, KDF parameters, the
key-split, a release path, the password-strength policy, or a new
network-facing surface must update this file in the same change (see the
CLAUDE.md "Documentation" rule and §9 below).

---

## 1. What kawarimi protects (assets)

| Asset | Sensitivity | Where it lives |
| --- | --- | --- |
| Vault contents (notes, credentials, documents) | The crown jewels | age-encrypted files in the vault dir; encrypted copies in the package and optional vault repo |
| Master key (MK) / age identity | Equivalent to the contents | Only ever wrapped: per-slot in `vault_header.json` |
| 8-word mnemonic (88 bits) | Opens the vault alone | Printed once at init; sealed (V4) inside `sealed_payload.age` |
| Owner password | ½ of an owner slot | The owner's head — **the only user-chosen secret, hence the strength meter** |
| Device key | Other ½ of an owner slot | `device.key`, Argon2id-encrypted under the owner password |
| Recovery code (128 bits) | With password: opens the vault | Printed once at init |
| Recipient passphrase (6 BIP39 words, ~66 bits) | ½ of the sealed payload | A physical card held by recipients |
| DMS key (256 bits) | Other ½ of the sealed payload | GitHub Actions secret (cloud-only default); emailed at release |
| SMTP credentials, GitHub PAT | Enable impersonation / cloud tampering | Actions secrets; PAT is transient (never stored) |
| Aliveness signal (heartbeat cadence) | Metadata: reveals the owner stopped checking in | `last_checkin` in the private DMS repo |

## 2. Security goals

1. **Confidentiality before the trigger, against every single party.** No single
   secret — and nothing the owner hands out early — opens the vault. This is the
   V4 key-split (ARCHITECTURE.md §4).
2. **Disclosure happens when it should.** After `FinalDays` of silence the
   recipients get the DMS key; any ambiguity (missing/unparseable heartbeat)
   alerts the owner and never releases: **fail-closed toward disclosure,
   fail-open toward owner alerting.**
3. **The recipient path works years later, offline, post-mortem.** The recipient
   binary is frozen: no network calls, no self-update, no external services at
   open time.
4. **Failures lose availability, never confidentiality.** A false trigger leaks
   a key that opens nothing by itself; a torn write, crash, or stale workflow
   degrades to an owner alert, not to plaintext.

## 3. The modeled attacker: $100,000/year of cracking

The design benchmark for every guessable secret is an adversary who sustains a
**$100k/year budget dedicated to offline brute force** against this one vault
(a hostile relative with means, a professional service they hire, a small
firm). Nation-state coercion, $0-day implants on the live machine, and rubber
hoses are explicitly out of scope — no password scheme survives those.

### 3.1 Cracking economics

One owner-slot guess costs one Argon2id evaluation at the owner profile
(`t=2, 256 MiB, p=4`) — measured at **~1.1 core-seconds** per guess
(`BenchmarkOwnerSlotKDF`, i5-8250U; re-run it if the parameters change):

- Cloud compute floors around **$0.01 per vCPU-hour** (spot/preemptible).
  3600 s ÷ 1.1 core-s ≈ 3,300 guesses per vCPU-hour → ≈ **330k guesses per
  dollar** → $100k/year buys ≈ 3.3×10¹⁰ guesses/year ≈ **2³⁵ per year**.
- We then grant the attacker a further **32× (2⁵)** advantage for custom
  memory-hard rigs, optimized kernels, and cheap power. That is deliberately
  generous: 256 MiB of Argon2id per guess is exactly the regime where GPU/ASIC
  speedups are smallest.
- **Budget: 2⁴⁰ guesses/year** (`crypto.AttackerGuessesPerYearLog2`). Expected
  crack time for an N-bit secret ≈ 2^(N−1−40) years.

| Entropy | Expected crack time at $100k/yr | Meter level |
| --- | --- | --- |
| 20 bits | seconds | 0 — very weak |
| 30 bits | ~4 hours | 0 / 1 boundary |
| 40 bits | ~6 months | 1 — weak |
| 45 bits | ~16 years | 2 — fair (**acceptance floor**) |
| 60 bits | ~500,000 years | 3 — strong |
| 75 bits | ~1.7×10¹⁰ years | 4 — excellent |

### 3.2 How each secret measures up

| Secret | Entropy | KDF it hides behind | Verdict at 2⁴⁰/yr |
| --- | --- | --- | --- |
| Mnemonic | 88 bits | Argon2id 1 GiB, t=4 | ~2⁴⁷ years — unreachable |
| Recovery code | 128 bits | Argon2id 256 MiB (with password) | unreachable |
| DMS key | 256 bits | — (random key) | unreachable |
| Recipient passphrase | ~66 bits | age scrypt, and only useful **with** the DMS key | ~2²⁵ years — comfortable |
| Enrollment token code | 4 BIP39 words ≈ 44 bits | strong Argon2id | ~8 years offline — see caveat §7.6 |
| **Owner password** | **user-chosen** | Argon2id 256 MiB, t=2 | **the weak link — hence the meter** |

### 3.3 The password strength meter

The owner password is the only secret a human invents, so every place a *new*
vault-protecting password is chosen shows a live strength meter calibrated to
the table above (`crypto.EstimatePasswordStrength`):

- **Estimator.** A compact zxcvbn-style *lower bound*: rank-ordered top-10k
  common passwords, English + Spanish frequency dictionaries (the product is
  bilingual; both languages carry equal weight) plus the BIP39 list,
  l33t-decoding, sequences, repeats, date-aware digit runs, and a charset
  ceiling. It reports bits, a 0–4 level, and the expected crack time *for this
  attacker*. Wordlists are regenerated with `tools/genstrengthwords`.
- **Policy: advisory with friction, not a hard block.** Below level 2 (fair,
  ≥45 bits): the CLI (`init`, `passwd`, `migrate`, `device accept`) demands a
  typed confirmation on a TTY; the GUI wizard requires an explicit "use anyway"
  checkbox, enforced server-side in `/api/init` so the meter cannot be bypassed
  by a raw request. A hard block was rejected: the estimator is a heuristic and
  must not lock out, say, a Portuguese diceware phrase it cannot credit.
- **Where there is deliberately no meter:** unlock prompts (the password already
  exists), SMTP app-passwords and GitHub tokens (externally issued), stored
  third-party credentials (entry data, different threat), and the generated
  secrets (mnemonic, recovery code, recipient passphrase — machine-random and
  already sized in §3.2).

## 4. Adversary catalogue

What each adversary holds, and what stops them. "Card" = recipient passphrase.

| Adversary | Holds | Outcome |
| --- | --- | --- |
| Package host / anyone with the zip | encrypted vault + sealed payload | Nothing opens: needs DMS key **and** card; mnemonic slot is 88 bits |
| Recipient, owner alive | package + card | Cannot open — DMS key not yet released (`TestV4VaultAloneInsufficient`) |
| GitHub (the company) or a DMS-repo leak | DMS key, heartbeat metadata | Key alone opens nothing (`TestDMSOperatorCannotDecrypt`); learns aliveness cadence (§7.1) |
| Email provider / release-mail interceptor | DMS key (+ package location) | Needs the card (`TestV4DMSPlusVaultNeedPassphrase`) |
| Thief of the owner's (powered-off) laptop | `device.key`, header, vault, local switch state | Everything password-gated: Argon2id at 256 MiB per guess — §3.1 economics apply. FDE still recommended |
| Malware on the owner's **unlocked** machine | live session | Out of scope: can read whatever the owner can. Cloud-only mode still denies it the DMS key |
| GitHub **account** compromise | can push a workflow that exfiltrates Actions secrets | Gets DMS key + SMTP creds → can trigger/suppress the switch and phish; **still cannot open the vault without the card** (§7.2) |
| Header/param tamperer | write access to vault files | MK-keyed HMAC + `ValidateArgon2Params` minimums fail closed (no KDF downgrade) |
| Local network attacker vs the GUI | browser context | Loopback-only bind, per-session token cookie, Host allowlist (DNS rebinding), strict CSP, Origin checks |
| Attacker guessing the vault password online | the GUI/CLI prompt | No lockout needed: every guess pays the full Argon2id cost locally; there is no remote guessing surface at all |

## 5. Trigger-integrity threats (dead man's switch)

The switch must neither fire early (confidentiality) nor fail to fire (the
product's whole point):

- **Early fire** requires the heartbeat to *parse* as ≥ `FinalDays` old. A
  missing or corrupt heartbeat alerts the owner instead of releasing. A
  clock-jump on the owner machine cannot fake elapsed time locally (ratchet in
  the local evaluator); the cloud path trusts GitHub's clock.
- **Suppression** (attacker keeps checking in as a dead owner) requires the
  owner's SSH key — i.e. control of an owner machine; see machine-compromise
  rows above.
- **Silent decay** is treated as an attack by entropy: `switch verify` checks
  the remote heartbeat freshness, byte-compares the deployed workflow against
  the generator, warns when `FinalDays` approaches GitHub's ~60-day idle
  auto-disable, and flags outdated workflow versions (`DMSWorkflowVersion`).
- **Legacy payloads** (pre-V4 generations that emailed real secrets) were
  removed; a leftover one fails closed with a rekey alert to the owner and
  releases nothing (`TestLegacyPayloadNeverReleasesToRecipients`).

## 6. Design-decision record

Decisions that exist *because of* this threat model, with their rationale:

1. **Three-way key-split (V4).** No single party — cloud, recipient-pre-release,
   package host — holds enough to decrypt. Chosen over "email the mnemonic"
   (V2) and "seal under passphrase only" (V3), both of which had a
   single-secret failure mode.
2. **Cloud-only default.** The owner machine deletes its DMS key after seeding;
   machine compromise yields no release-capable secret. Local release is opt-in.
3. **Fail-closed toward disclosure.** Every ambiguous switch state resolves to
   "alert the owner, release nothing".
4. **Memory-hard KDF with enforced floors.** Argon2id profiles sized so §3.1
   economics hold; `ValidateArgon2Params` + MK-keyed header HMAC make parameter
   downgrade fail closed.
5. **Strength meter with a $100k/yr yardstick** (§3.3) — the human-chosen
   password is the residual weak link, so it gets measured against the same
   attacker as everything else, in both product languages.
6. **Frozen recipient path.** No network, no self-update, vendored deps,
   CGO-free static binaries: the open-the-vault path must survive a decade of
   ecosystem drift. Owner-side self-update is Ed25519-signed (key never in the
   repo).
7. **Generated workflow with no third-party actions.** Email via `curl`, actions
   SHA-pinned, `permissions: contents: read` — nothing a third party can
   deprecate or backdoor out from under the switch.
8. **Loopback GUI hardening** (token cookie, Host/Origin checks, CSP): the GUI
   handles the same secrets as the CLI, so it gets a real server-side security
   model even though it only binds 127.0.0.1.
9. **The printed recipient card carries only the recipient passphrase.** GUI
   printing is per-secret-block with CSS isolation (no whole-page print exists),
   and the card is a dedicated bilingual artifact — so the sheet the owner hands
   to a recipient can never also carry the mnemonic or recovery code. Pinned by
   `internal/gui/printcard_test.go`.
10. **Atomic writes + self-healing header backups.** Durability is a security
    property here: a bricked header equals losing the estate.
11. **Demo mode adds no real attack surface.** `kawarimi demo` is an owner-side
    subcommand running a loopback, token-gated server like the GUI; every actor
    is an in-process mock, all "secrets" are generated per run inside a temp
    HOME with test-grade KDF and are worthless outside it, the mock-API env
    overrides live only in the demo process, and **the recipient path gains no
    network calls** — the recipient binary and wizard are untouched.

## 7. Caveats and accepted risks

Known limitations, deliberately accepted — with the reasoning:

1. **Aliveness metadata leaks to GitHub.** The heartbeat cadence tells GitHub
   (or anyone with repo read access) when the owner stopped checking in.
   Accepted: the repo is private, and the alternative is running our own
   always-on infrastructure, which contradicts the longevity goal.
2. **GitHub account compromise yields the DMS key and SMTP credentials** (via a
   pushed exfiltration workflow) and full trigger control. Accepted residual:
   the vault still needs the physical card; mitigate with 2FA on the GitHub
   account and `switch rekey` if suspected. The alternative (no cloud trigger)
   removes the product's core capability.
3. **The release email is as secure as the recipients' inboxes.** Post-release,
   the DMS key sits in ordinary mailboxes indefinitely. Accepted: recipients
   are non-technical by assumption; the card remains the second factor.
4. **An unlocked, compromised owner machine is game over** for vault contents
   (not for the DMS key in cloud-only mode). Standard endpoint-security scope
   line; FDE and OS hygiene are the owner's job.
5. **The weak-password gate is overridable, and warn-only without a TTY.**
   User sovereignty: the estimator is a heuristic lower bound and must not hard-lock
   legitimate choices (e.g. non-EN/ES diceware). Scripted/piped stdin gets a
   warning instead of a prompt so unattended use never hangs. Consequence: a
   determined user can still pick "password123" — measured, warned, their call.
6. **Passwords in languages other than English/Spanish are over-scored.** The
   dictionaries cover the product's two languages; a common French word reads
   as random characters. Also, no user-specific dictionary (names, birthdays of
   *this* owner) — classic zxcvbn has the same gap.
7. **Enrollment token: ~44 bits + honest-client expiry.** The token embeds the
   raw MK under a 4-word code; the 10-minute validity is enforced only by
   honest clients, so a *captured* token is offline-crackable in ~8 expected
   years at the modeled budget. Accepted: the token transits between the
   owner's own devices, momentarily, by choice; treat it like a password.
8. **`ZeroBytes` is best-effort.** Go's GC can copy buffers and the OS can swap;
   memory hygiene reduces exposure windows but is not a guarantee. FDE plus
   encrypted swap is the real mitigation.
9. **The budget model ages.** 2⁴⁰/year assumes 2026 cloud prices and the current
   Argon2id profiles. Re-run `BenchmarkOwnerSlotKDF` and revisit §3.1 when
   parameters change or every few years; thresholds live in one place
   (`internal/crypto/strength.go`).
10. **Physical-world failure modes** — a lost/destroyed card, a recipient who
    predeceases the owner, an estate dispute — are handled operationally
    (`switch rekey`, reprint, re-package), not cryptographically.

## 8. Test anchors

The invariants above are pinned by tests (see CLAUDE.md "Testing"): the
key-split negatives (`TestV4VaultAloneInsufficient`,
`TestV4DMSPlusVaultNeedPassphrase`, `TestDMSOperatorCannotDecrypt`), fail-closed
switch behavior and legacy-payload handling (`internal/lifecycle`,
`TestLegacyPayloadNeverReleasesToRecipients`), KDF floors
(`ValidateArgon2Params` tests), header tamper/HMAC tests, the GUI security
middleware tests, and the strength-meter policy tests
(`internal/crypto/strength_test.go`, `passphrase_test.go`,
`TestInitWeakPasswordGate`). A security-relevant change without a matching test
is incomplete.

## 9. When to update this document

| If you change… | Revisit |
| --- | --- |
| Argon2id profiles, scrypt params, or any KDF | §3.1–§3.3 economics + re-run `BenchmarkOwnerSlotKDF` |
| The strength estimator, thresholds, or wordlists | §3.3, §7.5–7.6 |
| The key-split, sealing, or a release path | §1, §2, §4, §5, §6 |
| Secrets stored in GitHub / a new external service | §1, §4, §7 |
| A new network-facing surface (GUI endpoint, channel) | §4, §6 |
| Demo mode / the simenv actors | §6.11 |
| A security invariant test | §8 |
