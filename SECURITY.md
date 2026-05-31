# Security Policy

Chinese version: [`SECURITY_zh.md`](SECURITY_zh.md).

AgentScope Go is a library framework for building AI agent applications. It is
not a hosted service. Application owners decide which model providers, tools,
workspaces, MCP servers, and credentials are available at runtime.

## Supported Versions

Security fixes are considered for:

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| `main` branch | Best effort |
| Older releases | No, unless explicitly announced |

## Reporting a Vulnerability

Please do not open a public GitHub issue for a suspected vulnerability.

Report security issues by email:

- Email: `yuluo08290126@gmail.com`
- Subject: `AgentScope Go security report`

Include as much detail as possible:

- Affected package, commit, tag, or module version.
- Operating system and Go version.
- Model provider, tool, workspace, or MCP configuration involved.
- Minimal reproduction steps or proof of concept.
- Expected impact and whether credentials, local files, or tool execution are
  involved.

The maintainer will try to acknowledge reports within 7 days and provide a
status update within 30 days. Coordinated disclosure timing will be discussed
with the reporter when a fix is available.

## Security Boundaries

AgentScope Go provides building blocks. It does not make an application safe by
default.

- Model API keys and other credentials must be supplied by the application and
  must not be committed to the repository.
- Tool execution is controlled by the application that registers tools in an
  agent or toolkit.
- Builtin shell and filesystem tools can affect the local machine when granted
  permission by the application.
- Permission checks and shell parsing are safety controls, not a complete
  operating-system sandbox.
- `LocalWorkspace` stores files on the local filesystem. Docker, E2B, and other
  remote sandbox backends are not currently implemented.
- MCP servers are external trust boundaries. Use trusted server binaries and
  review their configuration before connecting them to an agent.
- Messages, tool results, logs, and workspace files may contain sensitive data.
  The application owner is responsible for storage, redaction, and retention.

## Issues That Should Be Reported Privately

Please use the private reporting channel for issues such as:

- Tool permission bypasses.
- Command execution outside the configured permission model.
- Path traversal or unintended file access through workspace or builtin tools.
- Secret leakage in logs, examples, tool outputs, or generated files.
- MCP integration behavior that allows an untrusted server to access more than
  the configured tool surface.
- Dependency or supply-chain issues with a practical exploit path in this
  project.

## Non-Security Issues

Open a normal GitHub issue for:

- Incorrect model output.
- Prompt quality problems.
- Documentation mistakes.
- Missing examples.
- Feature requests for new providers, tools, or workspace backends.
- Bugs where the caller intentionally granted the tool or file access being used.

When in doubt, report privately first.
