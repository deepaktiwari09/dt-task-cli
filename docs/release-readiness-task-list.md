# v0.1.0 release-readiness task list

This checklist tracks the review findings for the local implementation. Items are checked only after code and tests verify them.

## Safety and data integrity

- [x] Make task editor changes transactional; reject invalid edits without touching the original.
- [x] Persist the global task counter before writing a newly allocated task; leave safe gaps on task-write failure.
- [x] Make skill installation rollback restore the target currently being replaced, including second-target failures.
- [x] Add doctor backups, duplicate-ID detection, dependency-cycle checks, orphan checks, and schema-version handling.
- [x] Add recoverable trash purge and archive/restore tests.

## Workflow and interfaces

- [x] Enforce legal status transitions and implement blocked-task resume.
- [x] Complete day start/day end prompts, notes, blockers, tomorrow planning, timer recovery, and carryover warnings.
- [x] Complete analytics: completion rate, estimates, stale/blocked metrics, breakdowns, custom/all-time ranges.
- [x] Make `skill --status` read-only and nonzero for missing/stale/divergent installations.
- [x] Honor TTY-only prompts, JSON errors, and exit codes 0/1/2.
- [x] Add `config edit`, project-alias synchronization, and timezone handling.

## Quality and release

- [x] Add dev-only structured logs with sensitive-data filtering.
- [x] Add unit, integration, golden-envelope, rollback, migration-rejection, concurrency, benchmark, and cross-platform checks.
- [x] Extend CI with cross-platform builds, completion smoke tests, and performance checks.
- [x] Validate release metadata and document remote GitHub/Homebrew steps.
