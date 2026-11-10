#!/usr/bin/env bash
set -euo pipefail

VERSION="v0.2.0"
REPO="ValidatorStack/solana-vigil"
PREFIX="/usr/local/bin"

usage() {
  cat <<'EOF'
Usage: bash install.sh [--version v0.2.0] [--repo ValidatorStack/solana-vigil] [--prefix /usr/local/bin]

Downloads a Linux Vigil release, verifies SHA-256, and verifies the keyless
Sigstore signature when cosign is installed. No validator keys are accessed.
EOF
}
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2:?missing version}"; shift 2 ;;
    --repo) REPO="${2:?missing repository}"; shift 2 ;;
    --prefix) PREFIX="${2:?missing prefix}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ "$(uname -s)" == "Linux" ]] || { echo "Vigil releases target Linux validators." >&2; exit 1; }
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || { echo "Invalid version: $VERSION" >&2; exit 2; }
[[ "$REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo "Invalid repository: $REPO" >&2; exit 2; }
command -v sha256sum >/dev/null || { echo "sha256sum is required." >&2; exit 1; }

if command -v curl >/dev/null; then
  fetch() { curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$1" --output "$2"; }
elif command -v wget >/dev/null; then
  fetch() { wget --quiet --https-only "$1" --output-document="$2"; }
else
  echo "curl or wget is required." >&2; exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
ASSET="vigil-linux-${ARCH}"
BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fetch "${BASE}/${ASSET}" "${TMP}/${ASSET}"
fetch "${BASE}/SHA256SUMS" "${TMP}/SHA256SUMS"
EXPECTED="$(awk -v asset="$ASSET" '$2 == asset {print $1}' "${TMP}/SHA256SUMS")"
[[ "$EXPECTED" =~ ^[a-f0-9]{64}$ ]] || { echo "Release checksum for ${ASSET} not found." >&2; exit 1; }
(cd "$TMP" && printf '%s  %s\n' "$EXPECTED" "$ASSET" | sha256sum --check --status -)

if command -v cosign >/dev/null; then
  fetch "${BASE}/SHA256SUMS.sig" "${TMP}/SHA256SUMS.sig"
  fetch "${BASE}/SHA256SUMS.pem" "${TMP}/SHA256SUMS.pem"
  cosign verify-blob \
    --signature "${TMP}/SHA256SUMS.sig" \
    --certificate "${TMP}/SHA256SUMS.pem" \
    --certificate-identity-regexp "^https://github.com/${REPO}/\.github/workflows/release\.yml@refs/tags/" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    "${TMP}/SHA256SUMS"
else
  echo "Notice: cosign not installed; SHA-256 verified, signature verification skipped." >&2
fi

if [[ ! -d "$PREFIX" || ! -w "$PREFIX" ]]; then
  echo "Cannot write ${PREFIX}. Re-run with sudo, or choose --prefix \$HOME/.local/bin." >&2
  exit 1
fi
install -m 0755 "${TMP}/${ASSET}" "${PREFIX}/vigil"
"${PREFIX}/vigil" version
cat <<EOF

Installed: ${PREFIX}/vigil
Next: vigil init
Vigil will never ask for a private key, seed phrase, or keypair file.
EOF
