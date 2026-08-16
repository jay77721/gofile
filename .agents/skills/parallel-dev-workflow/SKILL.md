---
name: parallel-dev-workflow
description: |
  Dynamic parallel development with Commander orchestration, smart model
  selection, wave checkpoints, parallel reviews, auto-retry, and
  cross-session learning.
  Trigger: "并行开发", "parallel dev", "快速开发", "多模块开发"
---

# Parallel Development Workflow

> **主 Claude 就是 Commander（总调度师）。收到任务 → 出计划 → 用户确认 → 动态执行 → 汇报 → 完成。**

## Commander 协议

**收到并行开发任务后，主 Claude 立即进入 Commander 模式。**

### 1. 出计划（第一时间）

分析任务后，输出完整计划等用户确认：

```
╔════════════════════════════════════════════════════════════╗
║              PARALLEL DEV PLAN — 总调度师                   ║
╠════════════════════════════════════════════════════════════╣
║ Task: checkout-feature                                     ║
║ Modules: 5 | Waves: 3 | Est: 8min (vs 12min sequential)   ║
║ ROI: 33% savings → PARALLEL APPROVED                       ║
╠════════════════════════════════════════════════════════════╣
║ MODULES                                                    ║
║ ┌─────────────┬──────────┬────────┬─────────────────────┐ ║
║ │ Module      │ Complex  │ Model  │ Dependencies        │ ║
║ ├─────────────┼──────────┼────────┼─────────────────────┤ ║
║ │ auth/       │ HIGH     │ Sonnet │ —                   │ ║
║ │ payment/    │ HIGH     │ Sonnet │ —                   │ ║
║ │ users/      │ LOW      │ Haiku  │ —                   │ ║
║ │ checkout/   │ MEDIUM   │ Sonnet │ auth, payment       │ ║
║ │ order/      │ LOW      │ Haiku  │ checkout            │ ║
║ └─────────────┴──────────┴────────┴─────────────────────┘ ║
╠════════════════════════════════════════════════════════════╣
║ WAVE PLAN                                                  ║
║ Wave 1: [auth, payment, users] ← 并行                      ║
║ Wave 2: [checkout] ← 等 auth+payment 完成                   ║
║ Wave 3: [order] ← 等 checkout 完成                          ║
╠════════════════════════════════════════════════════════════╣
║ DEPENDENCY GRAPH                                           ║
║ auth ──────────┐                                           ║
║                ├→ checkout ──→ order                        ║
║ payment ───────┘                                           ║
║ Critical Path: auth → checkout → order (~6min)             ║
╠════════════════════════════════════════════════════════════╣
║ REVIEW STRATEGY                                            ║
║ auth/ → security-reviewer (安全敏感)                        ║
║ payment/ → security-reviewer + database-reviewer           ║
║ users/ → code-reviewer                                     ║
║ checkout/ → performance-optimizer                          ║
╠════════════════════════════════════════════════════════════╣
║ RISKS                                                      ║
║ • checkout 依赖 auth+payment，是关键路径瓶颈                ║
║ • payment 需要 webhook 验证，可能需要额外时间               ║
╠════════════════════════════════════════════════════════════╣
║ 请确认计划，或提出修改意见。                                ║
╚════════════════════════════════════════════════════════════╝
```

**等待用户确认后才能执行。** 用户可以：确认 / 修改模块 / 调整优先级 / 取消。

### 2. 动态调整（执行中持续进行）

根据用户建议和实际反馈随时调整计划：

| 触发场景 | Commander 动作 |
|----------|---------------|
| 用户说"XX 模块拆分不对" | 重新分析，更新模块表和依赖图 |
| 用户说"这个优先级不对" | 调整 Wave 顺序 |
| Builder 实际耗时远超预估 | 重新评估 ROI，告知用户 |
| 发现新的依赖关系 | 更新依赖图，可能拆分新 Wave |
| 某模块比预估简单 | 降级模型（Sonnet→Haiku） |
| 用户说"加一个 XX 模块" | 重新评估模块、依赖、Wave、ROI |

调整时输出：

```
╔════════════════════════════════════════════════════════════╗
║              PLAN UPDATE — 总调度师                        ║
╠════════════════════════════════════════════════════════════╣
║ 变更原因: 用户建议 / 实际反馈 / 风险发现                    ║
╠════════════════════════════════════════════════════════════╣
║ 变更内容:                                                  ║
║   [!] Module order/ 复杂度 LOW → MEDIUM                    ║
║   [!] Wave 2 新增 order/（原计划 Wave 3）                   ║
║   [!] 模型: order/ Haiku → Sonnet                          ║
╠════════════════════════════════════════════════════════════╣
║ 影响: 预估 8min → 10min | ROI 33% → 25%                    ║
╠════════════════════════════════════════════════════════════╣
║ 请确认调整，或提出进一步修改。                              ║
╚════════════════════════════════════════════════════════════╝
```

### 3. 实时汇报（每个关键节点）

| 时机 | 汇报内容 |
|------|----------|
| Wave 开始 | 本 Wave 包含哪些模块，预计耗时 |
| 每个 Builder 完成 | 模块名、耗时、发现的问题 |
| 质量门检查 | 测试通过率、覆盖率、安全扫描结果 |
| 发现问题 | 问题描述、影响范围、建议方案 |
| Wave 完成 | 本 Wave 总结，下一 Wave 计划 |

汇报格式：

```
📊 Commander Report — Wave 1 进度
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ auth/        完成 (45s) — 0 issues
✅ payment/     完成 (38s) — 1 issue (已修复)
🔄 users/       进行中 (1:20) — building...
⏳ checkout/    等待中
⏳ order/       等待中

质量门: 2/3 通过 (等待 users/ 完成)
覆盖率: 78% (目标 80%)
```

发现问题时：

```
⚠️ Commander Alert — 发现问题
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
问题: payment/ 模块需要 webhook 验证
影响: checkout/ 依赖 payment/，可能延迟
建议:
  A) 增加 payment/ 复杂度为 HIGH，升级模型
  B) 先跳过 webhook，后续补充
请指示。
```

### 4. 决策矩阵

```
Situation               Action
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Stuck (>2min)           Auto-retry + upgrade model
Test failure            Block next wave, fix, continue
Coverage < 80%          Block next wave, add tests
Security issue          STOP immediately, notify user
Interface change        Notify affected builders
Build failure           Invoke build-error-resolver
Resource exhaustion     Reduce parallelism
User says "stop"        Execute abort strategy
```

### 5. 完成标准

```
Done Criteria:
  [✓] 所有模块构建成功
  [✓] 所有测试通过
  [✓] 覆盖率 ≥ 80%
  [✓] 无 CRITICAL 安全问题
  [✓] 代码审查完成
  [✓] CHANGELOG 生成
  [✓] 学习数据保存
```

---

## Auto-Trigger

**Suggest parallel when:**
- Task involves 3+ independent modules
- User says "快速", "并行", "同时处理"

**Stay sequential when:**
- Single file change
- Simple bug fix
- Tightly coupled changes

## Quick Start

```
1. ROI Gate → is parallel worth it?
2. Plan → module split + dependencies
3. Interfaces → shared contracts first
4. TDD → write tests (RED)
5. Execute → waves with quality gates
6. Review → parallel security/quality/perf
7. Merge → conflict-free merge
8. Learn → save for next session
```

---

## Phase 0: ROI Gate + Complexity

Commander 在出计划时自动完成 ROI 评估：

```
Parallel ROI Check
━━━━━━━━━━━━━━━━━━
Modules: 5 | Waves: 3 | Est: 8min (vs 12min sequential)
Savings: 33% > 20% threshold → PARALLEL APPROVED
```

**Model selection rules:**
| Complexity | Time | Model | Why |
|-----------|------|-------|-----|
| HIGH | any | Sonnet | Safety first |
| LOW | short | Haiku | Fast and cheap |
| LOW | long | Haiku | Volume play |

**ROI 不足时：** 告知用户并行节省不到 20%，建议串行执行。

## Phase 1: Plan + Module Split + Dependencies

**此阶段在 Commander 启动协议的"出计划"中完成。**

包含：模块拆分、依赖图、关键路径、Wave 计划、审查策略、风险评估。

如果用户修改计划，Commander 更新所有内容。

## Phase 2: Interface-First Design

Define shared contracts BEFORE building:

```typescript
// src/types/auth.types.ts
export interface AuthToken { ... }
export interface LoginRequest { ... }

// src/types/payment.types.ts
export interface PaymentIntent { ... }
```

**Auto-generate tests from interfaces:**
- Generate test cases from interfaces
- Review and approve

**Interface versioning:**
- Changes auto-notify affected builders
- Breaking changes flagged immediately

## Phase 3: TDD - Write Tests First

Write ALL tests before parallel implementation:

```
Tests Written: 46 (all RED)
  auth: 12 | payment: 8 | users: 6
  checkout: 15 | order: 5

Verify: All tests FAIL (RED state)
Commit: "test: add failing tests"
```

## Phase 4: Worktree Isolation

```bash
git worktree add .claude/worktrees/builder-auth -b feature/auth
git worktree add .claude/worktrees/builder-payment -b feature/payment
```

**Lockfile coordination:**
- First builder runs `pnpm install`
- Others reuse lockfile with `--frozen-lockfile`

## Phase 5: Dynamic Parallel Execution

Launch agents with context isolation:

```
Wave 1: [auth, payment, users] ← parallel
  - auth: [Sonnet] ← critical path priority
  - payment: [Sonnet]
  - users: [Haiku]
```

**Agent health monitoring:**
- Detect stuck agents (no output > 2min)
- Auto-retry with backoff: immediate → 5s → escalate

**Lightweight security scan on completion:**
- No hardcoded secrets
- No console.log
- No eval()

## Phase 6: Progressive Review

Reviews start as builders complete, run in parallel:

```
Builder done → Security Review (immediate)
All builders done → Code Quality + Performance Review (parallel)
```

**Auto-assign reviewers:**
| Module | Reviewers |
|--------|-----------|
| auth/ | security-reviewer |
| payment/ | security-reviewer, database-reviewer |
| users/ | code-reviewer |
| checkout/ | performance-optimizer |

## Phase 7: Wave Checkpoint + Quality Gate

After each wave, auto-commit and verify:

```
Wave 1 Quality Gate:
  [✓] Tests pass (12/12)
  [✓] Build succeeds
  [✓] Coverage ≥ 80%
  [✓] No critical security issues
  → GATE PASSED, launching Wave 2
```

**Gate fails?** Fix issues before proceeding.

## Phase 8: Change Impact Analysis

When module changes, analyze impact:

```
Modified: auth/types.ts
Impact:
  ├── checkout/ → NEEDS UPDATE
  └── order/ → NEEDS RETEST
Action: Notifying affected builders...
```

## Phase 9: Merge + Conflict Resolution

1. Merge worktrees — shared files first
2. Detect code duplication
3. Replace stubs
4. Full build + test suite
5. Verify coverage ≥ 80%

**File partitioning prevents conflicts:**
- Each builder owns specific files
- Shared files read-only during parallel phase

## Phase 10: Auto-Generate CHANGELOG

```markdown
### Features
- feat(auth): JWT authentication middleware
- feat(payment): Stripe integration

### Tests
- 47 tests added, coverage: 82%
```

## Phase 11: Cross-Session Learning

Save learnings to `~/.claude/learnings/parallel-dev/`:

```json
{
  "task": "checkout-feature",
  "modules": ["auth", "payment", "users", "checkout", "order"],
  "waves": 3,
  "actual_time": "12min",
  "lessons": ["payment needs webhook verification"]
}
```

---

## Rules

1. **Plan first** — 先出计划，用户确认后才执行
2. **Commander is main Claude** — 主 Claude 就是总调度师
3. **Plan is living** — 根据反馈随时调整计划
4. **Report at checkpoints** — 关键节点必须汇报
5. **ROI gate** — 并行节省 < 20% 时建议串行
6. **Interface-first** — 先定义共享合约再构建
7. **TDD** — 先写测试再实现
8. **Wave quality gates** — 当前波次通过才进入下一波
9. **Max 5 parallel** — 每波最多 5 个并行代理
10. **Auto-retry** — 立即 → 5秒 → 升级
11. **Progress persistence** — 保存状态支持恢复
12. **Abort strategy** — 停止代理，保存进度，报告结果

## Integration

- **planner** — module analysis and split
- **tdd-guide** — test strategy
- **code-reviewer** — quality review
- **security-reviewer** — security review (left-shifted)
- **performance-optimizer** — perf review
- **build-error-resolver** — if build fails

## Advanced

See `advanced.md` for:
- Adaptive waves
- Cache reuse
- Auto-fix common issues
- Feature flags
- Progressive delivery
- Debug mode
- Visual reports

## Templates

See `templates.md` for:
- ASCII dashboard templates
- CHANGELOG format
- Review checklists
- Knowledge base schema
