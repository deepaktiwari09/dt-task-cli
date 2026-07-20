---
name: dt-task
description: Operate the dt-task local PRD and daily planning workflow. Use when managing project tasks, writing or refining PRDs, preparing a workday, tracking focus sessions, reviewing carryover, or diagnosing dt-task state.
---

# dt-task workflow

Use `dt-task` as the source of truth for task metadata, status, estimates, daily plans, and work sessions.

## Start with context

1. Run `dt-task doctor` when project state may be inconsistent.
2. Use `dt-task task list --json` to inspect current work.
3. Use `dt-task day start` for cross-project planning.
4. Use `dt-task project list` when the current directory is not enough to identify a project.

## Create and refine work

- Use `dt-task capture "title"` for immediate inbox capture.
- Use `dt-task task create` for a complete PRD: problem/outcome, acceptance criteria, priority, and minutes.
- Keep acceptance criteria observable and QA-testable.
- Add dependencies with `dt-task task depend add <id> <dependency-id>`.
- Prefer CLI metadata edits and `dt-task task edit` over manually changing frontmatter.

## Daily execution

- Select work with `dt-task day start`; respect the displayed capacity warning.
- Start one focus session with `dt-task task start <id>` and stop it when switching.
- If a timer crossed midnight, use `dt-task task stop --continue`, `--adjust --minutes N`, or `--discard`.
- Mark progress immediately with `dt-task task status <id> in-progress|blocked|done`.
- Give blocked work a concrete reason.
- Run `dt-task day end` to record outcomes; unfinished work carries to tomorrow automatically.

## Safety

- Read before mutating and report affected task IDs.
- Do not expose PRD bodies, personal paths, credentials, or tokens in logs or summaries.
- Do not manually rename task files; status commands keep filename and metadata aligned.
- Run `dt-task doctor --fix` only after reviewing its proposed repairs.
