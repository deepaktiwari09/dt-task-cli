# dt-task

Local-first PRD and daily work manager for developers.

## Install

Download the archive for your OS and CPU from the
[v0.1.0 release](https://github.com/deepaktiwari09/dt-task-cli/releases/tag/v0.1.0),
extract `dt-task` (or `dt-task.exe` on Windows), and place it on `PATH`.

```sh
tar -xzf dt-task_0.1.0_linux_arm64.tar.gz
install -m 0755 dt-task ~/.local/bin/dt-task
dt-task version
```

Windows users should extract the `.zip` archive and add its directory to `PATH`.

## Quick start

```sh
dt-task init --alias my-project
dt-task capture "Fix login redirect"
dt-task task create --title "Implement login" --problem "Users lose context" --outcome "Users return to the requested page" --acceptance "Redirect preserves the original path" --estimate 90
dt-task day start
dt-task task start 1
dt-task task stop 1
dt-task day end
```

Task files are readable Markdown in `.task/`; initialization adds `/.task/` to `.gitignore`. Personal registry and daily journals live in `~/.dt-task/`.

Use `dt-task skill` to install the bundled agent workflow globally. Use `dt-task skill --status` to verify both supported skill locations.

## Commands

```text
dt-task init [directory] [--alias name]
dt-task capture <title>
dt-task task create|list|show|edit|status|depend|start|stop|archive|delete|restore|purge
dt-task day start|status|end
dt-task analytics [--days N|--from DATE --to DATE|--all-time]
dt-task project list|rename|remove
dt-task doctor [--fix]
dt-task config get|set|edit
dt-task skill [--status]
```

Most commands support `--json` for scripts. `--project alias` selects a registered project outside its directory.

`task stop --continue` keeps an interrupted timer running; `--adjust --minutes N`
records a corrected duration, and `--discard` removes it without recording time.
`task purge ID --yes` is the only permanent delete operation.

Exit status is `0` for success, `1` for state/IO failures, and `2` for usage or
validation errors. Data is written to stdout; diagnostics go to stderr. Set
`DT_TASK_ENV=development` to enable structured JSON diagnostics, optionally with
`DT_TASK_LOG_LEVEL=debug|info|warn|error`; logs never include PRD bodies or paths.

## Design

See [docs/business-logic.md](docs/business-logic.md) for storage relationships, daily flow, status rules, and QA scenarios.
