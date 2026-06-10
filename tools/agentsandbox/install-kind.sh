#!/usr/bin/env bash
set -euo pipefail

KIND_VERSION="${KIND_VERSION:-v0.30.0}"
KUBECTL_VERSION="${KUBECTL_VERSION:-v1.34.2}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

platform() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"

  case "${os}" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) echo "unsupported OS: ${os}" >&2; exit 1 ;;
  esac

  case "${arch}" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;;
  esac

  echo "${os}/${arch}"
}

install_binary() {
  local source target
  source="$1"
  target="$2"

  chmod +x "${source}"
  if [ -w "${INSTALL_DIR}" ]; then
    mv "${source}" "${INSTALL_DIR}/${target}"
  else
    sudo mv "${source}" "${INSTALL_DIR}/${target}"
  fi
}

if ! command -v kind >/dev/null 2>&1; then
  IFS=/ read -r os arch <<<"$(platform)"
  curl -fsSLo kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-${os}-${arch}"
  install_binary kind kind
fi

if ! command -v kubectl >/dev/null 2>&1; then
  IFS=/ read -r os arch <<<"$(platform)"
  curl -fsSLo kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${os}/${arch}/kubectl"
  install_binary kubectl kubectl
fi

kind version
kubectl version --client=true
