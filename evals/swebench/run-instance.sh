#!/usr/bin/env bash
# 跑通一个 SWE-bench 实例的「环境 → 交付」链路，判分交给外部 harness。
#
#   1. 构建 aladdin 二进制 + 题目镜像
#   2. 起容器（里面跑 daemon，连回控制面）
#   3. 注册机器 / Agent（eval_mode=1）
#   4. 建 Issue（创建即执行，提示词 = instruction.md 原文）
#   5. 等这一轮结束，导出 git diff 到 jobs 目录
#
# 用法：
#   ALADDIN_ADMIN_TOKEN=devtoken \
#   ./evals/swebench/run-instance.sh \
#     --instruction /path/to/instruction.md \
#     --job my-job-001
#
# 依赖：docker、go、curl、python3。
set -euo pipefail

cd "$(dirname "$0")/../.."   # 仓库根目录

CONFIG=evals/swebench/agent.example.yaml
INSTRUCTION=""
JOB_NAME=""
KEEP=0
TIMEOUT=${EVAL_TIMEOUT:-5400}

usage() {
  sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instruction) INSTRUCTION="$2"; shift 2 ;;
    --config) CONFIG="$2"; shift 2 ;;
    --job) JOB_NAME="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    -h|--help) usage ;;
    *) echo "未知参数: $1" >&2; usage 1 ;;
  esac
done

[[ -n "${ALADDIN_ADMIN_TOKEN:-}" ]] || { echo "缺少 ALADDIN_ADMIN_TOKEN" >&2; exit 1; }
[[ -n "$INSTRUCTION" && -f "$INSTRUCTION" ]] || { echo "需要 --instruction <文件>" >&2; exit 1; }

# agent.example.yaml 是扁平 key: value，用 sed 取值即可，不引入 yq 依赖。
cfg() { sed -n "s/^$1:[[:space:]]*//p" "$CONFIG" | head -1 | sed 's/^"//; s/"$//'; }

INSTANCE=${INSTANCE:-$(cfg instance)}
IMAGE=${IMAGE:-$(cfg image)}
PLATFORM=${PLATFORM:-$(cfg platform)}
BASE_COMMIT=${BASE_COMMIT:-$(cfg base_commit)}
WORK_DIR=${WORK_DIR:-$(cfg work_dir)}
CONTROL_PLANE=${CONTROL_PLANE:-$(cfg control_plane)}
WORKSPACE_ID=${WORKSPACE_ID:-$(cfg workspace_id)}
MACHINE_NAME=${MACHINE_NAME:-$(cfg machine_name)}
AGENT_NAME=${AGENT_NAME:-$(cfg agent_name)}
ISSUE_NAME=${ISSUE_NAME:-$(cfg issue_name)}
ISSUE_NAME=${ISSUE_NAME:-$INSTANCE}
JOB_NAME=${JOB_NAME:-"$INSTANCE-$(date +%Y%m%d-%H%M%S)"}
JOB_DIR="evals/jobs/$JOB_NAME"

API="$CONTROL_PLANE"
AUTH=(-H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" -H "Content-Type: application/json")
CONTAINER="aladdin-eval-$(echo "$JOB_NAME" | tr -cd 'a-z0-9-')"

log() { printf '\033[36m==>\033[0m %s\n' "$*"; }

# 读 JSON 字段：jget <expr>，输入从 stdin。
jget() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }

mkdir -p "$JOB_DIR"
log "任务目录 $JOB_DIR"

# ---------- 1. 构建 ----------
log "构建 aladdin（linux/amd64）"
mkdir -p evals/swebench/.build
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o evals/swebench/.build/aladdin ./cmd/aladdin

log "构建镜像 $IMAGE"
docker build -f evals/swebench/Dockerfile --platform "$PLATFORM" \
  --build-arg "BASE_IMAGE=$IMAGE" -t "aladdin-eval:$(echo "$INSTANCE" | tr -cd 'a-z0-9-')" .

# ---------- 2. 机器 / Agent ----------
if [[ -z "$WORKSPACE_ID" ]]; then
  WORKSPACE_ID=$(curl -sS -H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" "$API/workspaces" | jget 'd["data"][0]["id"]')
fi
log "工作区 $WORKSPACE_ID"

machine_json=$(curl -sS -X POST "${AUTH[@]}" \
  -d "{\"workspaceId\":\"$WORKSPACE_ID\",\"name\":\"$MACHINE_NAME\"}" "$API/machines")
MACHINE_ID=$(echo "$machine_json" | jget 'd.get("data",{}).get("machine",{}).get("id","")')
MACHINE_TOKEN=$(echo "$machine_json" | jget 'd.get("data",{}).get("token","")')

if [[ -z "$MACHINE_ID" ]]; then
  # 名称已存在：复用机器并重新签发令牌。
  MACHINE_ID=$(curl -sS -H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" \
    "$API/machines?workspaceId=$WORKSPACE_ID" \
    | jget "[m['id'] for m in d['data'] if m['name']==\"$MACHINE_NAME\"][0]")
  MACHINE_TOKEN=$(curl -sS -X POST "${AUTH[@]}" "$API/machines/$MACHINE_ID/token" | jget 'd["data"]["token"]')
fi
[[ -n "$MACHINE_ID" && -n "$MACHINE_TOKEN" ]] || { echo "注册机器失败" >&2; exit 1; }
log "机器 $MACHINE_ID"

agent_json=$(curl -sS -X POST "${AUTH[@]}" -d "{
    \"workspaceId\":\"$WORKSPACE_ID\",
    \"name\":\"$AGENT_NAME\",
    \"machineId\":\"$MACHINE_ID\",
    \"workDir\":\"$WORK_DIR\",
    \"systemPrompt\":\"\",
    \"evalMode\":true
  }" "$API/agents")
AGENT_ID=$(echo "$agent_json" | jget 'd.get("data",{}).get("id","")')
if [[ -z "$AGENT_ID" ]]; then
  AGENT_ID=$(curl -sS -H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" \
    "$API/agents?workspaceId=$WORKSPACE_ID" \
    | jget "[a['id'] for a in d['data'] if a['name']==\"$AGENT_NAME\"][0]")
fi
[[ -n "$AGENT_ID" ]] || { echo "注册 Agent 失败：$(echo "$agent_json" | head -c 400)" >&2; exit 1; }
log "Agent $AGENT_ID（eval_mode=1）"

# ---------- 3. 起容器 ----------
cleanup() {
  if [[ "$KEEP" -eq 1 ]]; then
    log "保留容器 $CONTAINER（--keep）"
    return
  fi
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

log "启动容器 $CONTAINER"
docker run -d --name "$CONTAINER" \
  --platform "$PLATFORM" \
  --add-host host.docker.internal:host-gateway \
  -e "ALADDIN_MACHINE_ID=$MACHINE_ID" \
  -e "ALADDIN_MACHINE_TOKEN=$MACHINE_TOKEN" \
  -e "ALADDIN_API_URL=http://host.docker.internal:${API##*:}" \
  -e "ALADDIN_ACP_URL=ws://host.docker.internal:${API##*:}/v1/acp" \
  "aladdin-eval:$(echo "$INSTANCE" | tr -cd 'a-z0-9-')" >/dev/null

# 等机器上线：daemon 连不上控制面时建 Issue 会直接派发失败。
log "等待机器上线"
for _ in $(seq 1 60); do
  status=$(curl -sS -H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" \
    "$API/machines?workspaceId=$WORKSPACE_ID" \
    | jget "[m['status'] for m in d['data'] if m['id']==\"$MACHINE_ID\"][0]")
  [[ "$status" == "online" ]] && break
  sleep 2
done
[[ "${status:-}" == "online" ]] || { echo "机器未上线，容器日志：" >&2; docker logs --tail 40 "$CONTAINER" >&2; exit 1; }

# ---------- 4. 建 Issue（创建即执行） ----------
log "创建 Issue：$ISSUE_NAME"
issue_body=$(python3 - "$ISSUE_NAME" "$AGENT_ID" "$INSTRUCTION" <<'PY'
import json, sys
name, agent_id, path = sys.argv[1], sys.argv[2], sys.argv[3]
requirement = open(path, encoding='utf-8').read()
print(json.dumps({
    "name": name,
    "agentIds": [agent_id],
    "plannerAgentId": agent_id,
    "repositories": [],
    "requirementContent": requirement,
}, ensure_ascii=False))
PY
)
ISSUE_ID=$(curl -sS -X POST "${AUTH[@]}" --data-binary "$issue_body" \
  "$API/workspaces/$WORKSPACE_ID/issues" | jget 'd["data"]["id"]')
[[ -n "$ISSUE_ID" ]] || { echo "创建 Issue 失败" >&2; exit 1; }
log "Issue $ISSUE_ID 已创建，等待执行"

# ---------- 5. 等这一轮结束 ----------
deadline=$(( $(date +%s) + TIMEOUT ))
run_status=""
while [[ $(date +%s) -lt $deadline ]]; do
  run_status=$(curl -sS -H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" \
    "$API/issues/$ISSUE_ID/runs" | jget '(d["data"][0]["status"] if d.get("data") else "")')
  case "$run_status" in
    completed|failed|cancelled) break ;;
  esac
  sleep 5
done
[[ -n "$run_status" ]] || run_status="timeout"
log "本轮状态：$run_status"

# ---------- 6. 导出 patch 与证据 ----------
log "导出交付补丁"
{
  docker exec "$CONTAINER" bash -lc "cd '$WORK_DIR' && git add --intent-to-add -- . ':!CLAUDE.md'"
  docker exec "$CONTAINER" bash -lc "cd '$WORK_DIR' && git diff --binary '$BASE_COMMIT' -- . ':!CLAUDE.md'"
} > "$JOB_DIR/agent-patch.diff"

curl -sS -H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" \
  "$API/issues/$ISSUE_ID/events?after=0" > "$JOB_DIR/issue-events.json"
curl -sS -H "Authorization: Bearer $ALADDIN_ADMIN_TOKEN" \
  "$API/issues/$ISSUE_ID/runs" > "$JOB_DIR/runs.json"
docker logs "$CONTAINER" > "$JOB_DIR/daemon.log" 2>&1 || true

cat > "$JOB_DIR/meta.json" <<META
{
  "job": "$JOB_NAME",
  "instance": "$INSTANCE",
  "image": "$IMAGE",
  "baseCommit": "$BASE_COMMIT",
  "controlPlane": "$API",
  "workspaceId": "$WORKSPACE_ID",
  "machineId": "$MACHINE_ID",
  "agentId": "$AGENT_ID",
  "issueId": "$ISSUE_ID",
  "runStatus": "$run_status",
  "patchBytes": $(wc -c < "$JOB_DIR/agent-patch.diff"),
  "finishedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
META
cp "$INSTRUCTION" "$JOB_DIR/instruction.md"

log "完成：$JOB_DIR"
log "补丁 $(wc -c < "$JOB_DIR/agent-patch.diff") 字节，状态 $run_status"
log "判分请用外部 harness：./evals/swebench/verify.sh --job $JOB_NAME"
