#!/usr/bin/env bash
# Systemd end-to-end scenario:
#   clean container -> install base -> install target via Sury -> switch ->
#   remove base -> target remains active.
#
# Usage: test/e2e/run.sh <image> <base-version> <target-version>
# Example: test/e2e/run.sh debian:12 8.2 8.3

set -euo pipefail

E2E_TMP="$(mktemp -d)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

IMAGE="${1:?uso: $0 <image> <base-version> <target-version>}"
BASE="${2:?uso: $0 <image> <base-version> <target-version>}"
TARGET="${3:?uso: $0 <image> <base-version> <target-version>}"
CID=""
NAME="phpman-e2e-$(printf '%s' "$IMAGE" | tr '/:.' '___')-$$"

cleanup() {
    if [[ -n "$CID" ]]; then
        docker rm -f "$CID" >/dev/null 2>&1 || true
    fi
    rm -rf "$E2E_TMP"
}
trap cleanup EXIT

require_cmd docker jq go

log "cenário systemd: $IMAGE base=$BASE target=$TARGET"
BIN="$(build_e2e_binary)"
TAG="$(build_image "$IMAGE")"
CID="$(start_systemd_container "$TAG" "$NAME")"
prepare_container "$CID" "$BIN"

log "verificando container limpo"
assert_no_php "$CID"

log "instalando versão base PHP $BASE"
phpman_as "$CID" install "$BASE" --yes
assert_package_installed "$CID" "php${BASE}-cli"
assert_fpm_active "$CID" "$BASE"
assert_status_has_version "$CID" "$BASE"

log "gerando dry-run do target PHP $TARGET via Sury/ondrej"
truncate_journal "$CID"
phpman_as "$CID" install "$TARGET" --third-party --dry-run --json >"$E2E_TMP/target-dry.json"
jq -e '.repository.name == "Sury/ondrej"' "$E2E_TMP/target-dry.json" >/dev/null \
    || die "dry-run do target não incluiu o repositório Sury/ondrej"
if [[ "$(jq -r '.steps | length' "$E2E_TMP/target-dry.json")" -eq 0 ]]; then
    die "dry-run do target não tem passos"
fi

log "instalando target PHP $TARGET via Sury/ondrej"
phpman_as "$CID" install "$TARGET" --third-party --yes

copy_journal "$CID" "$E2E_TMP/target-journal.jsonl"
assert_journal_balanced "$E2E_TMP/target-journal.jsonl"
assert_contract "$E2E_TMP/target-dry.json" "$E2E_TMP/target-journal.jsonl"
assert_no_password "$CID"
assert_package_installed "$CID" "php${TARGET}-cli"
assert_package_installed "$CID" "php${TARGET}-fpm"
assert_fpm_active "$CID" "$TARGET"
assert_status_has_version "$CID" "$TARGET"

ACTIVE="$(phpman_as "$CID" status --json | jq -r '.active_version // ""')"
if [[ "$ACTIVE" == "$TARGET" ]]; then
    log "target ficou ativo após a instalação; voltando para $BASE para exercitar o switch"
    phpman_as "$CID" switch "$BASE" --yes
    assert_active_version "$CID" "$BASE"
elif [[ "$ACTIVE" != "$BASE" ]]; then
    die "versão ativa inesperada antes do switch: ${ACTIVE:-<vazia>}"
fi

log "trocando a versão ativa para PHP $TARGET"
phpman_as "$CID" switch "$TARGET" --yes
assert_active_version "$CID" "$TARGET"
assert_php_version "$CID" "$TARGET"
assert_fpm_active "$CID" "$TARGET"
assert_socket_exists "$CID" "$TARGET"

log "removendo a versão base PHP $BASE"
phpman_as "$CID" remove "$BASE" --yes
assert_package_absent "$CID" "php${BASE}-cli"
assert_package_absent "$CID" "php${BASE}-fpm"
assert_status_lacks_version "$CID" "$BASE"
assert_status_has_version "$CID" "$TARGET"
assert_active_version "$CID" "$TARGET"
assert_php_version "$CID" "$TARGET"
assert_fpm_active "$CID" "$TARGET"
assert_no_password "$CID"

log "OK: $IMAGE base=$BASE target=$TARGET"
