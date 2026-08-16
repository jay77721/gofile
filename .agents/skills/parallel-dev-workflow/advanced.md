---
name: parallel-dev-workflow-advanced
description: |
  Advanced optimizations for parallel development workflow.
  Includes adaptive waves, cache reuse, auto-fix, feature flags,
  progressive delivery, debug mode, visual reports, and more.
parent: parallel-dev-workflow
---
# Advanced Optimizations

## Adaptive Waves

Adjust next wave based on previous wave performance:

```
Wave 1 Results:
  auth/    actual: 45s (estimated 3min)  → simpler than expected
  payment/ actual: 2min (estimated 3min) → close to estimate

Wave 2 Adjustments:
  checkout/ estimate: 3min → 2min
  Model: Sonnet → Haiku (module simpler than expected)
```

**Rules:**
- actual < 50% estimated → simplify model
- actual > 150% estimated → upgrade model
- Wave finishes early → pull modules from next wave

## Cache Reuse

Reuse implementation patterns from similar modules:

```
Previous: auth/ with JWT + refresh token
Current: auth/ needs similar → reuse structure
```

**Cache sources:**
1. Cross-session learnings
2. Similar modules in current project
3. Knowledge base patterns
4. Open-source implementations

## Auto-Fix Common Issues

Detect and auto-fix on builder completion:

```
Detected:
  auth/:42 - console.log → AUTO-REMOVED
  payment/:89 - unused import → AUTO-REMOVED
  users/:15 - missing error handling → AUTO-ADDED try-catch
```

**Rules:**
- console.log → remove
- unused imports → remove
- missing error handling → add try-catch
- hardcoded values → extract to config
- complex nesting → suggest refactor (don't auto-change)

## Feature Flags

Auto-configure feature flags:

```typescript
export const FEATURE_CHECKOUT = process.env.FEATURE_CHECKOUT === 'true'

if (FEATURE_CHECKOUT) {
  // New flow
} else {
  // Legacy flow
}
```

**Benefits:** Gradual rollout, easy rollback, A/B testing ready.

## Progressive Delivery

Deploy modules independently:

```
Wave 1 complete → auth + payment deployable
Wave 2 complete → checkout deployable
Wave 3 complete → full feature live

Strategy:
  auth → canary (10%) → full
  checkout → feature flag → gradual
```

## Incremental Testing

Only run affected tests:

```
Changed: auth/, checkout/
Affected: auth.test.ts, checkout.test.ts, integration.test.ts
Skipped: users.test.ts (unaffected)

Test time: 45s (vs full suite 3min)
```

**How to determine:**
1. Parse import graph
2. Find test files importing changed modules
3. Find integration tests touching changed APIs

## Test Parallelization

Run unit tests in parallel:

```
Serial:  auth 2.0s + payment 1.5s + users 1.0s = 4.5s
Parallel: max(2.0, 1.5, 1.0) = 2.0s (2.25x speedup)
```

**Rules:**
- Unit tests → always parallel
- Integration tests → parallel if no shared state
- E2E tests → serial (shared browser)

## Builder Result Caching

Reuse if module unchanged:

```
Module: users/
Last implementation: 2026-05-28, hash: abc123
Current interface: no changes
→ Reusing cached implementation (0s)
→ Running tests to verify: ✓ passed
```

**Invalidation:** Interface changed, tests changed, dependencies changed.

## Code Metrics Tracking

Track quality per module:

```
Module      Complexity  Coverage  Duplicates
auth/       12          82%       0
payment/    18          75%       1
users/      5           88%       0
```

**Thresholds:** Complexity < 20, Coverage > 80%, Duplicates < 3.

## Debug Mode

Enable detailed logging for troubleshooting:

```
Debug Mode: ON
- Agent input/output: logged to ~/.claude/debug/
- Execution trace: full timeline
- Token usage: per agent tracking
- Errors: full stack traces
```

**Enable:** User says "debug mode" or "调试模式".

## Visual Reports

Generate HTML report after completion:

```markdown
## Report: checkout-feature
- Timeline: Wave 1 (2min) → Wave 2 (1.5min) → Wave 3 (1min)
- Coverage: 82% (target 80%)
- Issues found: 3 (all fixed)
- Agents used: 5 (3 Sonnet, 2 Haiku)
```

**Location:** `~/.claude/reports/parallel-dev/{task}.html`

## Multi-Project Parallel

Support cross-repo parallel development:

```
Project A: auth-service (backend)
Project B: auth-client (frontend)
Shared: interface definitions

Parallel: Build both repos simultaneously
Merge: Coordinate interface changes
```

**Use case:** Microservices, monorepo splits.

## Auto PR Creation

After successful merge, auto-create PR:

```bash
gh pr create --title "feat: checkout feature" --body "$(cat <<'EOF'
## Summary
- Added auth, payment, users, checkout, order modules
- 47 tests, 82% coverage

## Test plan
- [ ] All tests pass
- [ ] Coverage ≥ 80%
- [ ] No security issues
EOF
)"
```

**PR includes:** CHANGELOG, coverage report, test results.

## Module Priority Queue

Assign business priority:

```
P0 [CRITICAL] auth/       → Sonnet
P1 [HIGH]     payment/    → Sonnet
P2 [MEDIUM]   checkout/   → Sonnet
P3 [LOW]      order/      → Haiku
```

**Factors:** Business criticality, security sensitivity, user impact.

## Performance Baseline

Establish and compare:

```
Before: API p50=45ms, p95=120ms
After:  API p50=48ms, p95=125ms (+4%)

Verdict: NO REGRESSION (within 10% threshold)
```

**Rule:** Flag if > 10% regression.

## Log Aggregation

Unified view across agents:

```
[10:00:01] Builder(auth): Starting JWT
[10:00:02] Builder(payment): Starting Stripe
[10:00:15] Builder(auth): Created middleware.ts
[10:00:25] Reviewer(security): auth/ - 2 issues
```

**Benefits:** Single view, easy debugging, timeline visualization.

## Incremental Rollback

Rollback single module, not whole wave:

```
Wave 2: checkout/ (failed) + order/ (passed)
→ Rollback only checkout/
→ Re-run checkout/ builder
→ Continue with order/ already done
```

## Abort Strategy

When user says "stop" or "abort":

```
1. Stop all running agents
2. Save progress to .claude/parallel-dev-progress.json
3. Do NOT commit partial work
4. Report what was completed vs abandoned

Resume: "继续上次的并行开发"
```

## Auto Dependency Updates

Check before development:

```
Package        Current  Latest   Type
stripe         12.0.0   12.1.0   minor (safe)
jsonwebtoken   8.5.1    9.0.0    major (review)

Auto-update: 1 safe update
Flagged: 1 major update (manual review)
```

**Rules:** Patch/minor → auto-update. Major → flag for review.

## Documentation Sync

Check docs stay in sync:

```
Changed: src/auth/middleware.ts
  ⚠ README auth section may be outdated
  ✓ API docs auto-updated
  ✓ CHANGELOG updated

Action: Flag README sections for review
```
