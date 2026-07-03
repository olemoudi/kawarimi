# kawarimi

An encrypted **digital-legacy vault**. You keep instructions, credentials, and
documents encrypted while you are alive; if you die or become permanently
incapacitated, a dead man's switch delivers to a family member exactly what they
need to open the package — and nothing before then.

Two goals drive every design decision:

1. **No unauthorized disclosure while you are alive and capable.**
2. **Easy for the recipient** — a possibly non-technical family member must be
   able to open the package with a guided, plain-language wizard.

## How it works

The vault is encrypted with [age](https://github.com/FiloSottile/age) (X25519).
The master key is wrapped in several slots: your **password + device key**, a
**recovery code**, and an **8-word mnemonic** (your paper backup).

For the recipient, kawarimi uses a **key split** so that no single secret — and
no secret you have to hand out early — can open the vault before the switch
fires. Three things are required, held by three different parties/places:

| Secret | Who holds it | When the recipient gets it |
| --- | --- | --- |
| **Sealed payload** (`sealed_payload.age`) | shipped inside the package (public) | already in the download |
| **DMS key** (32 random bytes) | the dead man's switch | emailed when the switch fires |
| **Recipient passphrase** (6 words) | a physical card you give them | in hand, from you |

The sealed payload is the 8-word mnemonic encrypted under *both* the DMS key and
the recipient passphrase. A leaked package + card cannot open it (no DMS key); a
leaked DMS key cannot open it (no card). Both are needed, and the DMS key is only
released after you stop checking in.

There are **two delivery channels**:

- **Cloud (GitHub Actions)** — the real post-mortem trigger. A workflow in a
  dedicated repo reads a heartbeat you push on each check-in and emails the DMS
  key to your recipients once you are overdue. This runs whether or not your
  machine is on.
- **Local (systemd timer)** — sends you reminders while your machine runs. By
  default it holds no key ("cloud-only") and never performs the final release.

## Quickstart (owner)

```sh
make build                      # or: go install github.com/olemoudi/kawarimi@latest
kawarimi init                   # creates the vault; prints your mnemonic, recovery
                                # code, and recipient passphrase ONCE — write them down

kawarimi add note "Bank accounts"
kawarimi add credential
kawarimi add document will.pdf

# Arm the dead man's switch (SMTP, recipients, thresholds, and the DMS repo):
kawarimi switch setup
kawarimi switch verify          # confirm it is armed and current
kawarimi checkin                # repeat on your schedule; also pushes the heartbeat

# Build the package your recipient will download (auto cross-compiles binaries):
kawarimi package build
```

Then, once:

- Print the **recipient passphrase** on a card and give it to your recipient.
- Create a **private, empty** GitHub repo for the switch and set its Actions
  secrets (`switch setup`/`switch seed` print the exact list, including the
  `DMS_KEY` value).
- Upload the package zip somewhere your recipient can reach and set that URL as
  `VAULT_PACKAGE_LOCATION`.

## For the recipient

When the switch fires, the recipient gets an email with a **key**. They:

1. Download the package and unzip it.
2. Run the bundled `kawarimi` program — on Windows they can **double-click** it;
   on macOS/Linux they run `./kawarimi-<os> open`. (Bare `kawarimi` launches the
   same wizard automatically when it is sitting next to a package.)
3. Paste the **key** from the email and type the **words** from the card.
4. The decrypted files appear in a `decrypted/` folder; `INDEX.md` lists
   everything.

All recipient-facing text (package instructions, the release email, the wizard)
is bilingual: **Spanish first, then English**.

## Threat model (summary)

- **Attacker with the public package + the card, owner alive:** cannot open it —
  the DMS key has not been released.
- **Attacker who compromises the owner's machine:** in the default *cloud-only*
  mode the machine holds no DMS key, so this does not yield it. (Use full-disk
  encryption regardless.)
- **Attacker who intercepts the release email (DMS key) only:** cannot open the
  vault without the physical card.
- **False trigger reaching the intended recipients:** low severity — the key
  alone opens nothing. Rotate with `kawarimi switch rekey` only if the key
  reached someone beyond your recipients.

## Operational constraints

- The DMS repo must be a **separate, private, empty** GitHub repo (no README) so
  the pushed workflow lands on the default `main` branch and actually schedules.
- Keep `FinalDays` well under ~60 — GitHub auto-disables scheduled workflows
  after ~60 days of repo inactivity; `switch verify` warns if it is too high.
- The SSH key used for check-ins must be passphrase-less (the systemd timer runs
  unattended).

## Building

```sh
make build       # local binary, version stamped from git
make test        # go test -short ./...
make cross       # recipient binaries for all platforms into dist/
```

Requires Go 1.25+.

## License

MIT — see [LICENSE](LICENSE).
