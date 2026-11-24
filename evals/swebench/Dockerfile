# 题目容器：在官方 SWE-bench 镜像上加「执行面」。
#
#   docker build -f evals/swebench/Dockerfile --platform linux/amd64 \
#     --build-arg BASE_IMAGE=<swebench 镜像> \
#     --build-arg BASE_COMMIT=<base commit> \
#     -t aladdin-eval:<instance> .
#
# 构建上下文是仓库根目录。镜像里不包含任何判分材料
#（hidden test patch / PASS_TO_PASS / 参考解）。

ARG BASE_IMAGE=swebench/sweb.eval.x86_64.sympy_1776_sympy-20590:latest
FROM ${BASE_IMAGE}

ARG NODE_VERSION=20.18.1
ARG AGENT_VERSION=0.101.0
ARG BASE_COMMIT=""
ARG DEBIAN_FRONTEND=noninteractive

# 与题目 environment/Dockerfile 对齐：conda testbed 环境优先。
ENV PATH="/opt/miniconda3/envs/testbed/bin:/opt/miniconda3/bin:/usr/local/bin:${PATH}"

# 仓库固定到 base commit，保证 git diff 的基线就是判分基线。
RUN if [ -n "$BASE_COMMIT" ]; then \
      cd /testbed \
      && git checkout --detach "$BASE_COMMIT" \
      && git reset --hard "$BASE_COMMIT" \
      && git for-each-ref --format='delete %(refname)' | git update-ref --stdin \
      && git reflog expire --expire=now --all \
      && git gc --prune=now; \
    fi \
 && echo "testbed HEAD: $(git -C /testbed rev-parse HEAD)"

# ACP Agent 的运行时（Node 20；官方二进制兼容 glibc 2.17+）。
RUN curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
      | tar -xJ -C /usr/local --strip-components=1 \
 && node --version

# 平台执行面：daemon + aladdin CLI（同一个二进制）。
COPY evals/swebench/.build/aladdin /usr/local/bin/aladdin
RUN chmod +x /usr/local/bin/aladdin

# ACP Agent：必须带 optional 依赖安装，否则缺 linux-x64 的 Claude Code 原生二进制。
RUN npm install -g "@agentclientprotocol/claude-agent-acp@${AGENT_VERSION}" \
 && claude-agent-acp --version

# 非 root 运行用户（题目的 environment 约定 uid 1001）。
RUN if ! id -u agent >/dev/null 2>&1; then useradd -u 1001 -m -s /bin/bash agent; fi \
 && mkdir -p /var/lib/aladdin \
 && chown -R agent:agent /var/lib/aladdin /testbed \
 && su agent -c 'git config --global --add safe.directory /testbed'

COPY evals/swebench/daemon-entrypoint.sh /usr/local/bin/daemon-entrypoint.sh
RUN chmod +x /usr/local/bin/daemon-entrypoint.sh

USER agent
ENV HOME=/home/agent
WORKDIR /testbed
ENTRYPOINT ["/usr/local/bin/daemon-entrypoint.sh"]
