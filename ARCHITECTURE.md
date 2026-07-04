# Architecture

This document explains how **kawarimi** works internally and *why* it is built
the way it is. It is written for contributors (human or agent) who need to change
the code without re-deriving the design from scratch.

For end-user instructions (owner quickstart, recipient steps, threat-model
summary) see [README.md](README.md). This document goes one level deeper and
does not repeat the quickstart.

> **Keeping this current:** when you change the architecture, security design,
> the dead man's switch flow, the package layout, an on-disk format, or the
> usage flow, update this file **and** [docs/usage-flow.md](docs/usage-flow.md)
> in the same change. See [§16](#16-keeping-this-document-current).

---

## 1. Overview

kawarimi is an encrypted **digital-legacy vault** with a **dead man's switch
(DMS)**. The owner stores notes, credentials, and documents encrypted while
alive; if they stop "checking in" (die or become incapacitated), a cloud
workflow emails a family recipient the one missing secret needed to open a
pre-distributed package — and nothing before then.

Two goals drive every design decision:

1. **No unauthorized disclosure while the owner is alive and capable.**
2. **Trivial for a non-technical recipient** — a guided, bilingual (Spanish
   first, then English) plain-language wizard.

The name ("kawarimi", Japanese *body-substitution*) is only branding; it is not
a code concept.

It is a **single Go binary** (`github.com/olemoudi/kawarimi`, Go 1.25+). The
same binary is the owner's CLI, the owner's TUI, and — when run next to a
package — the recipient's wizard. There is **no custom server**: the "server" is
generated GitHub Actions YAML running in a repository the owner owns.

---

## 2. System model / actors

| Actor | What it is | Holds |
| --- | --- | --- |
| **Owner** | Creates and maintains the vault, checks in on a schedule | `~/.kawarimi/` app state; the printed mnemonic / recovery code / recipient passphrase |
| **Owner device(s)** | Each enrolled machine has its own password + random device key and its own owner slot | `device.key` (encrypted at rest) |
| **Cloud DMS** | A **separate, private, empty** GitHub repo running a scheduled Actions workflow — the real post-mortem trigger; runs whether or not the owner's machine is on | `last_checkin` heartbeat, `deadman.yml`, and Actions **secrets** (incl. `DMS_KEY`) |
| **Local systemd timer** | `kawarimi switch evaluate` run unattended; sends the owner reminders. In the default *cloud-only* mode it holds no key and never releases | encrypted local switch config |
| **Recipient** | Non-technical family member; runs the bundled binary in wizard mode | the release email (DMS key) + a physical card (recipient passphrase) |
| **SMTP / Telegram / IMAP** | Delivery + reply channels | — |

There is no in-process RPC. The system is a **layered monolith** (CLI → domain
packages) plus out-of-process integration: git-over-SSH to the cloud, SMTP for
email, and file transfer (the package zip) to the recipient.

---

## 3. Repository layout

Entry point: [`main.go`](main.go) → `cmd.Execute()` (Cobra). With no subcommand,
[`cmd/root.go`](cmd/root.go) detects a "recipient context" (interactive TTY, no
owner `device.key`, a nearby `sealed_payload.age`) and auto-launches the wizard,
so double-clicking the binary inside an extracted package "just works".

| Path | Responsibility |
| --- | --- |
| `cmd/` | Cobra command layer, one file per command (`init`, `add`, `list`, `show`, `edit`, `remove`, `export`, `status`, `verify`, `passwd`, `migrate`, `open`, `tui`, `gui`, `sync`, `checkin`, `device`, `switch`, `package`) |
| `internal/vault/` | The vault model: multi-slot key header, entries, manifest, packaging, recipient cross-compile, V4 sealed-open, v1→v2 migration, device enrollment |
| `internal/crypto/` | All cryptographic primitives (age wrappers, Argon2id + HKDF, AES-GCM keywrap, mnemonic, recovery code, recipient passphrase, device key, V4 sealing, memory zeroing) |
| `internal/deadswitch/` | The DMS engine: stage evaluation & release, check-in + heartbeat push, GitHub Actions workflow generation, systemd units, SMTP/Telegram/IMAP, health verification |
| `internal/setup/` | Onboarding orchestration (`InitVault`, `SealAndInstallV4`, `StoreSwitchPayloadForMode`, `SeedSwitch`) shared by the CLI and the GUI so the two cannot drift |
| `internal/github/` | Minimal GitHub REST client: create the private DMS repo, set Actions secrets (sealed with `nacl/box`) — pure `net/http`, no CGo |
| `internal/gui/` | The browser owner console: a loopback HTTP server + embedded SPA + JSON API over the same `internal/*` APIs (see §11) |
| `internal/sync/` | `git.go` (go-git push/fetch/reset over SSH) and `usb.go` (copy to USB) |
| `internal/config/` | Non-sensitive JSON config at `~/.kawarimi/config.json`; derives app-dir and DMS-clone paths |
| `internal/recipient/` | The bilingual, plain-stdin recipient wizard |
| `internal/copytext/` | Centralized bilingual copy (package instructions, release email body) — single source of truth |
| `internal/tui/` | Optional owner-facing Bubble Tea terminal UI (Elm-style `Update`/`Msg`) over the same vault APIs |

`internal/*` is deliberately not importable from outside the module.

---

## 4. The V4 key-split (core security guarantee)

The recipient path is designed so that **no single secret — and nothing the
owner must hand out early — can open the vault before the switch fires.** Three
things are required, held by three different parties/places:

| Secret | Who holds it | Delivered to recipient |
| --- | --- | --- |
| **Sealed payload** (`sealed_payload.age`) | shipped inside the package (public) | already in the download |
| **DMS key** (32 random bytes) | the cloud dead man's switch | emailed when the switch fires |
| **Recipient passphrase** (6 words, ~66 bits) | a physical card the owner gives them | in hand, from the owner |

The sealed payload is the vault's 8-word mnemonic (its entropy) encrypted with
age **scrypt** under a combined passphrase:

```
combined = base64(DMS key) + ":" + recipientPassphrase
```

See `crypto.CombinePassphrase` / `SealMnemonicV4` / `UnsealMnemonicV4` in
[`internal/crypto/sealed.go`](internal/crypto/sealed.go). Unsealing needs
**both** halves, so:

- **Leaked package + card** (no DMS key) → cannot open it.
- **Leaked DMS key alone** (no card) → cannot open it.

The recipient's end-to-end open is `vault.OpenSealedV4` in
[`internal/vault/sealed.go`](internal/vault/sealed.go): read `sealed_payload.age`
→ `UnsealMnemonicV4` → recover entropy → mnemonic words → `header.OpenWithMnemonic`
→ age identity → `OpenV2`. The passphrase is normalized (`crypto.NormalizeWords`)
so a card typed with stray capitals or spaces still matches.

The cloud DMS only ever holds the DMS key — which is useless without the card —
so **no server or third party ever holds a decryption-capable secret.** This is
the sense in which the vault is end-to-end encrypted. These properties are
locked down by tests: `TestV4VaultAloneInsufficient`,
`TestV4DMSPlusVaultNeedPassphrase` (`internal/vault/sealed_integration_test.go`),
`TestV4CannotUnsealWithV3` (`internal/crypto/sealed_test.go`).

---

## 5. Vault & key management

The vault is encrypted with [age](https://github.com/FiloSottile/age) (X25519).
Key management lives in the header,
[`internal/vault/header.go`](internal/vault/header.go) (`vault_header.json`,
version 2).

- A random 32-byte **master key (MK)** wraps the age X25519 **identity**
  (AES-256-GCM). The identity encrypts every entry; the public **recipient** is
  stored in the header.
- The MK is wrapped **independently in each slot** — each slot is a separately
  encrypted copy of the same MK, unlocked by different inputs:

| Slot (`SlotType`) | Unlock inputs | Slot key derivation |
| --- | --- | --- |
| **mnemonic** (slot 0) | 8-word mnemonic | `Argon2id(entropy)` → unwrap MK |
| **owner** (slot 1, +1 per extra device) | password **+** device key | `HKDF( Argon2id(password) ‖ deviceKey )` → unwrap MK |
| **recovery** (slot 2) | password **+** recovery code | `Argon2id(password ‖ recoveryCode)` → unwrap MK |

Both the password *and* the device key are required for an owner slot
(`crypto.DeriveOwnerSlotKey`, HKDF info `"kawarimi-owner-slot"`), so a stolen
password without the machine's `device.key` is useless. The recovery slot also
stores the recovery code re-encrypted under MK, used by the password-change flow.

**Header integrity:** the whole header (with the HMAC field cleared) is protected
by **HMAC-SHA256 keyed with the MK** (`computeHMAC` / `verifyHMAC`). Every
`OpenWith*` path verifies it *after* unwrapping MK, so tampering with a slot or
downgrading a stored KDF parameter fails closed.

On-disk **vault directory** (default `~/kawarimi-vault`):

```
vault_header.json    # multi-slot key header (no plaintext secrets; the crypto root)
manifest.age         # encrypted index of entries
notes/  credentials/  documents/    # per-entry age ciphertext: NNN-slug.ext.age
sealed_payload.age   # the V4 sealed mnemonic (shipped in the package)
last_checkin         # RFC3339 heartbeat timestamp (excluded from packages)
README.md  DECRYPT_INSTRUCTIONS.md   # recipient docs (regenerated at package time)
```

---

## 6. Cryptography reference

All primitives are `filippo.io/age`, Go stdlib, and `golang.org/x/crypto` — no
exotic crypto. Files: [`internal/crypto/`](internal/crypto/)
`{argon2,keywrap,crypto,sealed,mnemonic,recovery_code,recipient_passphrase,device_key}.go`.

| Purpose | Primitive | Notes |
| --- | --- | --- |
| Entry & identity encryption | age X25519 | `EncryptWithIdentity` / `DecryptWithIdentity` |
| Sealed payload | age scrypt | passphrase = `base64(dmsKey):recipientPassphrase` |
| Key wrapping (MK, identity, recovery code) | AES-256-GCM | random nonce, 32-byte key (`keywrap.go`) |
| Password/mnemonic KDF | Argon2id | see profiles below |
| Owner-slot combine | HKDF-SHA256 | binds password-key ‖ device-key |
| Header integrity | HMAC-SHA256 (MK-keyed) | verified on every open |
| Mnemonic | custom BIP39-style | 8 words = 88 bits (11 bytes entropy), 11 bits/word |
| Recipient passphrase | 6 BIP39 words | ~66 bits; normalized on input |
| Recovery code | 128-bit | base32, no padding, dashed |
| DMS key | 32 random bytes | base64 for email |

**Argon2id profiles** ([`internal/crypto/argon2.go`](internal/crypto/argon2.go)):

| Profile | time | memory | threads | Rationale |
| --- | --- | --- | --- | --- |
| Mnemonic slot | 4 | 1 GiB | 4 | Deliberately heavy (~5–10 s); rare receiver access |
| Owner / recovery / device-key | 2 | 256 MiB | 4 | Balanced for daily use (~1 s) |

**Downgrade protection:** `ValidateArgon2Params` enforces minimums (time ≥ 1,
memory ≥ 64 MiB, threads ≥ 1) at every derive, so an attacker cannot weaken
stored params to brute-force a slot; combined with the header HMAC, tampered
params also fail integrity. Key material is wiped throughout with
`crypto.ZeroBytes`.

---

## 7. Dead man's switch engine

Lives in [`internal/deadswitch/`](internal/deadswitch/).

**Escalation stages** (`switch.go`, `EvaluateStage`), by days since last
check-in — defaults `Warning1Days=14`, `Warning2Days=21`, `FinalDays=30`:

```
Normal  →  Warning1 (owner only)  →  Warning2 (owner only)  →  Final (release)
```

**Check-in = proof of life** (`checkin.go`, `RecordCheckin`): always writes the
local `last_checkin` (RFC3339), then resets the local DMS clone to remote and
**pushes the heartbeat** to the cloud DMS repo over SSH
([`internal/sync/git.go`](internal/sync/git.go), `~/.ssh/id_ed25519`,
passphrase-less). A failed push **warns loudly** — otherwise the switch could
fire while the owner is alive. The local write always succeeds even if the push
fails (tests `TestRecordCheckinTwoDevices`,
`TestRecordCheckinPushFailureReportsError`).

**Cloud workflow generation** ([`github.go`](internal/deadswitch/github.go),
`GenerateGitHubDMSWorkflow`): renders the standalone `deadman.yml` from a
`text/template` that uses **`[[ ]]` delimiters** so GitHub's `${{ }}` expressions
pass through untouched (this makes the earlier `fmt.Sprintf` %-escaping bug class
impossible to reintroduce). The workflow:

- runs daily (`cron: 0 9 * * *`) plus `workflow_dispatch`, with
  `permissions: contents: read` and **SHA-pinned** actions;
- classifies the heartbeat as `status = ok | missing | unparseable`;
- is **fail-closed toward disclosure, fail-open toward owner alerting**: if the
  heartbeat is missing/unparseable it emails the OWNER ("DEAD MAN'S SWITCH IS
  NOT ARMED") and **never releases**;
- emails the **DMS key** + `VAULT_PACKAGE_LOCATION` to `RECIPIENT_EMAILS` only
  when `status==ok && days_since >= FinalDays` (bilingual body from
  `copytext.ReleaseEmailBody`).

Everything sensitive lives in GitHub repo **secrets** (`SMTP_*`, `USER_EMAIL`,
`RECIPIENT_EMAILS`, `DMS_KEY`, `VAULT_PACKAGE_LOCATION`, optional `TELEGRAM_*`),
never in code.

**Local timer** ([`systemd.go`](internal/deadswitch/systemd.go)): a user timer
(`OnCalendar=daily`) runs `kawarimi switch evaluate` → `deadswitch.Evaluate`,
which also reads Telegram `/alive` and IMAP `ALIVE` replies as auto-check-ins
([`telegram.go`](internal/deadswitch/telegram.go),
[`imap.go`](internal/deadswitch/imap.go)) and sends owner reminders.

**Cloud-only default:** `switch setup` defaults to storing a `CLOUDONLY:` marker
locally (`StoreSwitchCloudOnly`), so the owner's machine holds **no DMS key**;
after seeding, `offerDeleteLocalDMSKey` removes the plaintext `~/.kawarimi/dms-key`.
Compromising the live machine therefore yields no key. The local `Evaluate` in
cloud-only mode only alerts the owner and never releases.

**Health check** ([`verify.go`](internal/deadswitch/verify.go), `switch verify`):
local check-in present; remote heartbeat present and not stale (>48 h lag ⇒
pushes not landing); the deployed workflow byte-matches the current generator;
warns if `FinalDays >= 55` (GitHub auto-disables idle scheduled workflows after
~60 days); flags legacy mnemonic-by-email payloads.

---

## 8. Device enrollment

Each machine has its own random `device.key` (32 bytes, Argon2id-encrypted at
rest under that device's password;
[`internal/crypto/device_key.go`](internal/crypto/device_key.go)) and its own
owner slot, so devices can be added or revoked independently and each may use a
different password. Two paths ([`cmd/device.go`](cmd/device.go),
[`internal/vault/enroll.go`](internal/vault/enroll.go)):

- **`device add`** — unlock via password + recovery code, then `AddOwnerSlot`.
- **`device enroll` → `device accept`** — a trusted device mints an
  **EnrollmentToken** = the MK encrypted (AES-256-GCM) under a **4-word BIP39
  code**, valid 10 minutes. Because the token embeds the raw MK and its expiry
  is only honest-client-enforced, it must resist offline brute force: the code
  is protected by strong Argon2id and the token is **format version 2**; v1
  tokens are rejected (`TestEnrollmentTokenRejectsV1`). Codes are normalized on
  input.

---

## 9. Data flow & the two repositories

Two GitHub repos are involved, deliberately separate (see
[`internal/config/config.go`](internal/config/config.go)):

| Repo | Config field | Contents | Required? |
| --- | --- | --- | --- |
| **Vault repo** | `SyncTargets.GitRemote` | the encrypted vault (a backup) | optional |
| **DMS heartbeat repo** | `SyncTargets.DMSRemote` | `last_checkin` + `.github/workflows/deadman.yml` only | required for the cloud channel |

The cloud release path never touches the vault repo; it only emails the DMS key
and the package location. The DMS repo must be **private, empty (no README)** so
the pushed workflow lands on the default `main` branch and actually schedules
(go-git initializes repos on `main`).

**Owner-machine private state** (`~/.kawarimi/`, mode 0700; all secret-bearing
files 0600):

```
config.json           # non-sensitive: vault dir, checkin interval, sync targets
device.key            # this device's encrypted device key
dms-key               # base64 DMS key (deleted in cloud-only mode after seeding)
switch-identity.key   # X25519 private key protecting local switch state
switch-payload.age    # encrypted local switch payload (CLOUDONLY:/DMSKEY:/legacy)
switch-config.age     # encrypted SMTP/recipient/threshold config
switch-triggered      # marker written after a local final release (prevents re-send)
dms-repo/             # local clone of the DMS heartbeat repo
```

The local switch config/payload are themselves age X25519-encrypted to a
locally generated keypair ([`x25519.go`](internal/deadswitch/x25519.go)).

---

## 10. Packaging & the recipient path

`kawarimi package build` ([`cmd/package.go`](cmd/package.go),
[`internal/vault/package.go`](internal/vault/package.go)):

- **auto cross-compiles** recipient binaries for `linux/amd64`, `linux/arm64`,
  `darwin/amd64`, `darwin/arm64`, `windows/amd64` (`CGO_ENABLED=0`) via a
  `go build` subprocess (kept in sync with the Makefile `PLATFORMS`);
- zips the **encrypted** vault (`vault_header.json`, `manifest.age`,
  `sealed_payload.age`, entry files — but **skips `last_checkin`**) plus the
  binaries plus a freshly injected bilingual `INSTRUCTIONS.md`;
- **contains no secrets.** Path-traversal guards protect add/export/extract.

The recipient runs the bundled binary (auto-wizard, or `./kawarimi-<os> open`).
The wizard ([`internal/recipient/recipient.go`](internal/recipient/recipient.go))
chooses a language (Spanish default), locates the vault, prompts for the **KEY**
(DMS key from email) and **WORDS** (passphrase from card), then
`OpenSealedV4` → `Export("decrypted/")` → opens `decrypted/INDEX.md`. The
non-wizard equivalent is `export --sealed`.

---

## 11. The browser GUI (owner console)

`kawarimi gui` ([`cmd/gui.go`](cmd/gui.go), [`internal/gui/`](internal/gui/))
starts a **local web server bound to `127.0.0.1`** and opens the default browser
to a single-page owner console. It is the beginner-friendly path to everything the
CLI does: a wizard to create a vault, arm the cloud dead man's switch, and build
the recipient package, plus day-to-day entry management and check-ins. It stays
pure-Go and CGo-free — the SPA (`internal/gui/web/*`) is compiled in with
`//go:embed`, so the single-binary, cross-compile-from-one-machine model is
unchanged.

**One shared code path.** The GUI does *not* re-implement setup. It calls the same
`internal/setup` orchestration the CLI uses (`InitVault`, `StoreSwitchPayloadForMode`,
`SeedSwitch`) plus `internal/vault`, `internal/deadswitch`, and `internal/github`.
The JSON API (`/api/init`, `/api/switch/setup`, `/api/switch/cloud`,
`/api/package/build`, `/api/entries…`, `/api/checkin`, `/api/switch/verify`) is a
thin adapter over those functions.

**Cloud automation (the one new capability).** Unlike the CLI's guided-manual flow,
the GUI's cloud step uses a GitHub personal access token to *create* the private
DMS repo and *set* its Actions secrets via the API. Secrets are encrypted client-side
with libsodium sealed boxes (`nacl/box.SealAnonymous`) against the repo's public key
before upload. It then calls `SeedSwitch` to push the workflow + heartbeat over SSH
(so the owner's SSH key must be registered with GitHub). In the default cloud-only
mode the local `dms-key` is deleted once the GitHub secret is set.

**Security model** (the server handles secrets, so this matters):

- **Loopback only** — binds `127.0.0.1` on a random ephemeral port; never `0.0.0.0`.
- **Per-session token** — a 256-bit token in the launch URL is exchanged for an
  `HttpOnly; SameSite=Strict` cookie and checked on every request with a
  constant-time compare, so other local users/pages cannot drive the API.
- **DNS-rebinding defense** — requests are rejected unless `Host` is
  `127.0.0.1:<port>`/`localhost:<port>`; mutating requests also check `Origin`.
- **Strict CSP + embedded assets** — `default-src 'none'` with `script/connect-src
  'self'` and no external hosts, so a hostile page cannot exfiltrate.
- **Transient secrets** — the GitHub token lives only in memory for the cloud step;
  the mnemonic/recovery/recipient-passphrase are shown once and never logged; unlock
  still goes through the device-key owner slot. No new on-disk formats are introduced.
- **Auto-shutdown** — a keepalive ping keeps the server alive while the tab is open;
  it exits on idle, on `Quit`, or on Ctrl-C.

The existing Bubble Tea `kawarimi tui` is unchanged and complementary.

---

## 12. Versioned evolution

The code carries backward-compatible generations, dispatched by a payload prefix
in `triggerFinalRelease` (`internal/deadswitch/switch.go`). The **current default
is a v2 vault + V4 cloud-only key-split**; older paths remain only for
back-compat and migration.

| Gen | Prefix | Release mechanism | Status |
| --- | --- | --- | --- |
| V1 | *(bare)* | passphrase-only vault | legacy |
| V2 | `MNEMONIC:` | emails the mnemonic outright | legacy (insecure; `switch verify` flags it) |
| V3 | `SEALED:` | sealed under passphrase only | legacy |
| V4 | `DMSKEY:` | emails the DMS key; sealed under DMS key + passphrase | current (local-release opt-in) |
| V4 | `CLOUDONLY:` | local machine holds no key; cloud releases | **current default** |

Vault format itself migrated v1 → v2 (single-passphrase → multi-slot identity);
see [`internal/vault/migrate.go`](internal/vault/migrate.go).

---

## 13. Build, CI, release

- **Build** ([`Makefile`](Makefile)): `make build` (version stamped from
  `git describe` via `-ldflags -X …cmd.version`), `make test`
  (`go test -short ./...`), `make cross` (recipient binaries into `dist/`),
  `make vet` / `fmt` / `fmt-check` / `install`. Requires **Go 1.25+**.
- **CI** ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)): on push to
  `main` and all PRs — gofmt check, `go vet`, `go build`, `go test -short` on
  Go 1.25. Actions **pinned to commit SHAs**, `permissions: contents: read`.
- **Release** ([`.goreleaser.yml`](.goreleaser.yml)): goreleaser v2,
  `CGO_ENABLED=0`, linux/darwin/windows × amd64/arm64, binaries named
  `kawarimi-{{.Os}}-{{.Arch}}` so `dist/` feeds straight into
  `kawarimi package build --binaries dist/`; draft release, checksums.

---

## 14. Threat model

See [README.md §Threat model](README.md#threat-model-summary) for the
attacker-scenario summary. At the code level, the following invariants are
enforced by tests and should stay true across changes:

- **DMS operator / cloud cannot decrypt** (`TestDMSOperatorCannotDecrypt`): the
  cloud only holds the DMS key, useless without the card.
- **Package + card alone is insufficient** while the owner is alive
  (`TestV4VaultAloneInsufficient`) — the DMS key has not been released.
- **DMS key + package still needs the passphrase**
  (`TestV4DMSPlusVaultNeedPassphrase`).
- **Header tampering / KDF downgrade fails closed** (HMAC verify + Argon2 param
  minimums).
- **Machine compromise yields no DMS key** in the default cloud-only mode.
- A **false trigger** is low severity — the DMS key alone opens nothing; recover
  with `switch rekey` only if the key reached someone beyond the intended
  recipients.

---

## 15. Usage flow

The full owner-to-recipient lifecycle. The canonical copy of this diagram
(plus a supplementary key-split view) lives in
[docs/usage-flow.md](docs/usage-flow.md); keep the two in sync.

```mermaid
sequenceDiagram
    autonumber
    actor Owner
    participant CLI as kawarimi (CLI / wizard)
    participant DMS as Cloud DMS (GitHub Actions)
    participant SMTP as SMTP server
    actor Recipient

    Note over Owner,Recipient: Phase 1 — Setup & arming (once)
    Owner->>CLI: kawarimi init
    CLI->>CLI: Generate master key, age identity, 8-word mnemonic,<br/>recovery code, 6-word recipient passphrase
    CLI->>CLI: Seal V4 payload (mnemonic under DMS key + passphrase)<br/>→ sealed_payload.age
    CLI-->>Owner: Print mnemonic, recovery code, passphrase ONCE
    Owner->>CLI: kawarimi add note / credential / document
    Owner->>CLI: kawarimi switch setup / seed
    CLI->>DMS: Push deadman.yml + last_checkin (SSH)
    Owner->>DMS: Set Actions secrets (DMS_KEY, SMTP_*,<br/>RECIPIENT_EMAILS, VAULT_PACKAGE_LOCATION)
    Owner->>CLI: kawarimi package build
    CLI-->>Owner: package.zip (encrypted vault + binaries, no secrets)
    Owner->>Recipient: Hand over physical card (recipient passphrase)
    Owner->>Owner: Upload package.zip to VAULT_PACKAGE_LOCATION

    Note over Owner,Recipient: Phase 2 — Check-in loop (while alive)
    loop Every check-in interval
        Owner->>CLI: kawarimi checkin
        CLI->>CLI: Write local last_checkin
        CLI->>DMS: Push heartbeat over SSH
    end
    Note over DMS: Daily cron: quiet while current;<br/>Warning1 / Warning2 email the owner only

    Note over Owner,Recipient: Phase 3 — Overdue → final release
    DMS->>DMS: Daily cron reads last_checkin
    alt heartbeat missing / unparseable
        DMS->>SMTP: Alert OWNER "switch NOT armed"<br/>(fail-closed — no release)
    else days_since ≥ FinalDays
        DMS->>SMTP: Email DMS key + package location
        SMTP-->>Recipient: Release email (DMS key)
    end

    Note over Owner,Recipient: Phase 4 — Recipient opens the vault
    Recipient->>Recipient: Download & unzip package
    Recipient->>CLI: Run bundled binary (auto-wizard)
    Recipient->>CLI: Paste DMS key (email) + type words (card)
    CLI->>CLI: UnsealMnemonicV4 → mnemonic → OpenWithMnemonic
    CLI-->>Recipient: decrypted/ files + INDEX.md
```

---

## 16. Keeping this document current

This file is only useful if it stays true. When a change touches an area below,
update the named section here **and** [docs/usage-flow.md](docs/usage-flow.md) in
the same commit (the CLAUDE.md "Documentation" rule enforces this):

| If you change… | Update |
| --- | --- |
| package layout / a new `internal/*` package | §3 |
| the key-split, sealing, or recipient open path | §4, §10, and the diagram |
| the header/slot model or a KDF | §5, §6 |
| DMS stages, thresholds, workflow, or channels | §7, and the diagram |
| device enrollment | §8 |
| on-disk files / the two-repo split | §5, §9 |
| the browser GUI (server, security, or an API endpoint) | §11 |
| a payload prefix / a new generation | §12 |
| build, CI, or release config | §13 |
| a security invariant / test | §14 |
| the owner or recipient flow | §15 + `docs/usage-flow.md` (keep both diagrams byte-identical) |
