# Contributing to Vigil

Thank you for improving validator operations. Vigil optimizes for correctness, low noise, auditability, and a small trusted computing base—not feature count.

## Non-negotiable boundaries

Pull requests must not add private-key access, signing, transaction submission, automatic identity switching, automatic failover, or host/validator mutation. Advisory output and read-only evidence collection are welcome.

## Development setup

Use Go 1.23 or newer on Linux for the most representative environment.

```bash
make fmt-check
make vet
make test
make race
make cross
```

The project currently has no third-party Go dependencies. Adding one requires a clear justification, license review, and an explanation of why the standard library is insufficient.

## High-risk changes

Add focused tests and documentation for any change to:

- VAT rent adjustment, runway arithmetic, or next-epoch sufficiency.
- BLS absent/null/malformed/valid state handling.
- Delinquency, identity, vote-lag, leader-schedule, or block-production logic.
- Alert deduplication, escalation, recovery, persistence, or secret handling.
- Metric names, label cardinality, or exporter concurrency.
- Linux audit parsing and baselines.
- systemd sandboxing, installer verification, or release signing.

Use table-driven tests for valid, boundary, malformed, missing, and contradictory inputs. A failed observation must not silently become a zero or healthy value.

## Protocol and client changes

Cite primary protocol/client documentation in the pull request. Keep fee values and operator thresholds configurable. Distinguish observed facts from recommendations. Do not describe a check as Alpenglow certificate monitoring unless it actually reads and validates certificate evidence.

## Pull request checklist

- [ ] Scope is limited and read-only.
- [ ] `make fmt-check vet test race cross` passes on Linux.
- [ ] New behavior has tests, including failure paths.
- [ ] Metrics do not add per-slot or other unbounded labels.
- [ ] Secrets are not logged, embedded in config examples, or included in test fixtures.
- [ ] README/operations documentation matches actual behavior.
- [ ] Backward-incompatible config or metric changes are called out in the changelog.

## Bug reports

Include `vigil version`, OS/kernel, validator client/version, relevant public configuration with secrets removed, and the smallest reproduction possible. Never attach private keys, seed phrases, webhook URLs, tokens, or unreviewed production reports.
