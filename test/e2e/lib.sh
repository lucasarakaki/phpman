#!/usr/bin/env bash
# Shared helpers for the phpman end-to-end tests. These tests are intentionally
# outside the Go test suite: they need a privileged container with systemd and
# real apt/systemctl state.

set -euo pipefail

E2E_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
E2E_TMP="${E2E_TMP:-$(mktemp -d)}"
E2E_BIN="${PHPMAN_BIN:-$E2E_ROOT/bin/phpman-e2e}"

log() {
    printf '==> %s\n' "$*" >&2
}

die() {
    printf 'erro: %s\n' "$*" >&2
    exit 1
}

require_cmd() {
    local cmd
    for cmd in "$@"; do
        command -v "$cmd" >/dev/null 2>&1 || die "comando obrigatório não encontrado: $cmd"
    done
}

build_e2e_binary() {
    if [[ -n "${PHPMAN_BIN:-}" && -x "$PHPMAN_BIN" ]]; then
        printf '%s\n' "$PHPMAN_BIN"
        return
    fi
    log "compilando binário estático para o E2E"
    (
        cd "$E2E_ROOT"
        CGO_ENABLED=0 go build -trimpath \
            -ldflags '-s -w -X main.version=0.2.0-e2e' \
            -o "$E2E_BIN" ./cmd/phpman
    )
    printf '%s\n' "$E2E_BIN"
}

build_image() {
    local image="$1"
    local tag="phpman-e2e:$(printf '%s' "$image" | tr '/:' '__')"
    log "construindo imagem $tag a partir de $image"
    docker build \
        --build-arg "BASE_IMAGE=$image" \
        -f "$E2E_ROOT/test/e2e/Dockerfile" \
        -t "$tag" \
        "$E2E_ROOT/test/e2e" >&2
    printf '%s\n' "$tag"
}

start_systemd_container() {
    local tag="$1"
    local name="$2"
    local cid
    cid="$(docker run -d --name "$name" \
        --privileged \
        --cgroupns=host \
        -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
        --tmpfs /run \
        --tmpfs /tmp \
        "$tag")"
    wait_systemd "$cid"
    printf '%s\n' "$cid"
}

start_plain_container() {
    local tag="$1"
    local name="$2"
    docker run -d --name "$name" \
        --entrypoint /bin/bash \
        "$tag" \
        -c 'sleep infinity'
}

wait_systemd() {
    local cid="$1"
    local i state
    for i in $(seq 1 60); do
        state="$(docker exec "$cid" systemctl is-system-running 2>/dev/null || true)"
        case "$state" in
            running|degraded)
                log "systemd pronto ($state)"
                return 0
                ;;
        esac
        sleep 1
    done
    docker logs "$cid" >&2 || true
    die "systemd não ficou pronto no container $cid"
}

prepare_container() {
    local cid="$1"
    local bin="$2"
    log "preparando usuário e binário no container"
    docker exec "$cid" apt-get update -qq
    docker exec "$cid" bash -c '
        if ! id -u tester >/dev/null 2>&1; then
            useradd -m -s /bin/bash tester
        fi
        printf "%s\n" "tester ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/tester
        chmod 0440 /etc/sudoers.d/tester
    '
    docker cp "$bin" "$cid:/usr/local/bin/phpman"
    docker exec "$cid" chown root:root /usr/local/bin/phpman
    docker exec "$cid" chmod 0755 /usr/local/bin/phpman
    docker exec -u tester -e HOME=/home/tester "$cid" sudo -n true
}

phpman_as() {
    local cid="$1"
    shift
    docker exec -u tester -e HOME=/home/tester "$cid" /usr/local/bin/phpman "$@"
}

truncate_journal() {
    local cid="$1"
    docker exec "$cid" rm -f /var/lib/phpman/journal.jsonl
}

copy_journal() {
    local cid="$1"
    local dest="$2"
    docker cp "$cid:/var/lib/phpman/journal.jsonl" "$dest"
}

assert_no_php() {
    local cid="$1"
    local count
    count="$(phpman_as "$cid" status --json | jq -r '.versions | length')"
    if [[ "$count" != "0" ]]; then
        die "container deveria começar sem PHP, encontrou $count versão(ões)"
    fi
}

assert_package_installed() {
    local cid="$1"
    local pkg="$2"
    local status
    status="$(docker exec "$cid" dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null || true)"
    if [[ "$status" != *"install ok installed"* ]]; then
        die "pacote $pkg não está instalado (status: $status)"
    fi
}

assert_package_absent() {
    local cid="$1"
    local pkg="$2"
    local status
    status="$(docker exec "$cid" dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null || true)"
    if [[ "$status" == *"install ok installed"* ]]; then
        die "pacote $pkg ainda está instalado"
    fi
}

assert_fpm_active() {
    local cid="$1"
    local version="$2"
    docker exec "$cid" systemctl is-active --quiet "php${version}-fpm.service" \
        || die "php${version}-fpm.service não está ativo"
}

assert_socket_exists() {
    local cid="$1"
    local version="$2"
    docker exec "$cid" test -S "/run/php/php${version}-fpm.sock" \
        || die "socket /run/php/php${version}-fpm.sock não existe"
}

assert_active_version() {
    local cid="$1"
    local want="$2"
    local got
    got="$(phpman_as "$cid" status --json | jq -r '.active_version // ""')"
    if [[ "$got" != "$want" ]]; then
        die "versão ativa = ${got:-<vazia>}, esperado $want"
    fi
}

assert_php_version() {
    local cid="$1"
    local want="$2"
    local got
    got="$(docker exec -u tester -e HOME=/home/tester "$cid" php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')"
    if [[ "$got" != "$want" ]]; then
        die "php -r reportou $got, esperado $want"
    fi
}

assert_status_has_version() {
    local cid="$1"
    local version="$2"
    if ! phpman_as "$cid" status --json | jq -e --arg v "$version" '.versions[] | select(.raw == $v)' >/dev/null; then
        die "phpman status não encontrou PHP $version"
    fi
}

assert_status_lacks_version() {
    local cid="$1"
    local version="$2"
    if phpman_as "$cid" status --json | jq -e --arg v "$version" '.versions[] | select(.raw == $v)' >/dev/null; then
        die "phpman status ainda encontrou PHP $version"
    fi
}

assert_journal_balanced() {
    local journal="$1"
    jq -s -e '
        (group_by(.step) | all(.[]; (map(select(.phase == "start")) | length) == (map(select(.phase == "finish")) | length)))
        and (all(.[]; .phase != "finish" or (.exit != null)))
    ' "$journal" >/dev/null || die "journal com início/fim desbalanceado"
}

assert_journal_has_step() {
    local journal="$1"
    local step="$2"
    jq -s -e --arg step "$step" 'any(.[]; .step == $step and .phase == "start")' "$journal" >/dev/null \
        || die "journal não registrou o passo $step"
}

assert_journal_finish_exit() {
    local journal="$1"
    local step="$2"
    local want="$3"
    jq -s -e --arg step "$step" --argjson want "$want" \
        'any(.[]; .step == $step and .phase == "finish" and .exit == $want)' "$journal" >/dev/null \
        || die "passo $step não terminou com exit $want"
}

assert_no_password() {
    local cid="$1"
    docker exec "$cid" bash -c '
        test ! -f /var/lib/phpman/journal.jsonl \
            || ! grep -Eiq "password|senha|SUDO_ASKPASS" /var/lib/phpman/journal.jsonl
    ' || die "journal contém possível vazamento de senha"
}

# assert_contract compares the executable steps in the dry-run JSON with the
# start events in the helper journal. The helper resolves argv[0] to an
# absolute path, so the comparison uses the basename of argv[0].
assert_contract() {
    local dry_run="$1"
    local journal="$2"
    local expected="$E2E_TMP/contract-expected.json"
    local actual="$E2E_TMP/contract-actual.json"

    jq -S -c '[.steps[] | select(.argv != null or .file != null) | {
        id: .id,
        argv: (if .argv then ((.argv[0] | split("/") | last) as $cmd | [$cmd] + .argv[1:]) else null end),
        file: (if .file then .file.path else null end)
    }]' "$dry_run" >"$expected"

    jq -S -c -s '[.[] | select(.phase == "start") | {
        id: .step,
        argv: (if .argv then ((.argv[0] | split("/") | last) as $cmd | [$cmd] + .argv[1:]) else null end),
        file: (if .file then .file else null end)
    }]' "$journal" >"$actual"

    if ! diff -u "$expected" "$actual"; then
        die "contrato dry-run × journal divergiu"
    fi
}
