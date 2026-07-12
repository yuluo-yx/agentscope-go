#!/usr/bin/env bash
set -euo pipefail

VERSION="${OPENSANDBOX_SERVER_VERSION:-0.2.1}"
TEMP_ROOT="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
VENV="${OPENSANDBOX_SERVER_VENV:-${TEMP_ROOT}/opensandbox-server-venv}"
PYTHON="${PYTHON:-python3}"

printf '\n[opensandbox] installing opensandbox-server %s\n' "${VERSION}"
"${PYTHON}" -m venv "${VENV}"
"${VENV}/bin/pip" install "opensandbox-server==${VERSION}"
