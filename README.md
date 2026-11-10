# Vigil

**Read-only, Alpenglow-aware Solana validator operations for Linux.**

Vigil combines the checks operators normally glue together: vote-account VAT runway, BLS registration, delinquency, identity consistency, leader production, per-slot missed duties, Prometheus metrics, Linux host audits, actionable alerts, and advisory maintenance planning.

[![CI](https://github.com/ValidatorStack/solana-vigil/actions/workflows/ci.yml/badge.svg)](https://github.com/ValidatorStack/solana-vigil/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ValidatorStack/solana-vigil)](https://github.com/ValidatorStack/solana-vigil/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go version](https://img.shields.io/badge/Go-1.23%2B-00ADD8)](go.mod)

> **Safety boundary:** Vigil never asks for a seed phrase, private key, or keypair file. It never signs or sends a transaction, changes host settings, restarts a validator, or switches identities. Use a local RPC and public validator addresses only. Test on testnet and with your own alert thresholds before production use.

## Production target: main Solana validator servers

Vigil is built to run **directly on Linux Solana mainnet-beta validator servers**—small, middle-sized, and large operators alike. It is not a laptop dashboard, an Ethereum tool, or a substitute for local validator observation. Run one Vigil instance on each validator server and point it at that server's local RPC and public vote account. An RPC-only node can expose compatible data, but host-audit and validator-duty results are meaningful only when Vigil runs on the actual validator being monitored.

The production target is mainnet-beta. Testnet remains supported as a staging environment so operators can validate configuration and alerts safely before a mainnet deployment; no source-code change or artificial network restriction is required. Linux AMD64 and ARM64 are the supported production platforms.

## Why operators can trust it

- **Read-only by construction.** No key loading, signing, transaction, failover, or host-mutation code paths.
- **No hidden dependency tree.** The binary uses Go's standard library only; `CGO_ENABLED=0` produces a static Linux executable.
- **Honest uncertainty.** Missing RPC fields and failed observations become `unknown`, never a fabricated healthy value.
- **No alert storms.** Sustained-failure thresholds, stable recoveries, deduplication, severity escalation, retry-on-delivery-failure, and repeat intervals are built in.
- **No secret configuration.** Webhook URLs, Telegram tokens, and PagerDuty routing keys come from environment variables.
- **Small footprint.** One binary, no database, no required cloud service, no telemetry, and no vendor lock-in.
- **Made for every operator.** Guided setup and safe defaults for a first validator; JSON, Prometheus, systemd hardening, host drift, and multi-architecture builds for professional fleets.

## What is included

| Command | Purpose |
|---|---|
| `vigil init` | Guided setup with public-address validation, RPC probing, identity cross-checks, and same-cluster verification |
| `vigil check` | One-shot validator health, VAT, BLS, stake, delinquency, vote-lag, identity, slot-lag, and leader-production checks |
| `vigil watch` | Continuous monitoring, per-slot missed-leader detection, and Slack/Discord/Telegram/PagerDuty/generic alerts |
| `vigil test-alert` | Prove alert delivery works without changing incident state |
| `vigil export` | Cached, bounded-cardinality Prometheus exporter with health and readiness endpoints |
| `vigil audit` | Read-only Linux CPU, memory, sysctl, time, NIC, process-limit, and voting-mode audit with baseline drift |
| `vigil report` | Self-contained HTML, JSON, or terminal report; optionally includes the host audit |
| `vigil plan-restart` | Locate an advisory leader-free slot window; never performs a restart |
| `vigil score` | One 0-100 number, per-cell detail, and the exact fix for each failing cell |
| `vigil disk` | Free space and days-to-full from the observed growth slope |
| `vigil keys` | Keypair mode, owner and age — `stat` only, the file is never opened |
| `vigil duties` | Leader slots, blocks produced, missed slots and skip rate for the epoch |
| `vigil rewards` | Inflation rewards per epoch in SOL, with commission |
| `vigil fleet` | Roll-up across every node listed in `fleet.json` |

Every summary is read-only: no key is loaded, nothing is signed, no host setting
is changed. `vigil score` also prints how much of the picture it could observe, so
a partial run never looks like a clean bill of health.

## Five-minute Linux quickstart

### Requirements

- A Linux Solana mainnet-beta validator server on AMD64 (`x86_64`) or ARM64 (`aarch64`).
- That validator's local RPC endpoint, normally `http://127.0.0.1:8899`.
- Your **public** vote-account address. Your identity address is optional and can be auto-filled.
- An optional second RPC on **mainnet-beta** for independent slot-lag observations; genesis hashes must match.

### 1. Install a verified release

One command, as root, on the validator server:

```bash
curl -fsSL https://raw.githubusercontent.com/ValidatorStack/solana-vigil/main/install.sh | sudo bash
```

Or inspect first, if you prefer:

```bash
curl -fSLo install.sh https://raw.githubusercontent.com/ValidatorStack/solana-vigil/main/install.sh
less install.sh
sudo bash install.sh --version v0.2.0
```

For a user-local installation:

```bash
bash install.sh --version v0.2.0 --prefix "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"
```

The installer verifies SHA-256. If `cosign` is installed, it also verifies the release's keyless Sigstore signature. To build instead, see [Build from source](#build-from-source).

### 2. Run guided setup

```bash
vigil init
vigil check --config vigil.json
```

Setup asks only for public addresses and RPC URLs. By default it probes the local RPC, confirms that the identity owns the vote account, and compares genesis hashes when a reference RPC is configured. It writes `vigil.json` with mode `0600` and refuses to overwrite an existing file unless `--force` is explicit.

For automation:

```bash
vigil init \
  --non-interactive \
  --vote-account <your public vote account> \
  --local-rpc http://127.0.0.1:8899 \
  --output vigil.json
```

`check` uses familiar monitoring exit codes: `0` OK, `1` warning, `2` critical, and `3` unknown.

### 3. Start watching

```bash
vigil watch --config vigil.json
```

With no webhook configured, alerts are written to the log. To configure a destination, rerun `vigil init --force` or add a webhook as shown in [Alerting](#alerting), then verify it:

```bash
vigil test-alert --config vigil.json
```

## Alpenglow checks that must be correct

### VAT runway

Vigil reads the vote-account balance and its actual account size, asks the cluster for the rent-exempt minimum, and calculates:

```text
spendable = max(0, vote_account_balance - rent_exempt_minimum)
runway_epochs = spendable / configured_vat_per_epoch
next_epoch_sufficient = spendable >= configured_vat_per_epoch
```

The default fee is **0.8 SOL per epoch** (`800000000` lamports). This is configuration, not a hard-coded promise about future governance. Verify the current fee and slot-time assumptions for your cluster. Set `vat_lamports_per_epoch` to `0` only if VAT monitoring is intentionally disabled.

A runway below `1` is critical by default because the next payment cannot be covered. A runway below `2` warns. Thresholds are configurable.

### BLS registration

Vigil distinguishes three states that naïve exporters often merge:

- `blsPubkeyCompressed` contains a valid base58 value decoding to 48 bytes → **registered**.
- The field is explicitly `null` or empty, or the value is malformed → **critical**.
- The field is absent from the RPC response → **unknown**, with advice to verify the RPC/client schema.

### Leader duties

`getBlockProduction` supplies the current range's leader slots, produced blocks, misses, and skip ratio. In `watch` mode, Vigil also caches the leader schedule and checks newly confirmed assigned slots with `getBlocks`. The default confirmation depth is 32 slots, preventing immediate false pages on unconfirmed forks. Per-slot events do not become permanent Prometheus labels.

## Alerting

Secrets stay outside JSON. This Slack example uses a generic incoming-webhook configuration:

```json
{
  "name": "primary",
  "type": "slack",
  "url_env": "VIGIL_WEBHOOK_URL",
  "timeout": "5s"
}
```

Place that object in `alerts.webhooks`, then run:

```bash
export VIGIL_WEBHOOK_URL='https://hooks.slack.com/services/REPLACE/REPLACE/REPLACE'
vigil test-alert --config vigil.json
```

Supported `type` values and required settings:

| Type | Required settings |
|---|---|
| `slack` | `url_env` |
| `discord` | `url_env` |
| `generic` | `url_env`; optional `headers`, with `env:VARIABLE` for secret values |
| `telegram` | `token_env`, `chat_id` |
| `pagerduty` | `routing_key_env` |

Default behavior:

- Critical conditions notify immediately.
- Warnings and unknown states require two consecutive observations.
- Recovery requires two consecutive healthy observations.
- Severity changes notify immediately.
- An active incident repeats at most once per hour.
- Delivery failures are retried on the next observation rather than waiting for the repeat interval.
- State is saved atomically on Linux with owner-only permissions.

For a 24/7 box, use systemd rather than a terminal session.

## Run as hardened Linux services

From the repository root after creating `vigil.json`:

```bash
sudo useradd --system --home /var/lib/vigil --shell /usr/sbin/nologin vigil 2>/dev/null || true
sudo install -d -m 0750 -o vigil -g vigil /var/lib/vigil
sudo install -d -m 0750 -o root -g vigil /etc/vigil
sudo install -m 0640 -o root -g vigil vigil.json /etc/vigil/vigil.json
sudo install -m 0644 deploy/vigil-watch.service /etc/systemd/system/vigil-watch.service
sudo install -m 0644 deploy/vigil-export.service /etc/systemd/system/vigil-export.service
sudo systemctl daemon-reload
sudo -u vigil /usr/local/bin/vigil check --config /etc/vigil/vigil.json
sudo systemctl enable --now vigil-watch vigil-export
```

If using alert secrets:

```bash
sudo install -m 0600 -o root -g vigil deploy/env.example /etc/vigil/env
sudoedit /etc/vigil/env
sudo systemctl restart vigil-watch
```

Test delivery without exposing secret values in a process list:

```bash
sudo bash -c 'set -a; source /etc/vigil/env; set +a; exec sudo -u vigil --preserve-env=VIGIL_WEBHOOK_URL,VIGIL_TELEGRAM_BOT_TOKEN,VIGIL_PAGERDUTY_ROUTING_KEY /usr/local/bin/vigil test-alert --config /etc/vigil/vigil.json'
```

Check service health:

```bash
systemctl status vigil-watch vigil-export
journalctl -u vigil-watch -n 100 --no-pager
curl -fsS http://127.0.0.1:9109/healthz
curl -fsS http://127.0.0.1:9109/metrics | less
```

The exporter binds to loopback by default. Do not expose it directly to the public internet. If Prometheus runs on another machine, use a private network, firewall rules, and a reverse proxy or secure tunnel.

## Prometheus and Grafana

Add [`deploy/prometheus.yml`](deploy/prometheus.yml) to your existing scrape configuration and load [`deploy/prometheus-alerts.yml`](deploy/prometheus-alerts.yml) as a rule file. Import [`deploy/grafana-dashboard.json`](deploy/grafana-dashboard.json) into Grafana and select your Prometheus datasource for the ready-made VAT, BLS, delinquency, leader-production, slot-lag, and check-status panels.

```yaml
scrape_configs:
  - job_name: vigil
    scrape_interval: 15s
    scrape_timeout: 10s
    static_configs:
      - targets: ["127.0.0.1:9109"]
```

Core metrics include:

| Metric | Meaning |
|---|---|
| `solana_vat_runway_epochs` | Continuous above-rent VAT runway |
| `solana_vat_next_epoch_sufficient` | Whether the next VAT can be paid |
| `solana_bls_registered` | 1 registered, 0 missing/malformed; omitted when unknown |
| `vigil_observation_available` | Separates missing observations from negative states |
| `solana_delinquent` | Current RPC delinquency state |
| `solana_slot_lag` | Non-negative reference-minus-local slot lag |
| `solana_leader_slots` | Leader slots in the reported production range |
| `solana_blocks_produced` | Produced blocks in that range |
| `solana_leader_skip_ratio` | Skip ratio from 0 to 1 |
| `vigil_check_status` | 0 OK, 1 warning, 2 critical, 3 unknown, labeled by stable check ID |

If both `watch` and Alertmanager are used, route them to different severity levels or disable duplicate destinations. Two independent paging paths can otherwise page twice for the same condition.

## Read-only Linux audit

```bash
vigil audit --client agave
vigil audit --client firedancer
vigil audit --client agave --pid 12345 --interface eth0
```

The audit reports observed values, comparison baselines, and recommendations. It does **not** change sysctl, NIC rings, CPU isolation, huge pages, limits, or services. Profile-dependent values such as transparent huge pages and TCP congestion control are informational rather than falsely presented as universal rules.

Save a baseline and detect drift later:

```bash
vigil audit --client agave --format json --output audit-baseline.json
vigil audit --client agave --baseline audit-baseline.json
```

Some process, NIC, or time checks require host permissions or tools such as `ethtool`, `timedatectl`, or `chronyc`. Missing evidence is reported as `unknown`. Run the audit directly on the validator, not on your laptop.

## Reports and maintenance planning

Create a portable report with no external JavaScript, fonts, or CDN assets:

```bash
vigil report --config vigil.json --format html --output vigil-report.html
vigil report --config vigil.json --format html --include-audit --output vigil-full-report.html
vigil report --config vigil.json --format json --output vigil-snapshot.json
```

Find an advisory leader-free window:

```bash
vigil plan-restart --config vigil.json --min-free-slots 32 --search-slots 10000
```

The planner estimates timing from recent performance samples when available. Leader schedules and cluster conditions can change, so re-check immediately before maintenance. **Vigil never restarts a process, switches identities, or attempts automatic failover.** Double-signing prevention remains the operator's responsibility.

## Configuration

`vigil init` is the recommended interface. [`config.example.json`](config.example.json) documents the complete default shape. JSON decoding is strict: unknown fields are rejected so misspelled safety settings cannot silently disappear.

| Setting | Default | Notes |
|---|---:|---|
| `rpc.local_url` | `http://127.0.0.1:8899` | Primary validator observation endpoint |
| `rpc.reference_url` | empty | Optional independent endpoint on the same cluster |
| `rpc.commitment` | `confirmed` | `processed`, `confirmed`, or `finalized` |
| `rpc.timeout` | `8s` | Per-attempt timeout; transport/429/5xx retries are bounded |
| `monitor.interval` | `10s` | Watch polling interval |
| `monitor.vat_lamports_per_epoch` | `800000000` | Verify against current protocol rules |
| `monitor.vat_warning_epochs` | `2` | Low-runway warning |
| `monitor.vat_critical_epochs` | `1` | Critical below this runway or insufficient next payment |
| `monitor.leader_confirmation_depth` | `32` | Confirmation delay for per-slot duty decisions |
| `monitor.alert_on_missed_leader_slot` | `true` | Set false if using only aggregate skip alerts |
| `alerts.failure_threshold` | `2` | Sustained observations for non-critical checks |
| `alerts.recovery_threshold` | `2` | Stable healthy observations before resolution |
| `alerts.repeat_interval` | `1h` | Active-incident reminder interval |

See [`docs/OPERATIONS.md`](docs/OPERATIONS.md) for tuning and troubleshooting.

## Build from source

Go 1.23 or newer is sufficient; there are no third-party Go module dependencies.

```bash
git clone https://github.com/ValidatorStack/solana-vigil.git
cd vigil
make test
make build
./bin/vigil version
```

Build both static Linux release binaries and checksums:

```bash
make release VERSION=v0.2.0
ls -lh dist/
```

Container build, if desired:

```bash
docker build -t vigil:local .
docker run --rm vigil:local version
```

## Security and scope

- Read [`SECURITY.md`](SECURITY.md) before reporting or deploying.
- RPC URLs may contain provider credentials. Vigil rejects URLs with embedded userinfo and does not include endpoints in normal logs, but environment-managed URL credentials should still be protected by your secret system.
- The watcher and exporter need outbound HTTP(S) and local RPC access only. The systemd units remove capabilities and write access outside `/var/lib/vigil`.
- The host audit is intentionally separate from the long-running watcher because `/proc`, `/sys`, and NIC inspection can require broader read permissions.
- No application is a substitute for reviewed failover, key custody, transaction policies, or incident runbooks.

### Explicit limitations in this release

Vigil does not yet parse Alpenglow block-footer certificate participation, normalize Firedancer's tile endpoint, track SFDP version deadlines, or verify client upgrade binaries. `plan-restart` is schedule advice only. Vote/certificate semantics can change with client releases; unknown observations must be investigated rather than suppressed. These are deliberate scope boundaries, not features to infer from the presence of an `alpenglow` metric name.

## Development and contribution

```bash
make fmt-check
make vet
make test
make race
make cross
```

Read [`CONTRIBUTING.md`](CONTRIBUTING.md). Changes to VAT, BLS, leader accounting, alert state, or host-audit rules require tests and clear documentation of evidence sources. Avoid noisy defaults and never add key access, transaction submission, or automatic identity switching.

## License

MIT. See [`LICENSE`](LICENSE).
