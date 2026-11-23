# Security Policy

## Supported Versions

Security updates are applied to the latest state of the default branch of this repository (`TinkeringDao/cronos64`, a [`TinkeringDao`](https://github.com/TinkeringDao) fork of TECHO5 for the Echo Show 5 2nd generation).

| Branch / Version | Supported |
| :--- | :--- |
| Default branch (HEAD) | Yes |
| Historical tags / feature branches | No |

## Reporting a Vulnerability

Do **not** open a public GitHub issue for suspected security vulnerabilities, credential leaks, update-channel signing weaknesses, or memory corruption bugs.

1. **GitHub Security Advisories (Preferred):** Submit a report via the repository's **Security -> Report a vulnerability** tab.
2. **Maintainer Contact:** Reports are reviewed by `@TinkeringDao` via GitHub Security Advisories.

### Required Report Details

- Affected commit SHA and build target (hardware revision, `echod` package).
- Minimal deterministic reproduction steps or proof-of-concept, with all personal MAC addresses, serial numbers, and network addresses redacted.
- Assessment of impact, privilege boundary crossed, and blast radius.

## Release Signing and Egress Posture

- Update manifests are accepted only when signed with this fork's own Ed25519 release key. The private seed is never stored in this repository.
- Outbound connections of the Brain, VLM, STT, TTS, and actuation clients are restricted to RFC 1918 private ranges, RFC 6598 shared/overlay address space, and IPv6 ULA (`fc00::/7`). Cloud speech-to-speech is disabled in this build.

## Legal & Safe-Harbor Boundaries

All security testing must comply with [`LEGAL.md`](./LEGAL.md):

- Test exclusively on hardware that you personally own or are explicitly authorized to test.
- Never include proprietary OEM firmware binaries, cryptographic keys, or verbatim OEM hex dumps in reports or pull requests.

## Disclosure Policy

Reports are triaged upon receipt. Once a fix is verified and merged, coordinated disclosure is published via a GitHub Security Advisory.
