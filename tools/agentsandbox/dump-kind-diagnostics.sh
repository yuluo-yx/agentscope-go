#!/usr/bin/env bash
set -euo pipefail

CLUSTER="${AGENT_SANDBOX_KIND_CLUSTER:-agentscope-agent-sandbox}"
NAMESPACE="${AGENTSCOPE_AGENT_SANDBOX_NAMESPACE:-default}"

log() {
	printf '\n[agent-sandbox-kind] %s\n' "$*"
}

use_kind_context() {
	if command -v kind >/dev/null 2>&1 && kind get clusters 2>/dev/null | grep -qx "${CLUSTER}"; then
		kubectl config use-context "kind-${CLUSTER}" >/dev/null 2>&1 || kind export kubeconfig --name "${CLUSTER}" >/dev/null 2>&1 || true
	fi
}

dump_router_diagnostics() {
	log "sandbox router diagnostics"
	kubectl get svc sandbox-router-svc -o wide || true
	kubectl get svc,deploy,pods -l app=sandbox-router -o wide || true
	kubectl describe deploy/sandbox-router-deployment || true

	for pod in $(kubectl get pods -l app=sandbox-router -o name 2>/dev/null || true); do
		log "describe ${pod}"
		kubectl describe "${pod}" || true
		log "logs ${pod}"
		kubectl logs "${pod}" --all-containers --tail=160 || true
		log "previous logs ${pod}"
		kubectl logs "${pod}" --all-containers --previous --tail=160 || true
	done
}

dump_controller_diagnostics() {
	log "agent-sandbox controller diagnostics"
	kubectl -n agent-sandbox-system get deploy,pods -o wide || true
	for pod in $(kubectl -n agent-sandbox-system get pods -o name 2>/dev/null || true); do
		log "describe agent-sandbox-system/${pod}"
		kubectl -n agent-sandbox-system describe "${pod}" || true
		log "logs agent-sandbox-system/${pod}"
		kubectl -n agent-sandbox-system logs "${pod}" --all-containers --tail=200 || true
		log "previous logs agent-sandbox-system/${pod}"
		kubectl -n agent-sandbox-system logs "${pod}" --all-containers --previous --tail=200 || true
	done
}

dump_namespace_diagnostics() {
	ns="$1"
	log "namespace ${ns} diagnostics"
	kubectl -n "${ns}" get deploy,svc,pods,sandboxclaims,sandboxes,sandboxtemplates,sandboxwarmpools -o wide || true
	kubectl -n "${ns}" describe sandboxclaims || true
	kubectl -n "${ns}" describe sandboxes || true
	kubectl -n "${ns}" describe pods || true
	kubectl -n "${ns}" get events --sort-by='.metadata.creationTimestamp' || true
}

log "dumping kind and Kubernetes diagnostics"
use_kind_context
kind get clusters || true
kubectl version --client=true || true
kubectl cluster-info || true
kubectl get nodes -o wide || true
kubectl get pods -A -o wide || true
kubectl get sandboxclaims,sandboxes,sandboxtemplates,sandboxwarmpools -A -o wide || true
dump_namespace_diagnostics "agent-sandbox-system"
dump_namespace_diagnostics "${NAMESPACE}"
dump_router_diagnostics
dump_controller_diagnostics
