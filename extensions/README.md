# AgentScope Go 扩展模块

`extensions/` 用于放置不应进入根模块运行时依赖图的可选集成。

根模块已经提供 RAG 核心契约、`KnowledgeBase`、文本 Parser、Chunker、RAG
middleware，以及一个仅依赖标准库的 `rag.MemoryVectorStore`。外部向量数据库客户端
依赖放在这里，以独立 Go module 的形式维护，应用按需导入。

## Vector Store

当前已有三个可用后端：

| 后端 | 位置 | 说明 |
| --- | --- | --- |
| 内存 | `pkg/rag.NewMemoryVectorStore` | 根模块自带，适合测试、本地开发和小规模临时知识库 |
| Qdrant | `extensions/vectorstore/qdrant` | 独立 Go module，依赖官方 `github.com/qdrant/go-client` |
| Milvus | `extensions/vectorstore/milvus` | 独立 Go module，依赖官方 `github.com/milvus-io/milvus/client/v2` |

### 内存后端

```go
store := rag.NewMemoryVectorStore()
```

内存后端实现了 `rag.VectorStore`，支持 collection 创建/删除、插入、按 document id
删除、metadata 精确过滤、余弦相似度搜索和文档列表聚合。它不会持久化数据，进程退出
后内容会丢失。

### Qdrant

模块路径：

```text
github.com/yuluo-yx/agentscope-go/extensions/vectorstore/qdrant
```

本地服务 compose 文件：

```bash
cd /Users/shown/workspace/docker-compose/qdrant
docker compose up -d
```

默认 gRPC 地址是 `127.0.0.1:6334`。在本机 Docker 环境中，`localhost:6334` 可能因为
gRPC/IPv6 解析路径超时；扩展的 `Connect` 会把空 host 和 `localhost` 规整为
`127.0.0.1`。

```go
store, err := qdrant.Connect(qdrant.Config{
	Host: "127.0.0.1",
	Port: 6334,
})
if err != nil {
	panic(err)
}
defer store.Close()
```

运行集成测试：

```bash
cd extensions/vectorstore/qdrant
AGENTSCOPE_QDRANT_ADDR=127.0.0.1:6334 go test ./... -run TestStoreIntegration -count=1
```

### Milvus

模块路径：

```text
github.com/yuluo-yx/agentscope-go/extensions/vectorstore/milvus
```

本地服务 compose 文件使用现有目录：

```bash
cd /Users/shown/workspace/docker-compose/milvus
docker compose up -d
```

默认 gRPC 地址是 `localhost:19530`。

```go
store, err := milvus.Connect(ctx, milvus.Config{
	Address: "localhost:19530",
})
if err != nil {
	panic(err)
}
defer store.Close(context.Background())
```

运行集成测试：

```bash
cd extensions/vectorstore/milvus
AGENTSCOPE_MILVUS_ADDR=localhost:19530 go test ./... -run TestStoreIntegration -count=1
```

## 新增后端约定

新增向量数据库后端时，建议保持以下目录结构：

```text
extensions/vectorstore/<backend>/
  go.mod
  store.go
  store_test.go
```

每个后端模块应依赖 `github.com/yuluo-yx/agentscope-go/pkg/rag` 并实现
`rag.VectorStore`，不要把数据库 SDK 加入根模块依赖图。
