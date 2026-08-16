---
name: parallel-dev-workflow-templates
description: |
  Templates for parallel development workflow.
  Includes Commander dashboard, progress bars, quality gates,
  CHANGELOG, review checklists, and schema definitions.
parent: parallel-dev-workflow
---
# Templates

## Commander Dashboard

```
╔════════════════════════════════════════════════════════════╗
║           PARALLEL DEV WORKFLOW - COMMANDER                ║
╠════════════════════════════════════════════════════════════╣
║ Task: checkout-feature                                     ║
║ Wave: 2/3 | Elapsed: 5min 32s | Est. remaining: 3min      ║
╠════════════════════════════════════════════════════════════╣
║ BUILDERS                                                   ║
║   [✓] auth/          45s   Sonnet   0 issues               ║
║   [✓] payment/       38s   Sonnet   1 issue (fixed)        ║
║   [⟳] checkout/      1:20  Sonnet   building...            ║
║   [ ] order/         —     Haiku    waiting                ║
╠════════════════════════════════════════════════════════════╣
║ TESTERS                                                    ║
║   [✓] unit           12 tests passed                       ║
║   [⟳] integration    running...                             ║
║   [ ] e2e            waiting                                ║
╠════════════════════════════════════════════════════════════╣
║ REVIEWS                                                    ║
║   [✓] security       clean (Opus)                          ║
║   [!] quality        2 issues found (Sonnet)               ║
║   [ ] performance    waiting                                ║
╠════════════════════════════════════════════════════════════╣
║ COVERAGE: 78% (target: 80%)                                ║
║ ISSUES: 2 open, 3 fixed                                    ║
║ HEALTH: all agents healthy                                 ║
╚════════════════════════════════════════════════════════════╝
```

## Progress Bar

```
Progress: ████████░░░░░░░░ 50% (3/6 modules done)
```

## Quality Gate Template

```
Wave N Quality Gate:
━━━━━━━━━━━━━━━━━━━
[✓] Tests pass (X/Y)
[✓] Build succeeds
[✓] Coverage ≥ 80% (actual: XX%)
[✓] No critical security issues
→ GATE PASSED/PASSED WITH WARNINGS/FAILED
```

## CHANGELOG Template

```markdown
## CHANGELOG

### Features
- feat(module1): description
- feat(module2): description

### Bug Fixes
- fix(module1): description

### Tests
- X tests added, all passing
- Coverage: XX% (target met/not met)

### Breaking Changes
- module: change description

### Migration Guide
- Step 1: ...
- Step 2: ...
```

## Review Checklist Template

```markdown
## Review: module/file.ts

### Security
- [ ] No hardcoded secrets
- [ ] Input validation present
- [ ] Error messages don't leak info

### Performance
- [ ] No N+1 queries
- [ ] Caching used appropriately
- [ ] No unnecessary computations

### Code Quality
- [ ] Functions < 50 lines
- [ ] No deep nesting (> 4 levels)
- [ ] Error handling explicit

### Tests
- [ ] Unit tests present
- [ ] Edge cases covered
- [ ] Coverage > 80%
```

## Learning Schema

```json
{
  "task": "feature-name",
  "date": "2026-05-29",
  "modules": ["module1", "module2"],
  "complexity": {
    "module1": "HIGH",
    "module2": "LOW"
  },
  "waves": 3,
  "critical_path": ["module1", "module2"],
  "estimated_time": "15min",
  "actual_time": "12min",
  "issues_found": 3,
  "lessons": ["lesson1", "lesson2"],
  "coverage": {
    "target": 80,
    "actual": 82
  }
}
```

## Knowledge Base Schema

```json
{
  "pattern_name": {
    "description": "Pattern description",
    "files": ["path/to/file1", "path/to/file2"],
    "reuse_count": 3,
    "last_used": "2026-05-29",
    "quality_score": 9
  }
}
```

## Progress Persistence Schema

```json
{
  "task": "feature-name",
  "started": "2026-05-29T10:00:00Z",
  "wave": 2,
  "modules": {
    "module1": { "status": "done", "complexity": "HIGH" },
    "module2": { "status": "in_progress", "complexity": "LOW" }
  },
  "critical_path": ["module1", "module2"],
  "coverage": { "target": 80, "current": 78 },
  "checkpoints": ["wave-1-sha"]
}
```

## Agent Health Template

```
Agent Health:
  Builder A (auth):      [HEALTHY] 45s, 12 files touched
  Builder B (payment):   [HEALTHY] 38s, 8 files touched
  Builder C (users):     [STUCK] 2min no output
    → Sending heartbeat...
    → Restarting agent...
    → [HEALTHY] restarted successfully
```

## Dependency Graph Template

```
Module Dependencies:
━━━━━━━━━━━━━━━━━━━

auth ──────────┐
               ├→ checkout ──→ order
payment ───────┘

Critical Path (longest): auth → checkout → order
  auth: ~3min
  checkout: ~2min
  order: ~1min
  Total: ~6min
```

## File Partition Template

```
File Partition Map:
━━━━━━━━━━━━━━━━━━

Builder A (auth/):
  - src/auth/middleware.ts
  - src/auth/routes.ts
  - src/auth/types.ts

Builder B (payment/):
  - src/payment/stripe.ts
  - src/payment/webhook.ts
  - src/payment/types.ts

Shared (read-only during parallel):
  - src/types/shared.ts
  - src/utils/*
```

## Rollback Report Template

```
Rollback Report:
━━━━━━━━━━━━━━━━

Rolled back: Wave 2
Reason: Test failures in checkout/

Before rollback:
  [✓] Wave 1: auth, payment, users
  [✗] Wave 2: checkout (failed)

After rollback:
  [✓] Wave 1: auth, payment, users (verified)
  [ ] Wave 2: checkout (re-running)

Action: Re-running Wave 2 with fix...
```

## Abort Report Template

```
Abort Report:
━━━━━━━━━━━━━

Completed:
  [✓] auth/ (committed)
  [✓] payment/ (committed)

Abandoned:
  [~] checkout/ (60% complete)

Not started:
  [ ] order/

Progress saved. Resume with: "继续上次的并行开发"
```
