# Vigil operations guide

## Recommended rollout

1. Build or verify the release on a non-production Linux host.
2. Run `vigil init` against a testnet validator or a reviewed production RPC without enabling webhooks.
3. Run `vigil check` repeatedly through different cluster conditions. Investigate every `unknown` before relying on alert silence.
4. Configure one alert destination and run `vigil test-alert`.
5. Run `vigil watch` in the foreground for at least one leader schedule and an epoch boundary.
6. Establish a host audit baseline. Do not change host settings merely to make every baseline green.
7. Install the systemd service, verify its logs and alert test, then enable Prometheus if needed.
8. Document who receives each severity and the runbook for VAT, BLS, delinquency, identity mismatch, and missed leader slots.

## Why a check is unknown

`unknown` means Vigil cannot support a binary conclusion with the evidence returned. Common causes:

- RPC timeout, rate limit, unavailable method, or incompatible response schema.
- `blsPubkeyCompressed` is absent rather than explicitly null.
- The leader-production sample is below `leader_minimum_sample`.
- Local slot and vote observations use inconsistent views.
- Host audit lacks permission or an optional utility such as `ethtool`.

Unknown is a monitoring-coverage problem. Do not convert it to OK merely to reduce noise. Increase timeout carefully, use a reliable local RPC, or upgrade the relevant client after reviewing compatibility.

## Choosing RPC endpoints

The local endpoint should be the validator itself on loopback whenever possible. Public RPC endpoints can rate limit and should not be the only source of validator health. A reference endpoint is optional and adds only genesis/slot comparison; it is not a substitute for local vote-account evidence.

If a remote endpoint uses a secret in its URL path, protect the config like a credential. Prefer a local endpoint with no embedded token. Vigil rejects `user:password@host` URLs.

The exporter caches snapshots for five seconds by default. Multiple simultaneous scrapes share one collection, but very frequent scrapes still increase RPC work. Keep Prometheus `scrape_timeout` below the HTTP server's write timeout and do not use a one-second scrape interval without measuring RPC load.

## VAT operations

Vigil computes runway from the vote account, not the identity account. The balance above the rent-exempt minimum is what matters. The fee is configurable because protocol parameters can change.

When `solana_vat_next_epoch_sufficient` is zero:

1. Verify `solana_vat_lamports_per_epoch` against current protocol documentation and your slot-time configuration.
2. Confirm the vote address and cluster are correct.
3. Inspect total balance, rent minimum, and spendable metrics.
4. Follow your reviewed funding runbook. Vigil deliberately cannot transfer SOL or send a transaction.
5. Verify recovery before the epoch boundary; do not assume an epoch-boundary reward will arrive in time.

Do not set thresholds exactly at your intended balance. Leave operational margin for RPC delay, epoch timing, and fee changes.

## Leader-slot alerts

Per-slot detection starts at the current confirmed point after each watch restart. It does not backfill hours of missed slots and page for all of them. Current-range `getBlockProduction` statistics remain available separately.

A missing slot can result from validator behavior, cluster/fork conditions, or an RPC visibility problem. The confirmation depth reduces premature conclusions but does not prove root cause. Increase `leader_confirmation_depth` if your commitment and RPC view require it.

For noisy clusters or small validators that prefer aggregate alerts:

```json
"alert_on_missed_leader_slot": false
```

Keep `leader.production` enabled so sustained skip-rate changes remain visible.

## Alert policy

- `critical_immediate: true` is intended for conditions such as insufficient next-epoch VAT and identity mismatch.
- `failure_threshold: 2` filters a single warning/unknown observation.
- `recovery_threshold: 2` prevents rapid firing/resolved oscillation.
- `repeat_interval: 1h` is a reminder, not a minimum polling interval.
- Delivery failure resets the next-notification timestamp so the next observation retries promptly.

Keep one authoritative pager for each incident class. If Alertmanager and `watch` both consume the same rules, duplicate pages are expected unless routing is separated.

The alert state file is relative to the service working directory. With the supplied unit, `.vigil/alert-state.json` resolves under `/var/lib/vigil`. Preserve it across service restarts.

## Exporter and firewall

The default listener is `127.0.0.1:9109`. To scrape remotely, bind only to a private interface and apply host firewall rules. Metrics reveal public validator identifiers, client version, operational state, and genesis hash. They are not intended for unauthenticated public internet exposure.

Endpoints:

- `/metrics`: Prometheus text format 0.0.4.
- `/healthz`: process liveness; does not assert validator health.
- `/readyz`: whether a recent snapshot exists; initially returns 503.

## Host audit interpretation

The audit's numeric baselines are conservative starting points, not protocol guarantees. Client requirements and workload differ by cluster, account set, RPC role, hardware, and client release.

- CPU governor and isolation must be evaluated with topology, NUMA, thermal limits, and OS reserve capacity.
- Huge pages and THP are not interchangeable. Firedancer explicit huge-page reservations require its own deployment guide.
- UDP buffers and NIC rings should be changed alongside drop/error counters and load testing.
- File limits must be checked on the validator PID, not inferred from your interactive shell.
- TCP congestion-control algorithms do not directly optimize predominantly UDP shred ingest.
- Time synchronization requires offset/source monitoring as well as a daemon in the running state.

Use configuration management for any reviewed change. Vigil never applies one.

## Reports

HTML reports are self-contained and escaped. They still contain public addresses, client version, and optional host evidence. Review before sharing. For automated evidence pipelines, use JSON and validate against your own retention/access policy.

## Upgrade Vigil safely

1. Read the changelog and release notes.
2. Download the new binary and verify SHA-256/Sigstore.
3. Test `check`, `export --once`, and `test-alert` with a copy of the production config on a non-critical host or testnet.
4. Back up config and alert state.
5. Replace the binary and restart one service.
6. Confirm version, logs, readiness, and a fresh snapshot before expanding the rollout.

`plan-restart` concerns validator leader schedules; it does not orchestrate Vigil or validator upgrades.
