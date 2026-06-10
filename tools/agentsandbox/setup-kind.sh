#!/usr/bin/env bash
set -euo pipefail

VERSION="${AGENT_SANDBOX_VERSION:-v0.4.6}"
CLUSTER="${AGENT_SANDBOX_KIND_CLUSTER:-agentscope-agent-sandbox}"
NAMESPACE="${AGENTSCOPE_AGENT_SANDBOX_NAMESPACE:-default}"
TEMPLATE="${AGENTSCOPE_AGENT_SANDBOX_TEMPLATE:-python-sandbox-template}"
ROUTER_IMAGE="${AGENT_SANDBOX_ROUTER_IMAGE:-agentscope-agent-sandbox-router:${VERSION}}"
RUNTIME_IMAGE="${AGENT_SANDBOX_RUNTIME_IMAGE:-agentscope-agent-sandbox-runtime:${VERSION}}"
CONTROLLER_IMAGE="${AGENT_SANDBOX_CONTROLLER_IMAGE:-registry.k8s.io/agent-sandbox/agent-sandbox-controller:${VERSION}}"
PRELOAD_CONTROLLER_IMAGE="${AGENT_SANDBOX_PRELOAD_CONTROLLER_IMAGE:-true}"
BUILD_ROUTER_IMAGE="${AGENT_SANDBOX_BUILD_ROUTER_IMAGE:-true}"
BUILD_RUNTIME_IMAGE="${AGENT_SANDBOX_BUILD_RUNTIME_IMAGE:-true}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmpdir=""
cluster_exists=false

log() {
	printf '\n[agent-sandbox-kind] %s\n' "$*"
}

cleanup() {
	if [ -n "${tmpdir}" ]; then
		rm -rf "${tmpdir}"
	fi
}

fetch_agent_sandbox_file() {
	relpath="$1"
	output="$2"
	curl --connect-timeout 10 --retry 3 --retry-all-errors --retry-delay 2 -fsSL \
		"https://raw.githubusercontent.com/kubernetes-sigs/agent-sandbox/refs/tags/${VERSION}/${relpath}" \
		-o "${output}"
}

bool_enabled() {
	case "$1" in
		1 | true | TRUE | yes | YES | on | ON)
			return 0
			;;
	esac
	return 1
}

dump_router_diagnostics() {
	log "sandbox router deployment diagnostics"
	kubectl get svc sandbox-router-svc -o wide || true
	kubectl get svc,deploy,pods -l app=sandbox-router -o wide || true
	kubectl describe deploy/sandbox-router-deployment || true

	for pod in $(kubectl get pods -l app=sandbox-router -o name 2>/dev/null || true); do
		log "describe ${pod}"
		kubectl describe "${pod}" || true
		log "logs ${pod}"
		kubectl logs "${pod}" --all-containers --tail=120 || true
		log "previous logs ${pod}"
		kubectl logs "${pod}" --all-containers --previous --tail=120 || true
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

dump_diagnostics() {
	log "setup failed; dumping kind and Kubernetes diagnostics"
	kind get clusters || true
	kubectl version --client=true || true
	kubectl cluster-info || true
	kubectl get nodes -o wide || true
	kubectl get pods -A -o wide || true
	kubectl get sandboxclaims,sandboxes,sandboxtemplates,sandboxwarmpools -A -o wide || true
	dump_namespace_diagnostics agent-sandbox-system
	dump_namespace_diagnostics "${NAMESPACE}"
	dump_router_diagnostics
	dump_controller_diagnostics
}

on_exit() {
	status=$?
	if [ "${status}" -ne 0 ]; then
		dump_diagnostics
	fi
	cleanup
	exit "${status}"
}

trap on_exit EXIT

if ! command -v kind >/dev/null 2>&1; then
	echo "kind is required to set up Agent Sandbox tests" >&2
	exit 1
fi
if ! command -v kubectl >/dev/null 2>&1; then
	echo "kubectl is required to set up Agent Sandbox tests" >&2
	exit 1
fi
if (bool_enabled "${BUILD_ROUTER_IMAGE}" || bool_enabled "${BUILD_RUNTIME_IMAGE}") && ! command -v docker >/dev/null 2>&1; then
	echo "docker is required to build and load Agent Sandbox test images" >&2
	exit 1
fi

log "using Agent Sandbox ${VERSION}; kind cluster=${CLUSTER}; namespace=${NAMESPACE}; template=${TEMPLATE}; controller image=${CONTROLLER_IMAGE}; router image=${ROUTER_IMAGE}; runtime image=${RUNTIME_IMAGE}"
kind version
kubectl version --client=true
if bool_enabled "${PRELOAD_CONTROLLER_IMAGE}" || bool_enabled "${BUILD_ROUTER_IMAGE}" || bool_enabled "${BUILD_RUNTIME_IMAGE}"; then
	docker version
fi

if ! kind get clusters | grep -qx "${CLUSTER}"; then
	log "creating kind cluster ${CLUSTER}"
	kind create cluster --name "${CLUSTER}" --wait 60s
else
	cluster_exists=true
	log "reusing kind cluster ${CLUSTER}"
fi

log "cluster info"
kubectl cluster-info
kubectl get nodes -o wide

log "creating namespace ${NAMESPACE}"
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

if bool_enabled "${PRELOAD_CONTROLLER_IMAGE}"; then
	log "preloading Agent Sandbox controller image ${CONTROLLER_IMAGE} into kind cluster ${CLUSTER}"
	docker image inspect "${CONTROLLER_IMAGE}" >/dev/null 2>&1 || docker pull "${CONTROLLER_IMAGE}"
	kind load docker-image "${CONTROLLER_IMAGE}" --name "${CLUSTER}"
fi

log "installing Agent Sandbox controller and extensions"
kubectl apply -f "https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${VERSION}/manifest.yaml"
kubectl apply -f "https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${VERSION}/extensions.yaml"
if bool_enabled "${cluster_exists}"; then
	kubectl -n agent-sandbox-system delete pods -l app=agent-sandbox-controller --ignore-not-found --wait=false
fi

log "waiting for Agent Sandbox controller deployments"
kubectl -n agent-sandbox-system wait --for=condition=Available deploy --all --timeout=180s
kubectl -n agent-sandbox-system get deploy,pods -o wide

tmpdir="$(mktemp -d)"

if bool_enabled "${BUILD_ROUTER_IMAGE}"; then
	log "building sandbox router image ${ROUTER_IMAGE} from Agent Sandbox ${VERSION}"
	router_dir="${tmpdir}/sandbox-router"
	mkdir -p "${router_dir}"
	fetch_agent_sandbox_file "clients/python/agentic-sandbox-client/sandbox-router/Dockerfile" "${router_dir}/Dockerfile"
	fetch_agent_sandbox_file "clients/python/agentic-sandbox-client/sandbox-router/requirements.txt" "${router_dir}/requirements.txt"
	fetch_agent_sandbox_file "clients/python/agentic-sandbox-client/sandbox-router/sandbox_router.py" "${router_dir}/sandbox_router.py"
	docker build -t "${ROUTER_IMAGE}" "${router_dir}"

	log "loading sandbox router image ${ROUTER_IMAGE} into kind cluster ${CLUSTER}"
	kind load docker-image "${ROUTER_IMAGE}" --name "${CLUSTER}"
fi

if bool_enabled "${BUILD_RUNTIME_IMAGE}"; then
	log "building AgentScope sandbox runtime image ${RUNTIME_IMAGE}"
	docker build -t "${RUNTIME_IMAGE}" "${SCRIPT_DIR}/runtime"

	log "loading AgentScope sandbox runtime image ${RUNTIME_IMAGE} into kind cluster ${CLUSTER}"
	kind load docker-image "${RUNTIME_IMAGE}" --name "${CLUSTER}"
fi

log "installing sandbox router image ${ROUTER_IMAGE}"
sed "s|\${ROUTER_IMAGE}|${ROUTER_IMAGE}|g" "${SCRIPT_DIR}/sandbox-router.yml" | kubectl apply -f -
kubectl rollout status deploy/sandbox-router-deployment --timeout=180s
kubectl get deploy,pods -o wide

log "installing sandbox template ${TEMPLATE}"
sed \
	-e "s|\${SANDBOX_TEMPLATE_NAME}|${TEMPLATE}|g" \
	-e "s|\${SANDBOX_NAMESPACE}|${NAMESPACE}|g" \
	-e "s|\${RUNTIME_IMAGE}|${RUNTIME_IMAGE}|g" \
	"${SCRIPT_DIR}/python-sandbox-template.yml" | kubectl apply -f -

log "sandbox template status"
kubectl get sandboxtemplate -n "${NAMESPACE}" "${TEMPLATE}"
