> **状态：已交付（2026-11-06）**。真实 Claude Code 已完成
> `注册机器 → 配置 Agent → 创建 Issue → 确认启动 → 修改仓库 → commit/push → PR`，
> PR：https://github.com/roarzx/aladdin-demo/pull/1
> 证据：`docs/evidence/MAIN-PATH/README.md`。

# 主路径交付计划：注册机器 → 配置 Agent → 创建 Issue → 生成 PR

> 目标：用真实 Claude Code + 私有演示仓库 `roarzx/aladdin-demo` 跑通主路径。
> 范围：主路径优先；小队/Flow/其余 28 个页面保持现状，不阻塞本条链路。

## 验收标准

1. 前端「机器管理」可注册机器，服务端返回一次性机器密钥；daemon 用该密钥上线，
   页面显示 online。
2. 前端「Agent 管理」可基于在线机器创建 Agent（名称、workDir、System Prompt、可见性）。
3. 前端「创建任务」可选择工作区、Agent、仓库并创建 Issue。
4. 驾驶舱可提交计划、确认并启动任务。
5. daemon 在机器上 clone 仓库、切计划分支、运行真实 Claude Code 修改文件；
   完成后 commit、push，并创建 GitHub PR。
6. 驾驶舱显示 PR 链接；任务进入 `chatting`；`activities` 含 `step.done` 与 pr_url。

演示仓库：`https://github.com/roarzx/aladdin-demo`（main 分支）。
真实 Agent 可执行文件：`/Users/zx/.local/bin/claude-agent-acp`（0.101.0）。

---

## 一、数据库

新增迁移 `0006_repositories_meta`：

```sql
ALTER TABLE repositories
  ADD COLUMN default_branch VARCHAR(255) NOT NULL DEFAULT 'main',
  ADD COLUMN provider ENUM('github','git') NOT NULL DEFAULT 'github';
```

`task_repositories.base_sha` 保持空字符串（由 daemon clone 后以实际 HEAD 为准），
`target_branch` 在启动时写入任务的 plan_branch。

## 二、控制面 HTTP API（用户令牌，需 Workspace 成员校验）

统一响应 `{data: ...}` / `{error:{code,message}}`。

### Workspaces
- `GET /workspaces` → `[{id,name,role}]`（仅当前用户加入的工作区）

### Machines
- `GET /machines` → `[machineView]`
  ```
  {id, workspaceId, name, kind, status, capabilities, cliVersion, lastSeenAt}
  ```
- `POST /machines` `{workspaceId,name,kind?}` →
  `{machine: machineView, token: "<plaintext>", expiresAt}`
  - 服务端生成 id、写 `machines`（status=offline）并签发 machine token（只返回一次明文）
  - 同一工作区名称唯一；名称冲突返回 `CONFLICT`
- `POST /machines/{id}/token` → `{token, expiresAt}`（重新签发）

### Agent
- `GET /agents?workspaceId=` → `[agentView]`
  `{id,workspaceId,name,ownerId,visibility,runtimeType,machineId,workDir,model,
    systemPrompt,capabilitySet,envVars,enabled}`
- `POST /agents` `{workspaceId,name,machineId,workDir,systemPrompt,visibility?,model?,
  capabilitySet?,envVars?,enabled?}` → agentView
  - machine 必须属于同一工作区；workDir 必填绝对路径
- `PATCH /agents/{id}` 局部更新同上字段 → agentView
- `DELETE /agents/{id}` → 置 `enabled=false`（软删除）

### Repositories
- `GET /repositories?workspaceId=` → `[{id,workspaceId,url,defaultBranch,provider}]`
- `POST /repositories` `{workspaceId,url,defaultBranch?,provider?}` → 同 view

### 任务读取扩展
`GET /issues/{id}` 增加：

```json
{
  "agents": [{"ref":"a1","agentId":"...","name":"...","role":"planner"}],
  "repositories": [{"id":"...","url":"...","defaultBranch":"main"}]
}
```

### 机器在线状态
- WS 握手成功后：`machines.status=online`、`last_seen_at=now`
- 连接断开：`machines.status=offline`
- 通过 ws.Server 的 `OnConnect(ctx, machineID)` / `OnDisconnect(ctx, machineID)` 回调实现，
  在 `cmd/server/main.go` 注入 DB 更新

## 三、Task 创建与启动

- `POST /workspaces/{wid}/issues` 已有 `repositories[]`（repo id 列表）；
  保持只支持 1 个仓库。
- 任务创建时把 repo id 写入 `task_repositories`；`base_sha=''`、`target_branch=''`。
- `plan.Service.Start` 分配 `plan_branch` 后，把 `task_repositories.target_branch` 更新为该分支。
- `GET /issues/{id}` 返回 repositories，供驾驶舱展示。

## 四、派发契约（Server → daemon）

扩展 `runtime.DispatchRequest`：

```go
WorkDir           string // agent.work_dir（机器上的绝对路径）
RepoURL           string // 任务仓库 URL（可空）
RepoDefaultBranch string // main
SystemPrompt      string
TaskTitle         string
StepTitle         string
```

`hub.Dispatch` 把以下字段写入 `start_step_session.params`（snake_case）：
`work_dir, repo_url, repo_default_branch, system_prompt, task_title, step_title`。

`PlanRunner`：
- 读取 agent 配置（已有）与任务第一个仓库（新增 store 查询）
- prompt = `task.Name + "\n\n" + step.Title + "\n" + step.Body`
- 填入上述字段

`step_done` 事件 data 增加：`pr_url`（可空）。

## 五、daemon 执行与 PR

`StepHandler`：

1. 计算工作目录：
   - 有 `repo_url`：`filepath.Join(work_dir, "aladdin", short(task_id))`
   - 无 repo：`work_dir`
2. 有 repo 时使用 `git.Prepare`（已存在）：clone/open、切/建 `branch`；
   启动前 `EnsureClean` 失败则 step_fail（不清理用户改动）。
3. Provider Cwd = 工作目录；prompt 前缀 `system_prompt + "\n\n"`。
4. 成功后（有 repo）：
   - `git status --porcelain` 有改动 → `git add -A` + `git commit -m "aladdin: <task_title>"`
   - 提交后 `git rev-parse HEAD` 得到 commit_sha
   - `git push origin <branch>`（已有 `Workspace.Push`，仅允许计划分支）
   - `gh pr create --repo <owner/repo> --base <defaultBranch> --head <branch>
      --title "<task_title>" --body "..."`；解析 stdout 最后一行为 PR URL；
     若已存在 PR，用 `gh pr view <branch> --json url` 取 URL
   - 失败（push/PR）→ step_fail，error_class=tool，reason 含 stderr
5. `step_done` 携带 `commit_sha`、`branch`、`pr_url`、`summary_digest`（summary 的 sha256）。

Git 身份：提交前若仓库未配置 user.name/email，则本地设置
`Aladdin Bot <aladdin@example.com>`。
认证：依赖机器上的 `gh auth setup-git`（本机已配置）。

ACP 侧已支持：
- `session/new` 的 `systemPrompt: {mode:"append", content:[{type:"text",...}]}`
- `session/request_permission` 在 `agent.claude.auto_approve: true` 时自动选择
  `allow_once`/`allow_always`；daemon 真实配置需开启该开关。
真实 Claude Code 文件编辑已验证（`go test -tags=realacp ... -run TestRealPromptEditsFile`）。

## 六、前端（主路径页面）

### 机器管理
- 真实 `GET /machines` 列表；注册弹窗调用 `POST /machines`
- 成功后展示一次性 token、machine id、daemon 启动命令（含复制按钮）
- 刷新按钮重新拉取；在线/离线状态来自服务端

### Agent 管理
- 真实 `GET /agents` 列表；创建/编辑表单：
  名称、机器（下拉，展示 online）、workDir、System Prompt、可见性、启用
- 无 Agent 时给出创建引导

### 创建任务
- 加载 `GET /workspaces`（取第一个或下拉）、`GET /agents`、`GET /repositories`
- 表单：任务名称、需求描述、Agent、仓库（可内联新增仓库 URL + 默认分支）
- 提交 `POST /workspaces/{wid}/issues`，成功后跳转 `/task/detail?id=<taskId>`
- 需求描述暂存于前端 state，用于随后计划表单的默认正文（不新增后端字段）

### 驾驶舱
- 无计划时显示「提交计划」表单：正文 + 至少 1 个步骤（标题/正文，agentRef 默认 a1）
  → `POST /issues/{id}/plans`
- 计划存在且 `status=idle`：显示「确认计划」「确认并启动」
  → `POST /plans/{id}/confirm`、`POST /issues/{id}/start`（expectedVersion 用 task.version）
- 任务状态为 `executing/paused` 时显示状态与恢复区（重试已有）
- 时间线渲染事件 payload；`step.done` 含 pr_url 时显示「查看 PR」链接
- 所有按钮失败时显示服务端 error.message

## 七、文件所有权（并行 agent 约定）

- **Backend agent**：`cmd/server/main.go`、`internal/httpapi/`、`internal/task/`、
  `internal/machine/`、`internal/agent/`、`internal/store/mysql/`、`internal/runtime/`、
  `internal/orchestration/plan_runner.go`、`internal/transport/ws/server.go`、
  `migrations/0006_*`、相关测试。**不碰 `internal/daemon/` 与 `web/`。**
- **Daemon agent**：`internal/daemon/`（step_handler、git 扩展）、
  `internal/orchestration/step_done` 相关若需微调先读后端契约；
  **默认不碰 server/httpapi/web。**
- **Frontend agent**：`web/` 仅主路径页面；**不碰 Go。**
- 所有 agent 不 commit、不 push，不发真实 PR；由主会话统一验证与提交。

## 八、验证

```bash
go build ./...
go test ./... -count=1
go test -tags=integration ./internal/... -count=1 -p 1
pnpm --dir web test && pnpm --dir web build
```

最终人工 E2E（主会话）：
1. 页面注册机器 → 复制 token 启动 daemon（真实 ACP）
2. 页面创建 Agent → 创建任务（仓库 aladdin-demo）
3. 驾驶舱提交计划 → 确认并启动
4. 真实 Claude 修改仓库 → 自动 commit/push → 生成 PR → 驾驶舱显示链接
