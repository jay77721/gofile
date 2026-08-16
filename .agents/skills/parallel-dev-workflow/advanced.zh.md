---
name: parallel-dev-workflow-advanced-zh
description: |
  并行开发工作流的高级优化。
  包括自适应 Wave、缓存复用、自动修复、Feature Flags、
  渐进式交付、调试模式、可视化报告等。
parent: parallel-dev-workflow
---

> This is the Chinese translation of [advanced.md](./advanced.md).

# 高级优化

## 自适应 Wave

根据前一个 Wave 的表现调整下一个 Wave：

```
Wave 1 结果:
  auth/    实际: 45秒（预估 3 分钟）  → 比预期简单
  payment/ 实际: 2 分钟（预估 3 分钟）→ 接近预估

Wave 2 调整:
  checkout/ 预估: 3 分钟 → 2 分钟
  模型: Sonnet → Haiku（模块比预期简单）
```

**规则：**
- 实际 < 50% 预估 → 简化模型
- 实际 > 150% 预估 → 升级模型
- Wave 提前完成 → 从下一 Wave 拉取模块

## 缓存复用

复用相似模块的实现模式：

```
历史: auth/ 使用 JWT + refresh token
当前: auth/ 需要类似功能 → 复用结构
```

**缓存来源：**
1. 跨会话学习成果
2. 当前项目中的相似模块
3. 知识库模式
4. 开源实现

## 自动修复常见问题

构建者完成时检测并自动修复：

```
检测到:
  auth/:42 - console.log → 已自动移除
  payment/:89 - 未使用的 import → 已自动移除
  users/:15 - 缺少错误处理 → 已自动添加 try-catch
```

**规则：**
- console.log → 移除
- 未使用的 imports → 移除
- 缺少错误处理 → 添加 try-catch
- 硬编码值 → 提取到配置
- 复杂嵌套 → 建议重构（不自动修改）

## Feature Flags

自动配置 Feature Flags：

```typescript
export const FEATURE_CHECKOUT = process.env.FEATURE_CHECKOUT === 'true'

if (FEATURE_CHECKOUT) {
  // 新流程
} else {
  // 旧流程
}
```

**优势：** 渐进式发布、易于回滚、支持 A/B 测试。

## 渐进式交付

独立部署模块：

```
Wave 1 完成 → auth + payment 可部署
Wave 2 完成 → checkout 可部署
Wave 3 完成 → 完整功能上线

策略:
  auth → canary (10%) → 全量
  checkout → feature flag → 渐进式
```

## 增量测试

仅运行受影响的测试：

```
已变更: auth/, checkout/
受影响: auth.test.ts, checkout.test.ts, integration.test.ts
已跳过: users.test.ts（未受影响）

测试时间: 45 秒（完整套件需 3 分钟）
```

**如何确定：**
1. 解析 import 图
2. 查找导入了已变更模块的测试文件
3. 查找涉及已变更 API 的集成测试

## 测试并行化

并行运行单元测试：

```
串行:  auth 2.0秒 + payment 1.5秒 + users 1.0秒 = 4.5秒
并行:  max(2.0, 1.5, 1.0) = 2.0秒（2.25 倍加速）
```

**规则：**
- 单元测试 → 始终并行
- 集成测试 → 无共享状态时并行
- E2E 测试 → 串行（共享浏览器）

## 构建者结果缓存

模块未变更时复用：

```
模块: users/
上次实现: 2026-05-28, hash: abc123
当前接口: 无变更
→ 复用缓存实现（0 秒）
→ 运行测试验证: ✓ 通过
```

**失效条件：** 接口变更、测试变更、依赖变更。

## 代码指标跟踪

跟踪每个模块的质量：

```
模块       复杂度    覆盖率    重复数
auth/      12        82%       0
payment/   18        75%       1
users/     5         88%       0
```

**阈值：** 复杂度 < 20，覆盖率 > 80%，重复数 < 3。

## 调试模式

启用详细日志以便排查问题：

```
调试模式: 开启
- 代理输入/输出: 记录到 ~/.claude/debug/
- 执行轨迹: 完整时间线
- Token 使用量: 按代理跟踪
- 错误: 完整堆栈跟踪
```

**启用方式：** 用户说 "debug mode" 或 "调试模式"。

## 可视化报告

完成后生成 HTML 报告：

```markdown
## 报告: checkout-feature
- 时间线: Wave 1 (2分钟) → Wave 2 (1.5分钟) → Wave 3 (1分钟)
- 覆盖率: 82%（目标 80%）
- 发现问题: 3（已全部修复）
- 使用代理: 5（3 个 Sonnet，2 个 Haiku）
```

**位置：** `~/.claude/reports/parallel-dev/{task}.html`

## 多项目并行

支持跨仓库并行开发：

```
项目 A: auth-service（后端）
项目 B: auth-client（前端）
共享: 接口定义

并行: 同时构建两个仓库
合并: 协调接口变更
```

**适用场景：** 微服务、monorepo 拆分。

## 自动创建 PR

成功合并后，自动创建 PR：

```bash
gh pr create --title "feat: checkout feature" --body "$(cat <<'EOF'
## Summary
- Added auth, payment, users, checkout, order modules
- 47 tests, 82% coverage

## Test plan
- [ ] All tests pass
- [ ] Coverage >= 80%
- [ ] No security issues
EOF
)"
```

**PR 包含：** CHANGELOG、覆盖率报告、测试结果。

## 模块优先级队列

分配业务优先级：

```
P0 [CRITICAL] auth/       → Sonnet
P1 [HIGH]     payment/    → Sonnet
P2 [MEDIUM]   checkout/   → Sonnet
P3 [LOW]      order/      → Haiku
```

**考虑因素：** 业务关键性、安全敏感度、用户影响。

## 性能基线

建立并对比基线：

```
之前: API p50=45ms, p95=120ms
之后: API p50=48ms, p95=125ms (+4%)

判定: 无回归（在 10% 阈值内）
```

**规则：** 超过 10% 回归时标记。

## 日志聚合

跨代理统一视图：

```
[10:00:01] Builder(auth): 开始 JWT
[10:00:02] Builder(payment): 开始 Stripe
[10:00:15] Builder(auth): 创建 middleware.ts
[10:00:25] Reviewer(security): auth/ - 2 个问题
```

**优势：** 统一视图、便于调试、时间线可视化。

## 增量回滚

回滚单个模块，而非整个 Wave：

```
Wave 2: checkout/（失败）+ order/（通过）
→ 仅回滚 checkout/
→ 重新运行 checkout/ 构建者
→ 继续使用已完成的 order/
```

## 中止策略

当用户说 "stop" 或 "abort" 时：

```
1. 停止所有运行中的代理
2. 保存进度到 .claude/parallel-dev-progress.json
3. 不提交部分完成的工作
4. 报告已完成 vs 已放弃的内容

恢复: "继续上次的并行开发"
```

## 自动依赖更新

开发前检查：

```
包名         当前版本   最新版本   类型
stripe       12.0.0    12.1.0    minor（安全）
jsonwebtoken 8.5.1     9.0.0     major（需审查）

自动更新: 1 个安全更新
已标记: 1 个 major 更新（需手动审查）
```

**规则：** Patch/minor → 自动更新。Major → 标记待审查。

## 文档同步

检查文档保持同步：

```
已变更: src/auth/middleware.ts
  ⚠ README auth 部分可能过时
  ✓ API 文档已自动更新
  ✓ CHANGELOG 已更新

操作: 标记 README 相关部分待审查
```
