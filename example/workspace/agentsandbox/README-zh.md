# Agent With AgentSandbox Run

```shell
# 将 kubeConfig 放在本机，验证连接成功

kubectl --kubeconfig=/tmp/agentscope-k3s-kubeconfig.t533Oo get --raw=/readyz --insecure-skip-tls-verify=true

# 确认资源
kubectl --kubeconfig=/tmp/agentscope-k3s-kubeconfig.t533Oo -n agent get deploy,pods,svc,sandboxtemplate -o wide --insecure-skip-tls-verify=true

# 启动 example
KUBECONFIG=/tmp/agentscope-k3s-kubeconfig.t533Oo go run .
```

## 连接问题

- tls: failed to verify certificate: x509：使用 --tls-san 启动，携带公网 ip，或者在 kubeconfig 中添加 ``。
