# CXWorkflow state store

This directory is the file-backed single source of truth for a CXWorkflow team.

- `state.json` — task state machine (`Planned -> Assigned -> Implementing -> ReadyForTest -> Testing -> Fixing -> Accepted -> Reported`).
- `events.log` — append-only JSONL event log (`TaskCreated`, `TaskFinished`, `TestFailed`, `TestPassed`, `Blocked`, `MilestoneReached`, `RateLimitWarning`, `ProgressReport`, `RecoverySuggestion`).
- `decisions.md` — Commander decisions, appended.
- `briefs/` — Secretary briefs forwarded to Commander.

All roles read state from these files instead of asking other sessions. Manage the
store with `python3 scripts/cxwf.py` (see `--help`). Transient files are git-ignored.
