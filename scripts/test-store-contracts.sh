#!/usr/bin/env bash
set -euo pipefail

# test-store-contracts.sh — Test store contract harness against SQLite and ephemeral MariaDB
# Usage:
#   bash scripts/test-store-contracts.sh sqlite
#   bash scripts/test-store-contracts.sh mariadb
#   bash scripts/test-store-contracts.sh all

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/test/mariadb/compose.yml"

BACKEND="${1:-sqlite}"
case "${BACKEND}" in
    sqlite|mariadb|all) ;;
    *)
        echo "Usage: $0 [sqlite|mariadb|all]" >&2
        exit 1
        ;;
esac

run_sqlite() {
    echo "==> Running store contract tests against SQLite..."
    (
        cd "${REPO_ROOT}"
        WACALLS_TEST_BACKEND="sqlite" go test ./internal/testdb/... -run '^TestStoreHarnessContract$' -count=1 -timeout=5m
    )
}

detect_docker_compose() {
    if docker compose version >/dev/null 2>&1; then
        DOCKER_COMPOSE_CMD=(docker compose)
    elif command -v docker-compose >/dev/null 2>&1; then
        DOCKER_COMPOSE_CMD=(docker-compose)
    else
        echo "ERROR: Neither 'docker compose' nor 'docker-compose' found in PATH." >&2
        return 1
    fi
    return 0
}

generate_random_string() {
    local len="${1:-16}"
    LC_ALL=C tr -dc 'a-zA-Z0-9' < /dev/urandom 2>/dev/null | head -c "${len}" || true
}

CLEANUP_DONE="false"
PROJECT_NAME=""
DOCKER_COMPOSE_CMD=()

cleanup() {
    local exit_code=$?
    if [[ "${CLEANUP_DONE}" == "true" ]]; then
        return ${exit_code}
    fi
    CLEANUP_DONE="true"

    if [[ -n "${PROJECT_NAME}" ]]; then
        if [[ "${PROJECT_NAME}" != wacalls-store-contract-* ]]; then
            echo "ERROR: Refusing to clean up invalid project name: '${PROJECT_NAME}'" >&2
            return 1
        fi

        echo "==> Cleaning up MariaDB test environment for project '${PROJECT_NAME}'..."
        "${DOCKER_COMPOSE_CMD[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
    fi
    return ${exit_code}
}
trap cleanup EXIT INT TERM

run_mariadb() {
    echo "==> Preparing ephemeral MariaDB environment..."

    if ! command -v docker >/dev/null 2>&1; then
        echo "ERROR: 'docker' CLI not found." >&2
        return 1
    fi

    if ! docker info >/dev/null 2>&1; then
        echo "ERROR: Docker daemon is not accessible or not running." >&2
        return 1
    fi

    if ! detect_docker_compose; then
        return 1
    fi

    if [[ ! -f "${COMPOSE_FILE}" ]]; then
        echo "ERROR: Compose file not found: ${COMPOSE_FILE}" >&2
        return 1
    fi

    local random_suffix
    random_suffix=$(LC_ALL=C tr -dc 'a-z0-9' < /dev/urandom 2>/dev/null | head -c 8 || echo "$$")
    PROJECT_NAME="wacalls-store-contract-${random_suffix}"
    local db_name="wacalls_store_test_${random_suffix}"
    local db_user="wacalls_test_${random_suffix}"
    local root_pass
    root_pass=$(generate_random_string 24)
    local user_pass
    user_pass=$(generate_random_string 24)

    echo "==> Starting ephemeral MariaDB container (${PROJECT_NAME})..."
    MARIADB_ROOT_PASSWORD="${root_pass}" \
    MARIADB_DATABASE="${db_name}" \
    MARIADB_USER="${db_user}" \
    MARIADB_PASSWORD="${user_pass}" \
    "${DOCKER_COMPOSE_CMD[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" up -d

    echo "==> Waiting for MariaDB readiness (max 90s)..."
    local ready="false"
    local deadline=$((SECONDS + 90))

    while [[ ${SECONDS} -lt ${deadline} ]]; do
        # Verify container health and port mapping
        local port_mapping
        port_mapping=$("${DOCKER_COMPOSE_CMD[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" port mariadb 3306 2>/dev/null || true)
        if [[ -n "${port_mapping}" ]]; then
            local port
            port="${port_mapping##*:}"
            # Check SQL connectivity using the test user
            if docker run --rm --network host mariadb:11.4 mariadb -h 127.0.0.1 -P "${port}" -u "${db_user}" -p"${user_pass}" "${db_name}" -e "SELECT 1;" >/dev/null 2>&1 || \
               "${DOCKER_COMPOSE_CMD[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" exec -T mariadb mariadb -u "${db_user}" -p"${user_pass}" "${db_name}" -e "SELECT 1;" >/dev/null 2>&1; then
                ready="true"
                break
            fi
        fi
        sleep 2
    done

    if [[ "${ready}" != "true" ]]; then
        echo "ERROR: MariaDB failed to become ready within 90 seconds." >&2
        return 1
    fi

    local port_mapping
    port_mapping=$("${DOCKER_COMPOSE_CMD[@]}" -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" port mariadb 3306)
    local host_port="${port_mapping##*:}"

    local mariadb_dsn="${db_user}:${user_pass}@tcp(127.0.0.1:${host_port})/${db_name}?parseTime=true&timeout=10s"

    echo "==> Running store contract tests against MariaDB 11.4 (port ${host_port})..."
    (
        cd "${REPO_ROOT}"
        WACALLS_TEST_BACKEND="mariadb" \
        WACALLS_TEST_MARIADB_DSN="${mariadb_dsn}" \
        go test ./internal/testdb/... -run '^TestStoreHarnessContract$' -count=1 -timeout=5m
    )
}

case "${BACKEND}" in
    sqlite)
        run_sqlite
        ;;
    mariadb)
        run_mariadb
        ;;
    all)
        run_sqlite
        run_mariadb
        ;;
esac

echo "==> All requested store contract tests completed successfully."
