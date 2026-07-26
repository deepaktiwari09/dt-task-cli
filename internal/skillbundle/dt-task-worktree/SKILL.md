---
name: dt-task-worktree
description: Coordinate parallel development with dt-task Git worktrees and Warp/Codex terminals. Use when creating, inspecting, operating, or cleaning up isolated worktrees for independent agent tasks.
---

# dt-task worktree workflow

Use this skill when independent coding tasks need separate checkouts and branches.

## Configure once

Run from the registered project checkout:

```sh
dt-task init --alias <project>
dt-task project config set worktree_default_branch <base-branch>
dt-task project config set worktree_branch_prefix deepak/codex
```

Optionally configure setup for each new checkout:

```sh
dt-task project config set worktree_setup_command "pnpm install"
```

## Start parallel work

1. Create one worktree per independent task: `dt-task worktree create <slug>`.
2. Open the printed command in a new Warp tab and run `codex`.
3. Use the printed `codex exec` command for bounded unattended work.
4. Create the next worktree from the base checkout, not from another worktree.
5. Keep task metadata explicit: `dt-task --project <project> task status <id> in-progress`.

Warp tabs are the terminal boundary. Do not use tmux, switch branches inside an active worktree, or run multiple agents in one checkout.

## Inspect and clean up

```sh
dt-task worktree list
dt-task worktree path <slug>
dt-task worktree remove <slug>
```

Removal preserves the branch and refuses dirty or untracked work by default. Review, commit, or recover changes before using `--force`.

Worktree commands never commit, merge, or change task status automatically. Use `--base`, `--branch`, and `--no-setup` only when the project configuration needs an explicit override.
