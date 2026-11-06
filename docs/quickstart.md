# 快速启动

## 1. 依赖与迁移

```bash
make up && make build
export MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4'
export REDIS_ADDR=127.0.0.1:6379
make migrate
```

## 2. 启动控制面

```bash
export ALADDIN_ADMIN_TOKEN=devtoken
./bin/server
```

## 3. 启动执行面

```bash
# 安装 CLI（或在仓库内 make build 后使用 ./bin/aladdin）
go install github.com/roarzx/my-aladin/cmd/aladdin@latest

# 前端「机器管理」注册机器后，复制页面给出的命令：
aladdin daemon start \
  --machine-id <machine-id> \
  --server-auth-token <token>
```

默认连接 `ws://127.0.0.1:8080/v1/acp` 与 `http://127.0.0.1:8080`；
远程控制面用 `--acp-url` / `--api-url` 覆盖。
真实 Agent 需安装 `npm i -g @agentclientprotocol/claude-agent-acp` 并完成 claude 登录。

## 4. 创建任务

```bash
curl -s -X POST "http://127.0.0.1:8080/workspaces/<wid>/issues" \
  -H "Authorization: Bearer devtoken" -H 'Content-Type: application/json' \
  -d '{"name":"演示","mode":"ordinary","agentIds":["<agent-id>"],"plannerAgentId":"<agent-id>"}'
```

后续计划提交/确认/启动见 `dev_spec.md` §9.6。
