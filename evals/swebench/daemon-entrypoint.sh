#!/usr/bin/env bash
# 题目容器里的入口：把 aladdin daemon 接到控制面。
# 容器不跑控制面，只跑执行面；控制面地址通过 host.docker.internal 暴露。
set -euo pipefail

: "${ALADDIN_MACHINE_ID:?缺少 ALADDIN_MACHINE_ID}"
: "${ALADDIN_MACHINE_TOKEN:?缺少 ALADDIN_MACHINE_TOKEN}"

export ALADDIN_API_URL="${ALADDIN_API_URL:-http://host.docker.internal:8080}"
export ALADDIN_ACP_URL="${ALADDIN_ACP_URL:-ws://host.docker.internal:8080/v1/acp}"

exec aladdin daemon start \
  --machine-id "$ALADDIN_MACHINE_ID" \
  --server-auth-token "$ALADDIN_MACHINE_TOKEN" \
  --acp-url "$ALADDIN_ACP_URL" \
  --api-url "$ALADDIN_API_URL" \
  --profile-dir "${ALADDIN_PROFILE_DIR:-/var/lib/aladdin}" \
  --log-file -
