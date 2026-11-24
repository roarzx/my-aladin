#!/usr/bin/env bash
# 判分入口：把交付补丁放进**全新的干净容器**里按官方规则判分。
#
#   ./evals/swebench/verify.sh --job sympy-20590-local \
#     --task /path/to/swebench/tasks/sympy__sympy-20590
#
# --task 是 Harbor/官方格式的任务目录，需要包含：
#   tests/Dockerfile   （把测试与解析器 COPY 进 /tests）
#   tests/verify.py    （读 /tmp/multica-submission.patch 并输出 /logs/verifier/verification.json）
#
# 没有 --task 时只打印补丁位置与六步配方，不假装判分。
set -euo pipefail

JOB=""
TASK_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --job) JOB="$2"; shift 2 ;;
    --task) TASK_DIR="$2"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

[[ -n "$JOB" ]] || { echo "需要 --job <jobs 下的目录名>" >&2; exit 1; }
JOB_DIR="evals/jobs/$JOB"
PATCH="$JOB_DIR/agent-patch.diff"
[[ -f "$PATCH" ]] || { echo "找不到补丁：$PATCH" >&2; exit 1; }

if [[ -n "$TASK_DIR" && -f "$TASK_DIR/tests/Dockerfile" && -f "$TASK_DIR/tests/verify.py" ]]; then
  IMAGE="swebench-verify:$(echo "$JOB" | tr -cd 'a-z0-9-')"
  echo "==> 构建验收镜像 $IMAGE"
  docker build -f "$TASK_DIR/tests/Dockerfile" --platform linux/amd64 \
    -t "$IMAGE" "$TASK_DIR/tests" >/dev/null

  mkdir -p "$JOB_DIR/verifier"
  echo "==> 在干净容器里判分（no-network）"
  docker run --rm --platform linux/amd64 --network none \
    -v "$(cd "$JOB_DIR" && pwd)/agent-patch.diff:/tmp/multica-submission.patch:ro" \
    -v "$(cd "$JOB_DIR" && pwd)/verifier:/logs/verifier" \
    "$IMAGE" python3 /tests/verify.py || true

  RESULT="$JOB_DIR/verifier/verification.json"
  if [[ -f "$RESULT" ]]; then
    python3 - "$RESULT" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
print(f"reward={data.get('reward')} resolved={data.get('resolved')}")
for key in ("FAIL_TO_PASS", "PASS_TO_PASS"):
    block = data.get("tests_status", {}).get(key, {})
    print(f"  {key}: 通过 {len(block.get('success', []))} / 失败 {len(block.get('failure', []))}")
PY
  fi
  exit 0
fi

cat <<EOF
没有提供可用的 Harbor 任务定义（--task），补丁已就绪：

  补丁：$PATCH（$(wc -c < "$PATCH") 字节）
  事件：$JOB_DIR/issue-events.json
  轮次：$JOB_DIR/runs.json

请在**全新的干净容器**里按这六步判分（任何一步偷懒都会让结果不可信）：

  1. 从同一个 base 镜像起容器，确认 HEAD == base_commit
  2. git apply --binary agent-patch.diff          # 失败即 0 分
  3. git checkout <base_commit> -- <测试文件>      # 丢弃 Agent 对测试的任何改动
  4. 应用官方 hidden test patch（注入 FAIL_TO_PASS）
  5. 跑官方命令（例如 bin/test -C --verbose sympy/core/tests/test_basic.py）
  6. 用 swebench 4.0.3 的解析器比对 FAIL_TO_PASS / PASS_TO_PASS，全过才算 resolved
EOF
