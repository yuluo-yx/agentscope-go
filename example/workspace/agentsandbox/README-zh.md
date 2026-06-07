# Agent Sandbox Workspace 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 `workspace/agentsandbox.Workspace`：

- 通过 agent-sandbox Go SDK 创建 Kubernetes `SandboxClaim`。
- 使用 `SandboxTemplate` 启动 Python runtime sandbox。
- 使用 workspace `Write` 和 `Read` 工具操作 sandbox 内 `/home/user` 下的文件。
- 将 offload、Skill 和 MCP 索引写入宿主机临时 mirror 目录。
- 在存在 DashScope API Key 时，把工具读取结果发送给 DashScope ChatModel。

## 前置条件

- Go 1.26.3。
- 可访问的 Kubernetes 集群。
- 已安装 agent-sandbox controller、extensions、sandbox-router。
- 当前 kubeconfig 有权限创建 `SandboxClaim`。
- 目标 namespace 中存在 `SandboxTemplate`，默认名称为 `python-sandbox-template`。
- 可选：DashScope API Key，用于真实 ChatModel 调用。

可以用仓库根目录的 kind target 准备测试环境：

```bash
make test-e2e-agent-sandbox
```

该 target 会创建 kind 集群并安装 agent-sandbox 资源，然后运行集成和 E2E 测试。

## 在 Docker k3s 集群中准备环境

本节适用于 k3s 运行在远端 Docker 容器中的场景。示例会在本地运行，Kubernetes 资源创建在远端 k3s 集群中，DashScope API Key 只保留在本地环境变量中。

以下命令使用变量表示远端信息。若本机已有 `ssh@2450` 这类 zsh 函数，可以直接用该函数登录；脚本化命令建议使用标准 `ssh` 形式。

```bash
export REMOTE=root@<server-ip>
export K3S_CONTAINER=nginx-k3s-server
export K3S_API_HOST_PORT=64436
export AGENT_SANDBOX_VERSION=v0.4.6
export AGENTSCOPE_AGENT_SANDBOX_NAMESPACE=agent
export AGENTSCOPE_AGENT_SANDBOX_TEMPLATE=python-sandbox-template
export AGENT_SANDBOX_ROUTER_IMAGE=agentscope-agent-sandbox-router:${AGENT_SANDBOX_VERSION}
export AGENT_SANDBOX_RUNTIME_IMAGE=agentscope-agent-sandbox-runtime:${AGENT_SANDBOX_VERSION}
export REMOTE_WORKDIR=/tmp/agentscope-k3s-agent
```

先确认 Docker 容器和 k3s API 可用：

```bash
ssh "${REMOTE}" "docker ps --filter name=${K3S_CONTAINER}"
ssh "${REMOTE}" "docker exec ${K3S_CONTAINER} kubectl get nodes -o wide"
```

### 安装 Agent Sandbox CRD 和 controller

远端容器内的 `kubectl apply -f https://...` 可能受网络影响。推荐先在远端主机下载官方 release YAML，再复制到 k3s 容器内执行 `kubectl apply`。

```bash
ssh "${REMOTE}" "mkdir -p ${REMOTE_WORKDIR}"

ssh "${REMOTE}" "
set -euo pipefail
curl --connect-timeout 10 --retry 2 --retry-all-errors --retry-delay 2 -fsSL \
  https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${AGENT_SANDBOX_VERSION}/manifest.yaml \
  -o ${REMOTE_WORKDIR}/manifest.yaml
curl --connect-timeout 10 --retry 2 --retry-all-errors --retry-delay 2 -fsSL \
  https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${AGENT_SANDBOX_VERSION}/extensions.yaml \
  -o ${REMOTE_WORKDIR}/extensions.yaml
"

ssh "${REMOTE}" "
set -euo pipefail
docker exec ${K3S_CONTAINER} mkdir -p ${REMOTE_WORKDIR}
docker cp ${REMOTE_WORKDIR}/manifest.yaml ${K3S_CONTAINER}:${REMOTE_WORKDIR}/manifest.yaml
docker cp ${REMOTE_WORKDIR}/extensions.yaml ${K3S_CONTAINER}:${REMOTE_WORKDIR}/extensions.yaml
docker exec ${K3S_CONTAINER} kubectl create namespace ${AGENTSCOPE_AGENT_SANDBOX_NAMESPACE} --dry-run=client -o yaml | \
  docker exec -i ${K3S_CONTAINER} kubectl apply -f -
docker exec ${K3S_CONTAINER} kubectl apply -f ${REMOTE_WORKDIR}/manifest.yaml
docker exec ${K3S_CONTAINER} kubectl apply -f ${REMOTE_WORKDIR}/extensions.yaml
docker exec ${K3S_CONTAINER} kubectl -n agent-sandbox-system wait --for=condition=Available deploy --all --timeout=180s
"
```

验证 CRD 和 controller：

```bash
ssh "${REMOTE}" "docker exec ${K3S_CONTAINER} kubectl get crd | grep -E 'sandbox|agents'"
ssh "${REMOTE}" "docker exec ${K3S_CONTAINER} kubectl -n agent-sandbox-system get deploy,pods -o wide"
```

### 构建并导入 router 和 runtime 镜像

k3s 容器使用自己的 containerd。仅在远端 Docker 构建镜像还不够，需要把镜像导入 k3s 的 `k8s.io` containerd namespace。

```bash
ssh "${REMOTE}" "mkdir -p ${REMOTE_WORKDIR}/runtime ${REMOTE_WORKDIR}/router"
rsync -az --delete tools/agentsandbox/runtime/ "${REMOTE}:${REMOTE_WORKDIR}/runtime/"
rsync -az tools/agentsandbox/python-sandbox-template.yml tools/agentsandbox/sandbox-router.yml "${REMOTE}:${REMOTE_WORKDIR}/"

ssh "${REMOTE}" "
set -euo pipefail
curl --connect-timeout 10 --retry 2 --retry-all-errors --retry-delay 2 -fsSL \
  https://raw.githubusercontent.com/kubernetes-sigs/agent-sandbox/refs/tags/${AGENT_SANDBOX_VERSION}/clients/python/agentic-sandbox-client/sandbox-router/Dockerfile \
  -o ${REMOTE_WORKDIR}/router/Dockerfile
curl --connect-timeout 10 --retry 2 --retry-all-errors --retry-delay 2 -fsSL \
  https://raw.githubusercontent.com/kubernetes-sigs/agent-sandbox/refs/tags/${AGENT_SANDBOX_VERSION}/clients/python/agentic-sandbox-client/sandbox-router/requirements.txt \
  -o ${REMOTE_WORKDIR}/router/requirements.txt
curl --connect-timeout 10 --retry 2 --retry-all-errors --retry-delay 2 -fsSL \
  https://raw.githubusercontent.com/kubernetes-sigs/agent-sandbox/refs/tags/${AGENT_SANDBOX_VERSION}/clients/python/agentic-sandbox-client/sandbox-router/sandbox_router.py \
  -o ${REMOTE_WORKDIR}/router/sandbox_router.py

docker build --progress=plain -t ${AGENT_SANDBOX_RUNTIME_IMAGE} ${REMOTE_WORKDIR}/runtime
docker build --progress=plain -t ${AGENT_SANDBOX_ROUTER_IMAGE} ${REMOTE_WORKDIR}/router
docker save ${AGENT_SANDBOX_RUNTIME_IMAGE} ${AGENT_SANDBOX_ROUTER_IMAGE} \
  -o ${REMOTE_WORKDIR}/agentscope-agent-sandbox-images.tar
docker cp ${REMOTE_WORKDIR}/agentscope-agent-sandbox-images.tar \
  ${K3S_CONTAINER}:${REMOTE_WORKDIR}/agentscope-agent-sandbox-images.tar
docker exec ${K3S_CONTAINER} ctr --address /run/k3s/containerd/containerd.sock \
  --namespace k8s.io images import ${REMOTE_WORKDIR}/agentscope-agent-sandbox-images.tar
"
```

确认 k3s containerd 已识别镜像：

```bash
ssh "${REMOTE}" "docker exec ${K3S_CONTAINER} ctr --address /run/k3s/containerd/containerd.sock --namespace k8s.io images list | grep agentscope-agent-sandbox"
```

### 部署 sandbox-router 和 SandboxTemplate

将 router 部署到运行示例的 namespace，并创建 `python-sandbox-template`：

```bash
ssh "${REMOTE}" "
set -euo pipefail
sed \"s|\\\${ROUTER_IMAGE}|${AGENT_SANDBOX_ROUTER_IMAGE}|g\" \
  ${REMOTE_WORKDIR}/sandbox-router.yml > ${REMOTE_WORKDIR}/sandbox-router-rendered.yml
sed \
  -e \"s|\\\${SANDBOX_TEMPLATE_NAME}|${AGENTSCOPE_AGENT_SANDBOX_TEMPLATE}|g\" \
  -e \"s|\\\${SANDBOX_NAMESPACE}|${AGENTSCOPE_AGENT_SANDBOX_NAMESPACE}|g\" \
  -e \"s|\\\${RUNTIME_IMAGE}|${AGENT_SANDBOX_RUNTIME_IMAGE}|g\" \
  ${REMOTE_WORKDIR}/python-sandbox-template.yml > ${REMOTE_WORKDIR}/python-sandbox-template-rendered.yml

docker cp ${REMOTE_WORKDIR}/sandbox-router-rendered.yml ${K3S_CONTAINER}:${REMOTE_WORKDIR}/sandbox-router-rendered.yml
docker cp ${REMOTE_WORKDIR}/python-sandbox-template-rendered.yml ${K3S_CONTAINER}:${REMOTE_WORKDIR}/python-sandbox-template-rendered.yml
docker exec ${K3S_CONTAINER} kubectl -n ${AGENTSCOPE_AGENT_SANDBOX_NAMESPACE} apply -f ${REMOTE_WORKDIR}/sandbox-router-rendered.yml
docker exec ${K3S_CONTAINER} kubectl apply -f ${REMOTE_WORKDIR}/python-sandbox-template-rendered.yml
docker exec ${K3S_CONTAINER} kubectl -n ${AGENTSCOPE_AGENT_SANDBOX_NAMESPACE} rollout status deploy/sandbox-router-deployment --timeout=180s
docker exec ${K3S_CONTAINER} kubectl -n ${AGENTSCOPE_AGENT_SANDBOX_NAMESPACE} get deploy,pods,svc,sandboxtemplate -o wide
"
```

router 可用时，`sandbox-router-deployment` 应显示 `2/2`，`sandbox-router-svc` 应显示 `8080/TCP`。

### 配置本地 kubeconfig 和模型密钥

本地运行示例需要 kubeconfig 创建 `SandboxClaim`。k3s kubeconfig 默认指向容器内 `127.0.0.1:6443`，因此需要本地 SSH 隧道。

```bash
export AGENTSCOPE_K3S_KUBECONFIG=/tmp/agentscope-k3s-kubeconfig

ssh "${REMOTE}" "docker exec ${K3S_CONTAINER} cat /etc/rancher/k3s/k3s.yaml" > "${AGENTSCOPE_K3S_KUBECONFIG}"
chmod 600 "${AGENTSCOPE_K3S_KUBECONFIG}"
KUBECONFIG="${AGENTSCOPE_K3S_KUBECONFIG}" kubectl config set-cluster default --server=https://127.0.0.1:16443

ssh -N -L 16443:127.0.0.1:${K3S_API_HOST_PORT} "${REMOTE}"
```

在另一个终端验证隧道：

```bash
KUBECONFIG="${AGENTSCOPE_K3S_KUBECONFIG}" kubectl get --raw=/readyz
KUBECONFIG="${AGENTSCOPE_K3S_KUBECONFIG}" kubectl -n agent get deploy,pods,svc,sandboxtemplate -o wide
```

设置 DashScope API Key 后，示例会走真实 ChatModel 调用路径。模型名可选，未设置时使用代码中的默认值。

```bash
export AI_DASHSCOPE_API_KEY="<your DashScope API key>"
# 可选：export AI_DASHSCOPE_MODEL=qwen3.7-max
```

## 运行

```bash
cd example/workspace/agentsandbox
go run .
```

指定 template 或 namespace：

```bash
AGENTSCOPE_AGENT_SANDBOX_TEMPLATE=python-sandbox-template \
AGENTSCOPE_AGENT_SANDBOX_NAMESPACE=default \
go run .
```

使用 direct URL 模式：

```bash
AGENTSCOPE_AGENT_SANDBOX_API_URL=http://sandbox-router-svc.default.svc.cluster.local:8080 go run .
```

使用 Gateway 模式：

```bash
AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAME=kind-gateway \
AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAMESPACE=default \
go run .
```

运行真实 ChatModel 路径：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

在上述 Docker k3s 环境中运行：

```bash
cd example/workspace/agentsandbox
KUBECONFIG="${AGENTSCOPE_K3S_KUBECONFIG}" \
AGENTSCOPE_AGENT_SANDBOX_NAMESPACE=agent \
AGENTSCOPE_AGENT_SANDBOX_TEMPLATE=python-sandbox-template \
go run .
```

如果程序运行在集群内，或者本地已经把 router 端口转发到 `127.0.0.1:18080`，可以用 direct URL 模式连接 router。本地端口转发命令如下：

```bash
KUBECONFIG="${AGENTSCOPE_K3S_KUBECONFIG}" \
kubectl -n agent port-forward svc/sandbox-router-svc 18080:8080
```

在另一个终端运行示例：

```bash
KUBECONFIG="${AGENTSCOPE_K3S_KUBECONFIG}" \
AGENTSCOPE_AGENT_SANDBOX_NAMESPACE=agent \
AGENTSCOPE_AGENT_SANDBOX_TEMPLATE=python-sandbox-template \
AGENTSCOPE_AGENT_SANDBOX_API_URL=http://127.0.0.1:18080 \
go run .
```

## 预期输出

没有 API Key 时，输出包含：

```text
agent_sandbox_workspace_alive=true
read_has_brief=true
dashscope_live=skipped
```

有 API Key 时，输出还包含：

```text
dashscope_live=ok
```

本地通过远端 Docker k3s 集群实测时，输出示例：

```text
agent_sandbox_workspace_alive=true template=python-sandbox-template namespace=agent tools=Bash,Edit,Glob,Grep,Read,Write write=success read_has_brief=true chat_model=dashscope:qwen3.7-max estimated_tokens=56
dashscope_live=ok response="Random float check: ..."
```

示例结束时会调用 `Close` 删除 `SandboxClaim`。sandbox pod 可能短暂显示 `Terminating`，稍后会被 Kubernetes 清理。

## 排错

- `connection refused`：先确认 SSH 隧道仍在运行，再执行 `KUBECONFIG="${AGENTSCOPE_K3S_KUBECONFIG}" kubectl get --raw=/readyz`。
- `SandboxTemplate` 不存在：执行 `kubectl -n agent get sandboxtemplate python-sandbox-template`，确认 namespace 和 template 名称与环境变量一致。
- sandbox pod `ImagePullBackOff`：确认 runtime 镜像已导入 k3s containerd，并且 template 中的镜像名与 `ctr --namespace k8s.io images list` 输出一致。
- router pod 未就绪：执行 `kubectl -n agent describe pod -l app=sandbox-router`，重点检查镜像、`/healthz` 探针和端口 `8080`。
- `dashscope_live=skipped`：本地未设置 `AI_DASHSCOPE_API_KEY`。设置后重新运行示例即可。
