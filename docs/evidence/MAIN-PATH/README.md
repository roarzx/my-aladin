# 主路径验收证据 — 注册机器 → 配置 Agent → 创建 Issue → 生成 PR

环境：本机 Docker MySQL 8 / Redis 7；真实 `claude-agent-acp 0.101.0`；
私有演示仓库 `https://github.com/roarzx/aladdin-demo`。

## 步骤与结果

1. **注册机器（前端）**
   - `/machines` → 注册「Real Claude 机器」，页面返回 machine id 与一次性 token
   - daemon 使用该 token 启动后，机器状态变为 online（`lastSeenAt` 更新）
2. **配置 Agent（前端）**
   - `/agents` → 绑定 Real Claude 机器、workDir `/tmp/aladdin-run/realwork`、
     System Prompt、可见性
3. **创建 Issue（前端）**
   - `/task/create` → 选择 Agent、内联新增仓库 `roarzx/aladdin-demo`（main）
   - 创建后跳转 `/task/detail?id=2c3e723b-5f39-4944-aca6-621ea411dc40`
4. **计划与启动（驾驶舱）**
   - 提交 1 步计划 → 「确认并启动」
   - 任务进入 executing，daemon 领取并执行
5. **真实 Claude 执行 + PR**
   - Claude 在克隆仓库中修改 `README.md`
   - daemon 提交 `7ef9b03 aladdin: Real Claude PR 演示`
   - 推送到 `aladdin/2c3e723b` 并创建 PR
   - 驾驶舱时间线 `step.done` 显示「查看 PR」
6. **最终状态**
   - Task `chatting`，Step `completed`，attempt `completed`
   - PR：https://github.com/roarzx/aladdin-demo/pull/1
   - 演示仓库已设为 public（2026-11-06），PR 可匿名访问（HTTP 200）

## 证据

- 事件流：`task.created → plan.submitted → step.dispatched → step.done`
  （`step.done` payload 含 `pr_url`、`commit_sha`）
- 截图：
  - `docs/evidence/MAIN-PATH/ui-task-list.png`
  - `docs/evidence/MAIN-PATH/ui-machines.png`
  - `docs/evidence/MAIN-PATH/ui-agents.png`
  - `docs/evidence/MAIN-PATH/ui-cockpit-pr.png`
  - `docs/evidence/MAIN-PATH/github-pr.png`
- 仓库提交：`git log --oneline -1` → `7ef9b03 aladdin: Real Claude PR 演示`
- PR 校验：`gh pr list --repo roarzx/aladdin-demo --head aladdin/2c3e723b`

## CLI 注册方式复验

按参考实现的注册 UX 调整为：

```bash
go install github.com/roarzx/my-aladin/cmd/aladdin@latest
aladdin daemon start --machine-id <id> --server-auth-token <token>
```

2026-11-06 实测（任务 `9cdf0fc2-a334-4b23-a4dc-e728bb8af1a9`）：

- daemon 使用内置默认配置上线（无需 config.yaml / 环境变量）
- 真实 Claude 修改 README，commit `6ff1d45`
- PR：https://github.com/roarzx/aladdin-demo/pull/2

## 复现命令

```bash
make up && make build && make migrate
# 前端注册机器后，用页面给出的 token 启动真实 daemon：
aladdin daemon start --machine-id <machine-id> --server-auth-token <token>
# 浏览器完成 Agent/任务/计划/启动
```

前置：`gh auth setup-git` 已配置；`claude-agent-acp` 已安装且本机 Claude 已登录；
`config/config.yaml` 的 `agent.claude.auto_approve: true`。
