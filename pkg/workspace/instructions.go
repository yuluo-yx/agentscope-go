// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workspace

import "strings"

// DefaultWorkspaceInstructions is the standard workspace system-prompt
// fragment shared by every workspace backend. It supports two placeholders:
// "{backend}" for the backend label (for example "local" or "Docker-based")
// and "{workdir}" for the workspace root visible to the agent.
const DefaultWorkspaceInstructions = `<workspace>You have access to a {backend} workspace at {workdir} with the following structure:

` + "```" + `
{workdir}
├── data/        # offloaded multimodal files (images, etc.) — system-managed
├── skills/      # reusable skills, each in its own subdirectory
└── sessions/    # offloaded session context and tool results — system-managed
` + "```" + `

This workspace is your personal working environment. You are responsible for keeping it clean, structured, and easy to navigate over time.

### Project Directory
- Create a dedicated subdirectory for each task or project under the workspace root.
- Name each project subdirectory concisely and descriptively, prefixed with its absolute creation date, e.g. ` + "`20240315_web-scraper`" + `, so it stays identifiable long after creation.
- Always create a ` + "`README.md`" + ` at the project root documenting:
  - What the project is about
  - Its absolute creation date
  - Key decisions or context that would help you resume work later

### Working Across Sessions
- The same project may be worked on from more than one session at a time. There is no live lock that tells you another session is editing a file — avoid conflicts by isolation, not by hoping:
  - Prefer ` + "`git worktree`" + ` with a session-specific name so parallel work happens on separate trees and never shares the same files.
  - Encode ownership in names (creation date, session identifier) so it is clear which session created what.
- Be conservative about deletion: do not delete anything you did not create in the current session, prefer archiving over deleting, and rely on git so any change can be rolled back. Confirm before destructive cleanup.

### Scratch / Temporary Files
- Put one-off experiments, intermediate data, and anything you would otherwise drop in ` + "`/tmp`" + ` under a ` + "`scratch/`" + ` directory (created on first use), not inside project directories — this keeps projects and their git history clean.
- Treat ` + "`scratch/`" + ` as disposable: exclude it from git, and assume nothing in it is guaranteed to persist. Nothing clears it automatically (it lives inside your persistent workspace, not the OS temp dir), so delete your own scratch files when you are done with them.

### Version Control
- Prefer initializing a ` + "`git`" + ` repository in each project directory to track changes and allow rollbacks.
- If you use git, create a ` + "`.gitignore`" + ` before the first commit to exclude unwanted files (e.g. virtual environments, cache, ` + "`scratch/`" + `, secrets).
- Never hard-code secrets into project files or commit them — this is a personal environment, but treat credentials as if they could leak.

### Python Environment
- ` + "`uv`" + ` is recommended for managing and isolating Python environments per project:
` + "```shell" + `
uv venv && uv pip install ...
- Never install packages into a shared or global environment — each project must manage its own dependencies to avoid conflicts.</workspace>`

// RenderInstructions resolves the "{backend}" and "{workdir}" placeholders
// in a workspace instruction template. An empty backend removes the
// placeholder word; an empty workdir renders as "<unknown>" so the fragment
// stays readable before initialization.
func RenderInstructions(template, backend, workdir string) string {
	if workdir == "" {
		workdir = "<unknown>"
	}
	rendered := strings.ReplaceAll(template, "{backend}", backend)
	rendered = strings.ReplaceAll(rendered, "{workdir}", workdir)
	return rendered
}
