# Changelog

All notable changes are documented here. The format follows Keep a Changelog and versions follow Semantic Versioning.

## [Unreleased]

## [0.1.0] - 2026-11-09

### Added

- Guided public-data-only setup with local RPC, identity, vote-account, and same-cluster validation.
- VAT runway above rent and next-epoch sufficiency checks.
- Explicit BLS registered, missing/malformed, and unknown states.
- Delinquency, identity, active-stake, vote-lag, local/reference slot-lag, and leader-production monitoring.
- Per-slot missed-leader detection in watch mode with confirmation delay.
- Persistent alert flap suppression, escalation, recovery, retries, and Slack/Discord/Telegram/PagerDuty/generic delivery.
- Safe alert-delivery test command.
- Cached Prometheus exporter with low-cardinality metrics and health/readiness endpoints.
- Read-only Linux audit and host-drift comparison.
- HTML, JSON, and terminal reports.
- Advisory leader-free restart-window planner.
- Static Linux AMD64/ARM64 builds, checksum-verifying installer, hardened systemd services, and Sigstore-signed release checksums.

## v0.2.0

New summaries, all read-only:

- `vigil score` — 0-100 health score with per-cell detail and fix hints, plus a coverage line so a partial observation is never mistaken for a pass.
- `vigil disk` — free space, growth slope and days-to-full forecast.
- `vigil keys` — keypair hygiene from `stat` only (mode, owner, age, size); the file is never read.
- `vigil duties` — leader slots, blocks produced, missed slots, skip rate for the epoch.
- `vigil rewards` — inflation rewards per epoch in SOL with commission.
- `vigil fleet` — one command across a fleet.json of nodes, with per-node status, score, missed slots and runway.

Internal: new `internal/ops` package, no new dependencies.
