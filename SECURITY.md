# Security policy

## Supported versions

Security fixes are applied to the latest minor release. Until v1.0, test new releases on a non-production or testnet validator before deploying them across a fleet.

## Report a vulnerability

Do not open a public issue for a vulnerability that could expose keys, credentials, RPC endpoints, or production infrastructure.

Use GitHub's **Private vulnerability reporting** feature on this repository. If it is unavailable, ask the repository owner through a public issue for a private contact without including sensitive details. Include the affected version, reproduction steps, impact, and a suggested remediation when possible. Do not include seed phrases, private keys, keypair files, webhook URLs, tokens, or production logs containing secrets.

A maintainer should acknowledge a report within 72 hours, reproduce it, publish a coordinated fix, and credit the reporter if desired. These are project goals, not a paid bug-bounty commitment.

## Threat model

### What Vigil handles

- Public vote-account and identity addresses.
- Solana JSON-RPC responses from configured local and reference endpoints.
- Host state during the explicitly invoked read-only audit.
- Alert secrets read from environment variables at process startup.
- Local alert incident state and generated reports.

### What Vigil must never do

- Read validator identity keypair files, authorized-voter keys, withdrawer keys, seed phrases, or remote signers.
- Sign or submit transactions.
- Change vote accounts, commissions, BLS keys, or protocol settings.
- Restart validators, switch identities, or automate failover.
- Apply sysctl, NIC, firewall, huge-page, CPU-isolation, or service changes.
- Send telemetry to the project author or a default third party.

### Trust boundaries

1. **Local RPC:** normally trusted but can be stale, unhealthy, or compromised. Vigil exposes contradictory observations as unknown/critical rather than treating them as valid.
2. **Reference RPC:** untrusted for local validator state and used only for independent slot/genesis observations. Genesis hashes must match at startup. Do not use the reference endpoint as proof that your validator is healthy.
3. **Alert destinations:** receive public validator identifiers and incident details. Protect webhook URLs and review the provider's retention/access policy.
4. **Generated reports:** contain public addresses, client version, host audit evidence, and possibly network interface names. Review before posting publicly.
5. **Host audit:** has a wider observation surface than the daemon. It is separate, one-shot, and should run with the least permission that returns the required evidence.

### Release integrity

Release binaries are built in GitHub Actions with `CGO_ENABLED=0`, published with SHA-256 checksums, and the checksum file is signed using Sigstore keyless signing through GitHub OIDC. `install.sh` verifies SHA-256 and verifies the signature when `cosign` is available. Skipping signature verification produces an explicit notice.

For high-assurance deployments, build from a reviewed source commit and compare the resulting behavior against your test environment. Go module dependencies are currently limited to the standard library, reducing but not eliminating toolchain and release-pipeline risk.

### Secret handling

- Keep `/etc/vigil/env` owner-readable and never commit it.
- Prefer an RPC URL without credentials on loopback. If a remote RPC requires a token in its path, protect the config and service logs accordingly.
- Vigil rejects URLs containing `user:password@...` and avoids printing endpoint URLs in normal error messages.
- Rotate alert credentials if they appear in shell history, tickets, screenshots, CI logs, or generated reports.

## Security-related changes

Changes introducing network listeners, external commands, filesystem writes, release signing, or new RPC methods require focused review. Proposed key access, transaction submission, automatic failover, or host-mutation functionality is out of scope for this project.
