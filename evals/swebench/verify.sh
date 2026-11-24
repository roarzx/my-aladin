#!/usr/bin/env bash
# 判分入口（占位）：本仓库不实现 SWE-bench 判分，只负责把补丁交给外部 harness。
#
# 判分必须独立：不要用 Agent 自报的状态，也不要在 Agent 改动过的容器里跑测试。
# 推荐直接把 jobs 目录里的 agent-patch.diff 交给 Harbor / 官方 swebench harness。
set -euo pipefail

JOB=""
TASK_DIR=""
KEEP=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --job) JOB="$2"; shift 2 ;;
    --task) TASK_DIR="$2"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    -h|--help) sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

[[ -n "$JOB" ]] || { echo "需要 --job <jobs 下的目录名>" >&2; exit 1; }
JOB_DIR="evals/jobs/$JOB"
PATCH="$JOB_DIR/agent-patch.diff"
[[ -f "$PATCH" ]] || { echo "找不到补丁：$PATCH" >&2; exit 1; }

if [[ -n "$TASK_DIR" && -x "$(command -v harbor || true)" && -f "$TASK_DIR/tests/verify.py" ]]; then
  echo "==> 用 Harbor 任务定义判分：$TASK_DIR"
  exec harbor run \
    --agent external \
    --path "$TASK_DIR" \
    --patch "$PATCH" \
    --jobs-dir "$JOB_DIR/harbor"
fi

cat <<EOF
没有找到可用的 Harbor 任务定义，补丁已就绪：

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
