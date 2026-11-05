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
cat > /tmp/aladdin-daemon.yaml <<'YAML'
daemon:
  acp_url: "ws://127.0.0.1:8080/v1/acp"
  api_url: "http://127.0.0.1:8080"
  profile_dir: "/tmp/aladdin-profiles"
agent:
  claude:
    bin: "/tmp/fakeagent"        # 演示用；真实环境指向 claude-agent-acp
    expected_version: "0.100.0"
    input_timeout_seconds: 120
YAML

go build -o /tmp/fakeagent ./tests/fixtures/acp/fakeagent
TOKEN=$(./bin/aladdin machine token --machine-id <machine-id> --dsn "$MYSQL_DSN" --ttl 8h | tail -1)
ALADDIN_MACHINE_TOKEN="$TOKEN" ./bin/daemon --config /tmp/aladdin-daemon.yaml \
  --profile dev --machine-id <machine-id>
```

## 4. 创建任务

```bash
curl -s -X POST "http://127.0.0.1:8080/workspaces/<wid>/issues" \
  -H "Authorization: Bearer devtoken" -H 'Content-Type: application/json' \
  -d '{"name":"演示","mode":"ordinary","agentIds":["<agent-id>"],"plannerAgentId":"<agent-id>"}'
```

后续计划提交/确认/启动见 `dev_spec.md` §9.6。
