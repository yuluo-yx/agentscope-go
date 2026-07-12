#!/usr/bin/env bash
set -euo pipefail

TEMP_ROOT="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
VENV="${OPENSANDBOX_SERVER_VENV:-${TEMP_ROOT}/opensandbox-server-venv}"
SERVER="${VENV}/bin/opensandbox-server"
CONFIG="${TEMP_ROOT}/opensandbox.toml"
LOG="${TEMP_ROOT}/opensandbox-server.log"
PID_FILE="${TEMP_ROOT}/opensandbox-server.pid"
TIMEOUT="${OPENSANDBOX_E2E_TIMEOUT:-25m}"
GO="${GO:-go}"
STARTED_SERVER=false

log() {
	printf '\n[opensandbox] %s\n' "$*"
}

dump_diagnostics() {
	log "server diagnostics"
	if [[ -f "${LOG}" ]]; then
		cat "${LOG}"
	fi
	docker ps -a --filter 'label=opensandbox' || true
}

cleanup() {
	local status=$?
	trap - EXIT
	if [[ "${STARTED_SERVER}" == true ]]; then
		if [[ -f "${PID_FILE}" ]]; then
			kill "$(cat "${PID_FILE}")" 2>/dev/null || true
			wait "$(cat "${PID_FILE}")" 2>/dev/null || true
		fi
		docker ps -aq --filter 'label=opensandbox' \
			| xargs -r docker rm -f >/dev/null 2>&1 || true
	fi
	exit "${status}"
}

trap cleanup EXIT

if [[ -z "${OPEN_SANDBOX_DOMAIN:-}" ]]; then
	if [[ ! -x "${SERVER}" ]]; then
		echo "opensandbox-server is missing; run tools/opensandbox/install-server.sh" >&2
		exit 1
	fi

	log "starting local server"
	"${SERVER}" init-config "${CONFIG}" --example docker
	OPENSANDBOX_INSECURE_SERVER=YES \
		"${SERVER}" --config "${CONFIG}" >"${LOG}" 2>&1 &
	echo "$!" >"${PID_FILE}"
	STARTED_SERVER=true

	for attempt in {1..120}; do
		if curl --fail --silent http://127.0.0.1:8080/health >/dev/null; then
			break
		fi
		if ! kill -0 "$(cat "${PID_FILE}")" 2>/dev/null; then
			dump_diagnostics
			exit 1
		fi
		if [[ "${attempt}" -eq 120 ]]; then
			dump_diagnostics
			exit 1
		fi
		sleep 1
	done

	export OPEN_SANDBOX_DOMAIN=127.0.0.1:8080
	export OPEN_SANDBOX_API_KEY=
	export OPEN_SANDBOX_PROTOCOL=http
fi

log "running workspace E2E"
if ! "${GO}" test -tags=integration -timeout="${TIMEOUT}" \
	./pkg/workspace/opensandbox; then
	dump_diagnostics
	exit 1
fi
