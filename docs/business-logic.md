# dt-task business logic

## Storage relationships

- One project registration has one project configuration (1–1).
- One project has many task PRDs (1–N). Example: `web` owns task `0001` and task `0002`.
- One task has many work sessions (1–N). A 90-minute task may have three focus sessions.
- Tasks depend on many other tasks and can be depended on by many tasks (N–N). Example: API task `0002` blocks UI task `0003`.
- A day journal plans many tasks, and a task can appear on many day journals (N–N).

Project task files are private and ignored by Git. Global registry and day journals are private to the local OS user.

## Task lifecycle

1. `capture` creates a backlog draft for immediate capture.
2. `task create` creates a complete PRD with problem, outcome, acceptance criteria, priority, and estimate.
3. `day start` plans work across all registered projects.
4. `task start` moves backlog/planned work to in-progress and starts the only global timer.
5. `task status ... blocked --blocker` records an external or dependency blocker.
6. `task status ... done` clears remaining minutes and records completion.
7. `day end` records outcomes and carries unfinished work to tomorrow as planned.
8. Three carryovers produce a warning; the task remains recoverable and visible.

Done work is manually archived. Deleted work is moved to trash and can be restored.

## Daily example

Alice starts the day in a frontend directory. `day start` combines frontend and backend tasks, warns that 420 minutes exceed her 360-minute capacity, and she removes one low-priority task. She starts task `0004`, stops after 55 minutes, marks it done, and ends the day. The unfinished task `0007` is placed in tomorrow’s journal. The next morning, the carryover is visible with its history.

Bob clones the same repository on another machine. His `.task/` and `~/.dt-task/` are separate, so Alice’s private notes and timer never sync accidentally.

## A-to-Z multi-project flow

Alice initializes `web` and `api` on one workstation. Both registrations point
to one global registry, while each project keeps its own ignored `.task/tasks`
files (1–N). `day start` joins planned work from both projects into one local
journal. She starts API task `0002`, records a session, then switches to web
task `0007` only after stopping the global timer. At `day end`, completed IDs,
focus sessions, notes, blockers, estimate variance, and tomorrow references are
written once. The next day carries those references forward without copying PRD
text. Renaming `api` updates its 1–1 project config and all journal references.

A second developer cloning the same Git repository registers their own local
alias. Git contains source code only: `.task/`, global journals, timers, and
skill backups remain user-scoped and are never merged between developers.

## QA scenarios

- Run `init` twice: exactly one `/.task/` line remains and existing files are unchanged.
- Create, rename by status, show, edit, archive, delete, and restore a task.
- Start a second timer while one is active: command fails and identifies the active task.
- End a day with unfinished work: next day contains planned carryovers.
- Add a dependency cycle: command rejects it.
- Run `doctor --fix` after a manual filename mismatch: repair is deterministic.
- Run `skill` with missing, current, stale, and customized global targets: backups and checksums are correct.
- Run `skill --status` before installation: it performs no writes and exits non-zero while either target is missing.
- Run `day start` after an overnight timer: continue, adjust, or discard interactively; non-TTY output stays actionable.
