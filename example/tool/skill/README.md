# Skill Loader Example

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how the `tool/skill` package loads local `SKILL.md` files:

- Read two example skill directories from `resources/`.
- Each `SKILL.md` declares `name` and `description` in YAML front matter.
- Use `NewLocalLoader(..., WithScanSubdirs(true))` to scan subdirectories.
- Read skill names, descriptions, and Markdown bodies.

## Prerequisites

- Go 1.26.3.
- No API key is required.

## Run

```bash
cd example/tool/skill
go run .
```

## Expected Output

Output includes:

```text
skills=2
```
