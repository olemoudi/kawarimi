# Code signing: current state and the paths to warning-free binaries

Status (2026-07): **the distributed binaries are not OS-signed.** Windows shows
SmartScreen's "Windows protected your PC" and macOS Gatekeeper blocks the first
run. This document records why, what users see, and the exact steps to enable
real signing later — so it becomes a configuration exercise, not a research
project. Decision log: signing was evaluated and deliberately deferred
(2026-07-05); the mitigations below shipped instead.

## What users see today, and the mitigations that shipped

- **Windows (SmartScreen):** "unrecognized app" on first run of a downloaded
  binary. Mitigation: the package `INSTRUCTIONS.md`, the release notes, and the
  recipient docs all carry bilingual "More info → Run anyway" guidance.
- **macOS (Gatekeeper):** the run is blocked. **On macOS 15 (Sequoia) and newer,
  right-click → Open no longer bypasses Gatekeeper for unsigned programs** — the
  only path is System Settings → Privacy & Security → "Open Anyway", then run
  again. All shipped guidance says this (older-Mac right-click hint kept as a
  parenthetical).
- **Both:** `kawarimi package build` now prefers the **official published
  release binaries** — downloaded and verified against the Ed25519-signed
  `checksums.txt` (`selfupdate.FetchOfficialBinaries`) — over local
  cross-compiles. Kawarimi's Ed25519 chain proves authenticity to *kawarimi*,
  but is invisible to SmartScreen/Gatekeeper; its value here is that the moment
  releases are OS-signed, every recipient package automatically contains signed
  binaries with no further changes.
  - **Note:** GitHub's `/releases/latest` (which both the updater and
    `FetchOfficialBinaries` use) ignores drafts **and prereleases**. A release
    marked "pre-release" will not be picked up — publish releases as full
    releases.

## Path A — macOS: Developer ID + notarization (removes the warning entirely)

1. Enroll in the **Apple Developer Program** ($99/year; individuals in the EU
   are eligible).
2. Create a **Developer ID Application** certificate and an **App Store Connect
   API key** (for notarization).
3. Sign + notarize from the existing **Linux** release runner with
   [`rcodesign`](https://gregoryszorc.com/docs/apple-codesign/stable/) (the
   `apple-codesign` project — no Mac needed):
   - `rcodesign sign --p12-file devid.p12 --code-signature-flags runtime <binary>`
   - `rcodesign notary-submit --api-key-file key.json --wait <zip-of-binary>`
4. Integration point: a `binary_signs:` block in `.goreleaser.yml` (runs
   per-binary after compile, before checksums — so `checksums.txt` covers the
   *signed* bytes and the Ed25519 chain stays valid), or a post-goreleaser step
   on the draft release's assets (the release is `draft: true`; re-run
   checksums + `tools/ksign` after signing in that case). Secrets to add next
   to `RELEASE_SIGNING_KEY` in `.github/workflows/release.yml`: the base64 p12
   + password, and the App Store Connect API key.
5. Caveat: bare Mach-O executables **cannot be stapled** — Gatekeeper fetches
   the notarization ticket online on first run. Fine for typical users; a fully
   offline recipient still needs the "Open Anyway" path, which is why that copy
   stays in the instructions.

## Path B — Windows: Authenticode (eligibility decides the route)

| Route | Cost | Eligibility | CI story |
| --- | --- | --- | --- |
| **SignPath Foundation** (recommended for this project) | free | qualifying open-source projects (apply; review takes days–weeks) | first-class GitHub Actions integration; OV cert with established reputation |
| **Certum Open Source** | ~€70/yr | EU individuals OK | smart-card/cloud based — CI-hostile; realistically a manual signing step on the draft assets per release |
| **Azure Artifact Signing** (ex Trusted Signing) | $9.99/mo | individuals: **USA/Canada only**; organizations: USA/Canada/EU/UK | good CI story (`jsign` from Linux, or the official action on a Windows runner) |
| Classic OV cert (DigiCert/SSL.com/…) | $200–400/yr | orgs, some individuals | hardware-key requirement since 2023 makes CI need their cloud-signing add-ons |

Notes: any OV route builds SmartScreen reputation over time per certificate
(not instant); reputation earned survives releases, unlike the per-file-hash
reputation unsigned binaries slowly accrue and lose on every release. Signing
`kawarimi-windows-amd64.exe` slots into the same `binary_signs:` /
sign-the-draft integration points as Path A.

## Sources consulted

- [Azure Artifact Signing pricing](https://azure.microsoft.com/en-us/pricing/details/artifact-signing/) and [FAQ](https://learn.microsoft.com/en-us/azure/artifact-signing/faq) (individual eligibility: USA/Canada)
- [Trusted Signing individual-developer announcement](https://techcommunity.microsoft.com/blog/microsoft-security-blog/trusted-signing-is-now-open-for-individual-developers-to-sign-up-in-public-previ/4273554)
- [rcodesign notarizing docs](https://gregoryszorc.com/docs/apple-codesign/stable/apple_codesign_rcodesign_notarizing.html) and [apple-code-sign-action](https://github.com/indygreg/apple-code-sign-action)
