---
name: parallel-dev-workflow-zh
description: |
  基于 Commander 编排的动态并行开发，支持智能模型选择、
  Wave 检查点、并行审查、自动重试和跨会话学习。
  触发词: "并行开发", "parallel dev", "快速开发", "多模块开发"
---

> This is the Chinese translation of [SKILL.md](./SKILL.md).

# 并行开发工作流

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
场景                    操作
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
卡住（>2 分钟）          自动重试 + 升级模型
测试失败                阻塞下一 Wave，修复后继续
覆盖率 < 80%            阻塞下一 Wave，补充测试
安全问题                立即停止，通知用户
接口变更                通知受影响的构建者
构建失败                调用 build-error-resolver
资源耗尽                降低并行度
用户说 "stop"           执行中止策略
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

## 自动触发

**建议并行的场景:**
- 任务涉及 3+ 个独立模块
- 用户说 "快速", "并行", "同时处理"

**保持串行的场景:**
- 单文件修改
- 简单 bug 修复
- 紧密耦合的变更

## 快速开始

```
1. ROI 门控 → 并行是否值得？
2. 规划 → 模块拆分 + 依赖关系
3. 接口 → 先定义共享契约
4. TDD → 先写测试（RED）
5. 执行 → 带质量门控的 Wave
6. 审查 → 安全/质量/性能并行审查
7. 合并 → 无冲突合并
8. 学习 → 保存供下次使用
```

---

## Phase 0: ROI 门控 + 复杂度评估

Commander 在出计划时自动完成 ROI 评估：

```
并行 ROI 检查
━━━━━━━━━━━━━━━━━━
模块: 5 | Wave: 3 | 预估: 8分钟（串行需 12 分钟）
节省: 33% > 20% 阈值 → 批准并行
```

**模型选择规则:**
| 复杂度 | 时间 | 模型 | 原因 |
|--------|------|------|------|
| HIGH | 任意 | Sonnet | 安全优先 |
| LOW | 短 | Haiku | 快速且经济 |
| LOW | 长 | Haiku | 批量处理 |

**ROI 不足时：** 告知用户并行节省不到 20%，建议串行执行。

## Phase 1: 规划 + 模块拆分 + 依赖关系

**此阶段在 Commander 启动协议的"出计划"中完成。**

包含：模块拆分、依赖图、关键路径、Wave 计划、审查策略、风险评估。

如果用户修改计划，Commander 更新所有内容。

## Phase 2: 接口优先设计

在构建前定义共享契约：

```typescript
// src/types/auth.types.ts
export interface AuthToken { ... }
export interface LoginRequest { ... }

// src/types/payment.types.ts
export interface PaymentIntent { ... }
```

**从接口自动生成测试：**
- 从接口生成测试用例
- 审查并批准

**接口版本控制：**
- 变更自动通知受影响的构建者
- 破坏性变更立即标记

## Phase 3: TDD - 先写测试

在并行实现前写好所有测试：

```
测试已写: 46 个（全部 RED）
  auth: 12 | payment: 8 | users: 6
  checkout: 15 | order: 5

验证: 所有测试失败（RED 状态）
提交: "test: add failing tests"
```

## Phase 4: Worktree 隔离

```bash
git worktree add .claude/worktrees/builder-auth -b feature/auth
git worktree add .claude/worktrees/builder-payment -b feature/payment
```

**锁文件协调：**
- 第一个构建者运行 `pnpm install`
- 其他构建者使用 `--frozen-lockfile` 复用锁文件

## Phase 5: 动态并行执行

启动代理，上下文隔离：

```
Wave 1: [auth, payment, users] ← 并行
  - auth: [Sonnet] ← 关键路径优先
  - payment: [Sonnet]
  - users: [Haiku]
```

**代理健康监控：**
- 检测卡住的代理（>2 分钟无输出）
- 自动重试：立即 → 5秒 → 升级

**轻量安全扫描：**
- 无硬编码密钥
- 无 console.log
- 无 eval()

## Phase 6: 渐进式审查

构建者完成后立即开始审查，并行执行：

```
构建者完成 → 安全审查（立即）
所有构建者完成 → 代码质量 + 性能审查（并行）
```

**自动分配审查者：**
| 模块 | 审查者 |
|------|--------|
| auth/ | security-reviewer |
| payment/ | security-reviewer, database-reviewer |
| users/ | code-reviewer |
| checkout/ | performance-optimizer |

## Phase 7: Wave 检查点 + 质量门控

每个 Wave 完成后，自动提交并验证：

```
Wave 1 质量门:
  [✓] 测试通过 (12/12)
  [✓] 构建成功
  [✓] 覆盖率 ≥ 80%
  [✓] 无关键安全问题
  → 门控通过，启动 Wave 2
```

**门控失败？** 修复问题后再继续。

## Phase 8: 变更影响分析

模块变更时，分析影响：

```
已修改: auth/types.ts
影响:
  ├── checkout/ → 需要更新
  └── order/ → 需要重新测试
操作: 通知受影响的构建者...
```

## Phase 9: 合并 + 冲突解决

1. 合并 worktree — 先处理共享文件
2. 检测代码重复
3. 替换桩代码
4. 完整构建 + 测试套件
5. 验证覆盖率 ≥ 80%

**文件分区防止冲突：**
- 每个构建者拥有特定文件
- 并行阶段共享文件为只读

## Phase 10: 自动生成 CHANGELOG

```markdown
### Features
- feat(auth): JWT 认证中间件
- feat(payment): Stripe 集成

### Tests
- 新增 47 个测试，覆盖率: 82%
```

## Phase 11: 跨会话学习

将学习成果保存到 `~/.claude/learnings/parallel-dev/`：

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

## 规则

1. **计划先行** — 先出计划，用户确认后才执行
2. **Commander 就是主 Claude** — 主 Claude 就是总调度师
3. **计划是活的** — 根据反馈随时调整计划
4. **关键节点必须汇报** — 每个检查点向用户汇报
5. **ROI 门控** — 并行节省 < 20% 时建议串行
6. **接口优先** — 先定义共享合约再构建
7. **TDD** — 先写测试再实现
8. **Wave 质量门控** — 当前波次通过才进入下一波
9. **最多 5 个并行** — 每波最多 5 个并行代理
10. **自动重试** — 立即 → 5秒 → 升级
11. **进度持久化** — 保存状态支持恢复
12. **中止策略** — 停止代理，保存进度，报告结果

## 集成

- **planner** — 模块分析和拆分
- **tdd-guide** — 测试策略
- **code-reviewer** — 质量审查
- **security-reviewer** — 安全审查（左移）
- **performance-optimizer** — 性能审查
- **build-error-resolver** — 构建失败时使用

## 高级功能

参见 `advanced.zh.md`：
- 自适应 Wave
- 缓存复用
- 自动修复常见问题
- Feature Flags
- 渐进式交付
- 调试模式
- 可视化报告

## 模板

参见 `templates.zh.md`：
- ASCII 仪表盘模板
- CHANGELOG 格式
- 审查检查清单
- 知识库 Schema
