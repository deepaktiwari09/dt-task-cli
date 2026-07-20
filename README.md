# dt-task

`dt-task` is a small, offline-first CLI for turning product ideas into actionable PRD tasks and a realistic daily plan.

It keeps work readable and private:

- PRD tasks are Markdown files in the project’s `.task/` directory.
- Personal project registration, journals, timers, and analytics live in `~/.dt-task/`.
- No daemon, cloud account, telemetry, or runtime network calls.

## What it helps with

`dt-task` gives a developer one short workflow for:

- capturing an idea before it is forgotten;
- refining it into a PRD with an outcome and acceptance criteria;
- seeing remaining, blocked, due, and draft work;
- planning today and carrying unfinished work into tomorrow;
- recording focus time and estimate accuracy;
- reviewing progress across multiple projects;
- giving coding agents a safe, consistent task workflow.

Tasks use stable names such as:

~~~text
.task/0001.[backlog].implement-login.md
~~~

The task number is global and never reused. Status changes update the filename and the YAML metadata inside the Markdown file.

## Install

### Release archive (recommended)

Open the [latest release](https://github.com/deepaktiwari09/dt-task-cli/releases/latest), download the archive matching your OS and CPU, then put the binary on `PATH`.

macOS/Linux example:

~~~sh
tar -xzf dt-task_0.1.0_darwin_arm64.tar.gz
mkdir -p ~/.local/bin
install -m 0755 dt-task ~/.local/bin/dt-task
dt-task version
~~~

Use `darwin_amd64` for Intel macOS, `linux_amd64` or `linux_arm64` for Linux, and the matching Windows `.zip` archive for Windows. Add `~/.local/bin` to `PATH` if it is not already there.

Always verify the downloaded archive with `checksums.txt` when scripting an installation.

### Homebrew (macOS)

Once the project tap is published, install the cask with:

~~~sh
brew tap deepaktiwari09/tap
brew install --cask dt-task
dt-task version
~~~

The current v0.1.0 release does not publish the tap yet. Until then, use the release archive above.

### Build from source

Requires Go 1.26 or newer:

~~~sh
go install github.com/deepaktiwari09/dt-task-cli/cmd/dt-task@latest
dt-task version
~~~

## Start using it

Run this in any project:

~~~sh
dt-task init --alias my-project
~~~

`init` creates `.task/`, repairs missing task structure on reruns, creates or updates `.gitignore` with exactly `/.task/`, and registers the project locally. The `.task/` directory is intentionally ignored so each developer keeps their own task state after cloning the same repository.

Capture a quick idea:

~~~sh
dt-task capture "Fix login redirect"
~~~

Create a complete PRD task non-interactively:

~~~sh
dt-task task create --title "Implement login" --problem "Users lose their original destination" --outcome "Users return to the page they requested" --acceptance "Redirect preserves the original path" --estimate 90 --priority P1 --tags auth,frontend
~~~

Interactive prompts are used only when a terminal is available. Scripts and CI must provide the required flags.

## Daily workflow

Start the day. The plan shows carried work, planned work, drafts, blockers, due items, estimates, and capacity warnings:

~~~sh
dt-task day start
dt-task task list --status backlog
~~~

Move a task into work and record focus time:

~~~sh
dt-task task status 1 planned
dt-task task start 1
# work
dt-task task stop 1
dt-task task status 1 done
~~~

If work is blocked, keep the reason with the task:

~~~sh
dt-task task status 1 blocked --blocker "Waiting for API contract"
dt-task task status 1 resume
~~~

Close the day. Unfinished work is carried to the next day automatically:

~~~sh
dt-task day end --note "API contract still pending"
~~~

Review results:

~~~sh
dt-task analytics --days 7
dt-task analytics --all-time
~~~

## Common task operations

~~~sh
dt-task task list                         # remaining tasks in this project
dt-task task list --all-projects          # aggregate registered projects
dt-task task show 1                       # read one PRD
dt-task task edit 1                       # edit with $EDITOR and validate
dt-task task depend add 1 2               # task 1 depends on task 2
dt-task task archive 1                    # keep it searchable, hide from active work
dt-task task delete 1                     # move it to project trash
dt-task task restore 1                    # restore from trash
dt-task doctor                            # check storage health
dt-task doctor --fix                      # backup, then repair safe issues
~~~

Use `--project <alias>` when working outside a registered project directory:

~~~sh
dt-task --project my-project task list
~~~

Use `--json` for scripts. JSON data goes to stdout; diagnostics go to stderr. Exit codes are `0` for success, `1` for state/I/O failures, and `2` for usage or validation errors.

## Global agent skill

The bundled `dt-task` skill teaches Codex and other compatible agents to inspect remaining work, refine PRDs, update status/dependencies, run timers, plan days, and use JSON output safely.

Install or update it globally:

~~~sh
dt-task skill
~~~

Check both supported installation targets without changing files:

~~~sh
dt-task skill --status
~~~

The command installs matching copies under `CODEX_HOME/skills/dt-task` (or `~/.codex/skills/dt-task` when `CODEX_HOME` is unset) and `~/.agents/skills/dt-task`. Existing copies are backed up before replacement. No agent content or project paths are sent over the network.

## Update dt-task

Homebrew users:

~~~sh
brew update
brew upgrade --cask dt-task
~~~

Archive users should download the newest archive from the [latest release](https://github.com/deepaktiwari09/dt-task-cli/releases/latest), replace the existing binary, and verify:

~~~sh
dt-task version
~~~

Go users can update to the newest source release with:

~~~sh
go install github.com/deepaktiwari09/dt-task-cli/cmd/dt-task@latest
~~~

## Shell completion

~~~sh
# zsh
dt-task completion zsh > "${fpath[1]}/_dt-task"

# bash
dt-task completion bash > ~/.local/share/bash-completion/completions/dt-task
~~~

Restart the shell after installing completion.

## Data, safety, and documentation

Task files remain readable Markdown. Project `.task/` data is ignored by Git; global journals and registries stay in the user’s home directory. Mutations use locks, validation, backups where needed, temporary files, and atomic renames.

Run `dt-task doctor` after moving or restoring task data. See [business logic](docs/business-logic.md), the [release checklist](docs/release.md), and the [release-readiness task list](docs/release-readiness-task-list.md) for deeper storage, workflow, QA, and release details.

## Command index

~~~text
dt-task init
dt-task capture
dt-task task create|list|show|edit|status|depend|start|stop|archive|delete|restore|purge
dt-task day start|status|end
dt-task analytics
dt-task project list|rename|remove
dt-task doctor [--fix]
dt-task config get|set|edit
dt-task skill [--status]
dt-task completion bash|zsh
dt-task version
~~~
