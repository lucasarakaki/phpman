#!/usr/bin/env bash
# Failure/rollback scenario: run a real install in a container that has the
# systemctl binary but no systemd PID 1. The install packages successfully,
# fails at the FPM step, and the executor must roll back the packages.
#
# Usage: test/e2e/rollback.sh <image> <version>
# Example: test/e2e/rollback.sh debian:12 8.2

set -euo pipefail

E2E_TMP="$(mktemp -d)"
# shellcheck source=lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

IMAGE="${1:?uso: $0 <image> <version>}"
VERSION="${2:?uso: $0 <image> <version>}"
CID=""
NAME="phpman-rollback-$(printf '%s' "$IMAGE" | tr '/:.' '___')-$$"

cleanup() {
    if [[ -n "$CID" ]]; then
        docker rm -f "$CID" >/dev/null 2>&1 || true
    fi
    rm -rf "$E2E_TMP"
}
trap cleanup EXIT

require_cmd docker jq go

log "cenário de falha/rollback: $IMAGE PHP $VERSION"
BIN="$(build_e2e_binary)"
TAG="$(build_image "$IMAGE")"
CID="$(start_plain_container "$TAG" "$NAME")"
prepare_container "$CID" "$BIN"

log "verificando container limpo"
assert_no_php "$CID"

log "executando install; o passo systemctl deve falhar e disparar rollback"
set +e
phpman_as "$CID" install "$VERSION" --yes >"$E2E_TMP/install.log" 2>&1
CODE=$?
set -e
if [[ "$CODE" -eq 0 ]]; then
    cat "$E2E_TMP/install.log" >&2 || true
    die "install deveria falhar sem systemd"
fi
if ! grep -q "Rollback automático" "$E2E_TMP/install.log"; then
    cat "$E2E_TMP/install.log" >&2 || true
    die "saída do install não menciona o rollback automático"
fi

copy_journal "$CID" "$E2E_TMP/journal.jsonl"
assert_journal_balanced "$E2E_TMP/journal.jsonl"
assert_journal_has_step "$E2E_TMP/journal.jsonl" "install-packages"
assert_journal_finish_exit "$E2E_TMP/journal.jsonl" "install-packages" 0
assert_journal_has_step "$E2E_TMP/journal.jsonl" "enable-fpm"
assert_journal_finish_exit "$E2E_TMP/journal.jsonl" "enable-fpm" 1
assert_journal_has_step "$E2E_TMP/journal.jsonl" "rollback-remove-packages"
assert_journal_finish_exit "$E2E_TMP/journal.jsonl" "rollback-remove-packages" 0
assert_no_password "$CID"

assert_package_absent "$CID" "php${VERSION}-cli"
assert_package_absent "$CID" "php${VERSION}-common"
assert_package_absent "$CID" "php${VERSION}-fpm"
assert_package_absent "$CID" "php${VERSION}-opcache"
assert_no_php "$CID"

log "OK: rollback após falha em $IMAGE PHP $VERSION"
