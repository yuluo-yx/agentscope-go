#!/usr/bin/env bash
set -euo pipefail

curl -fsSLo kind https://kind.sigs.k8s.io/dl/v0.30.0/kind-linux-amd64
chmod +x kind
sudo mv kind /usr/local/bin/kind
curl -fsSLo kubectl https://dl.k8s.io/release/v1.34.2/bin/linux/amd64/kubectl
chmod +x kubectl
sudo mv kubectl /usr/local/bin/kubectl
kind version
kubectl version --client=true
