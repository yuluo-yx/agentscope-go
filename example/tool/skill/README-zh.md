# Skill 加载示例

英文文档：[README.md](README.md)。

本示例展示 `tool/skill` 包如何加载本地 `SKILL.md`：

- 从 `resources/` 读取两个示例 skill 目录。
- 每个 `SKILL.md` 使用 YAML front matter 声明 `name` 和 `description`。
- 使用 `NewLocalLoader(..., WithScanSubdirs(true))` 扫描子目录。
- 读取 skill 名称、描述和 Markdown 正文。

## 前置条件

- Go 1.26.3。
- 不需要 API Key。

## 运行

```bash
cd example/tool/skill
go run .
```

## 预期输出

输出包含：

```text
skills=2
```
