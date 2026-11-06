# UI E2E（Playwright）验收证据

实现：`web/playwright.config.ts`、`web/e2e/`，CI job `ui-e2e`。

## 覆盖（3 个 chromium spec）

1. `machines.spec.ts` — 机器注册：一次性 token、machine id、`aladdin daemon start` 命令展示
2. `agents.spec.ts` — Agent 创建：绑定机器、工作目录、System Prompt
3. `main-flow.spec.ts` — 主路径：
   - API 创建机器并通过真实 daemon（fakeagent）上线
   - 浏览器创建 Agent
   - 浏览器创建 Issue（绑定仓库）→ 提交计划 → 确认并启动
   - daemon clone 本地 bare 仓库 → fakeagent 写文件 → commit/push
   - `ALADDIN_PR_PROVIDER=local` 返回 `local://pull/<branch>`
   - 驾驶舱显示 `step.done` 与「查看 PR」链接；任务进入 `chatting`

## 测试环境

- 真实 MySQL 8 / Redis 7（`deploy/docker-compose.yml`）
- 真实 server + 真实 daemon 二进制
- fake ACP agent（`tests/fixtures/acp/fakeagent` 的 `write_file` 模式）
- 本地 bare git 仓库（每次运行重建，`file://` URL）
- 不访问 GitHub、不调用真实 Claude

## 运行

```bash
make up
make test-ui-e2e
# 或
cd web && pnpm exec playwright test
```

结果（2026-11-06）：

```
3 passed (6.5s)
```

## CI

`.github/workflows/ci.yml` 的 `ui-e2e` job：启动 Docker 依赖 → 迁移 → 构建 →
`playwright install --with-deps chromium` → `pnpm --dir web test:e2e`。

## 限制

- 执行面使用 fakeagent，真实 Claude Code 的链路仍由人工/nightly 验证
- PR 使用本地 provider（`local://`），真实 GitHub PR 不在 CI 中创建
- 测试依赖 Docker 与本地端口 8080/5173
