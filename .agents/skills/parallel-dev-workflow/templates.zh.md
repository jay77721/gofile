---
name: parallel-dev-workflow-templates-zh
description: |
  并行开发工作流的模板。
  包括 Commander 仪表盘、进度条、质量门控、
  CHANGELOG、审查检查清单和 Schema 定义。
parent: parallel-dev-workflow
---

> This is the Chinese translation of [templates.md](./templates.md).

# 模板

## Commander 仪表盘

```
╔════════════════════════════════════════════════════════════╗
║           并行开发工作流 - COMMANDER                         ║
╠════════════════════════════════════════════════════════════╣
║ 任务: checkout-feature                                     ║
║ Wave: 2/3 | 已用时: 5分32秒 | 预计剩余: 3分钟               ║
╠════════════════════════════════════════════════════════════╣
║ 构建者                                                     ║
║   [✓] auth/          45秒  Sonnet   0 问题                  ║
║   [✓] payment/       38秒  Sonnet   1 问题（已修复）          ║
║   [⟳] checkout/      1:20  Sonnet   构建中...               ║
║   [ ] order/         —     Haiku    等待中                  ║
╠════════════════════════════════════════════════════════════╣
║ 测试器                                                     ║
║   [✓] 单元测试       12 个测试通过                            ║
║   [⟳] 集成测试       运行中...                               ║
║   [ ] E2E           等待中                                  ║
╠════════════════════════════════════════════════════════════╣
║ 审查                                                       ║
║   [✓] 安全审查       无问题 (Opus)                           ║
║   [!] 质量审查       发现 2 个问题 (Sonnet)                   ║
║   [ ] 性能审查       等待中                                  ║
╠════════════════════════════════════════════════════════════╣
║ 覆盖率: 78%（目标: 80%）                                    ║
║ 问题: 2 个待处理，3 个已修复                                  ║
║ 健康: 所有代理正常                                           ║
╚════════════════════════════════════════════════════════════╝
```

## 进度条

```
进度: ████████░░░░░░░░ 50%（3/6 模块完成）
```

## 质量门控模板

```
Wave N 质量门控:
━━━━━━━━━━━━━━━━━━━
[✓] 测试通过 (X/Y)
[✓] 构建成功
[✓] 覆盖率 ≥ 80%（实际: XX%）
[✓] 无关键安全问题
→ 门控通过/通过但有警告/失败
```

## CHANGELOG 模板

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

## 审查检查清单模板

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

## 学习 Schema

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

## 知识库 Schema

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

## 进度持久化 Schema

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

## 代理健康模板

```
代理健康:
  Builder A (auth):      [HEALTHY] 45秒, 修改 12 个文件
  Builder B (payment):   [HEALTHY] 38秒, 修改 8 个文件
  Builder C (users):     [STUCK] 2 分钟无输出
    → 发送心跳...
    → 重启代理...
    → [HEALTHY] 重启成功
```

## 依赖图模板

```
模块依赖:
━━━━━━━━━━━━━━━━━━━

auth ──────────┐
               ├→ checkout ──→ order
payment ───────┘

关键路径（最长）: auth → checkout → order
  auth: 约 3 分钟
  checkout: 约 2 分钟
  order: 约 1 分钟
  总计: 约 6 分钟
```

## 文件分区模板

```
文件分区映射:
━━━━━━━━━━━━━━━━━━

Builder A (auth/):
  - src/auth/middleware.ts
  - src/auth/routes.ts
  - src/auth/types.ts

Builder B (payment/):
  - src/payment/stripe.ts
  - src/payment/webhook.ts
  - src/payment/types.ts

共享（并行阶段只读）:
  - src/types/shared.ts
  - src/utils/*
```

## 回滚报告模板

```
回滚报告:
━━━━━━━━━━━━━━━━

已回滚: Wave 2
原因: checkout/ 测试失败

回滚前:
  [✓] Wave 1: auth, payment, users
  [✗] Wave 2: checkout（失败）

回滚后:
  [✓] Wave 1: auth, payment, users（已验证）
  [ ] Wave 2: checkout（重新运行中）

操作: 使用修复后重新运行 Wave 2...
```

## 中止报告模板

```
中止报告:
━━━━━━━━━━━━━

已完成:
  [✓] auth/（已提交）
  [✓] payment/（已提交）

已放弃:
  [~] checkout/（完成 60%）

未开始:
  [ ] order/

进度已保存。恢复命令: "继续上次的并行开发"
```
