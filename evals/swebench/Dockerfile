# 题目容器：在官方 SWE-bench 镜像上加「执行面」。
#
# 构建上下文是仓库根目录（需要先跑 run-instance.sh 把 aladdin 编到
# evals/swebench/.build/）：
#
#   docker build -f evals/swebench/Dockerfile \
#     --platform linux/amd64 \
#     -t aladdin-eval:sympy-20590 .
#
# 镜像里不包含任何判分材料（hidden test patch / PASS_TO_PASS 列表 / 参考解）。

ARG BASE_IMAGE=swebench/sweb.eval.x86_64.sympy_1776_sympy-20590:latest
FROM ${BASE_IMAGE}

ARG AGENT_NPM_PKG=@agentclientprotocol/claude-agent-acp
ARG DEBIAN_FRONTEND=noninteractive

# 1) 平台执行面：daemon + aladdin CLI（同一个二进制）。
COPY evals/swebench/.build/aladdin /usr/local/bin/aladdin
RUN chmod +x /usr/local/bin/aladdin

# 2) ACP Agent（默认 claude-agent-acp；换成你自己的适配器改这个 ARG）。
RUN if ! command -v npm >/dev/null 2>&1; then \
      apt-get update && apt-get install -y --no-install-recommends nodejs npm \
      && rm -rf /var/lib/apt/lists/*; \
    fi \
 && npm install -g "${AGENT_NPM_PKG}"

# 3) 非 root 运行用户；工作目录归它所有。
RUN if ! id -u agent >/dev/null 2>&1; then useradd -m -s /bin/bash agent; fi \
 && mkdir -p /var/lib/aladdin \
 && chown -R agent:agent /var/lib/aladdin

# SWE-bench 镜像里 /testbed 属于 root，agent 需要可写；
# 同时告诉 git 这是安全目录，避免 dubious ownership 报错。
RUN chown -R agent:agent /testbed \
 && su agent -c 'git config --global --add safe.directory /testbed'

COPY evals/swebench/daemon-entrypoint.sh /usr/local/bin/daemon-entrypoint.sh
RUN chmod +x /usr/local/bin/daemon-entrypoint.sh

USER agent
WORKDIR /testbed
ENTRYPOINT ["/usr/local/bin/daemon-entrypoint.sh"]
