# 评测接入：SWE-bench Verified（MVP）

把阿拉丁当成「谁来干活」这一层，接到 SWE-bench 式的判分流程里。
环境、交付、判分保持外部 harness 拥有，平台只负责派活、收 patch、留证据。

## 现在能做什么

一条命令跑通一个实例的**执行链路**：

```bash
ALADDIN_ADMIN_TOKEN=devtoken \
./evals/swebench/run-instance.sh \
  --instruction /path/to/instruction.md \
  --job sympy-20590-001
```

它会：

1. 构建 linux/amd64 的 `aladdin`，再基于官方 swebench 镜像构建题目镜像；
2. 起容器（容器里只跑 daemon，通过 `host.docker.internal` 连回控制面）；
3. 注册机器与 Agent（`evalMode: true`）；
4. 建 Issue，`requirementContent` = instruction 原文 —— **创建即执行**；
5. 等这一轮结束（`completed` / `failed` / 超时）；
6. 导出交付补丁与证据到 `evals/jobs/<job>/`：

```
agent-patch.diff     git diff --binary <base_commit> -- . ':!CLAUDE.md'
issue-events.json    会话与工具调用
runs.json            每轮执行记录
daemon.log           容器内 daemon 日志
meta.json            镜像 / base commit / issue id / 补丁字节数
instruction.md       实际喂给 Agent 的原文
```

判分不在本仓库：交给 Harbor 或官方 harness，见 `verify.sh`。

## evalMode 做了什么

Agent 配置上的 `evalMode: true`（`agents.eval_mode`）会改变三件事：

| 行为 | 普通模式 | evalMode |
|---|---|---|
| 产物上传 | 把执行期间改动的文件上报，页面可下载 | **关闭**（补丁由 harness 取） |
| 提示词 | 末尾追加「平台上报（必须执行）」 | **不追加**（避免改变提示词分布） |
| 单轮超时 | 默认 30 分钟 | **4 小时** |

另外，评测 Issue 一轮结束后平台会把它落到「已阻塞（等待验收）」，
因为 Agent 没有被要求上报状态；判分结果由外部回填，平台不猜。

## 保持不变的四条

1. **测试目录是判分真相**：harness 必须 `git checkout <base> -- <测试文件>`。
   平台侧不提供任何「改 tests 也能过」的通路。
2. **`solution/`、hidden test patch、`PASS_TO_PASS` 列表不进 Agent 容器**。
   镜像构建脚本里也不要 `COPY` 任何判分材料。
3. **预算要记**：换 Agent 后模型请求数会变。当前平台只能给 token
   与工具调用次数，**看不到模型请求数**（见下节），跑批时请自行在网关侧计数。
4. **验收独立**：不要在 Agent 改过的容器里跑测试，也不要用 Agent 自报的 `completed`。

## 已知缺口（下一阶段）

- **模型请求数不可见**：请求发生在 Agent 进程内部。要在网关侧计数，
  或者让 Agent 适配器回报 usage；否则两次 run 之间不可比。
- **没有完整轨迹**：活动流只有工具调用（标题 + 命令）和截断 8KB 的最终回复，
  没有 `trajectory.jsonl` 那种逐条消息/工具输出。复现与 review 需要新功能。
- **判分结果不回传**：平台不认识 reward，还没有 `POST /api/v1/cli/verifications`。
- **批量**：一次一个 Issue；`internal/evaluation` 有 Report 骨架但没接。

## 坑

- **ARM 主机**：swebench 镜像是 amd64，走 QEMU 后测试慢 5–10 倍，超时按 5 倍留。
  `run-instance.sh` 的默认 `EVAL_TIMEOUT=5400` 是为轻量题准备的。
- **容器内无外网**：模型入口也要走 `host.docker.internal`，否则 Agent 连不上模型，
  Issue 会一直「执行中」直到平台超时。
- **git 身份**：镜像里给 `agent` 用户配了 `safe.directory /testbed`；如果换基础镜像，
  记得保留这一步，否则 `git diff` 会因 dubious ownership 失败。
- **这道题测不了能力**：`sympy__sympy-20590` 的修复在训练记忆里，模型可能直接回忆。
  它只证明「链路通」，能力测量请换低曝光题或自建题。

## 目录

```
evals/swebench/
  Dockerfile            题目镜像：官方 swebench 镜像 + aladdin + ACP Agent
  daemon-entrypoint.sh  容器入口：把 daemon 接到控制面
  run-instance.sh       一个实例的端到端执行
  verify.sh             判分入口（委托 Harbor / 打印六步配方）
  agent.example.yaml    机器 / Agent / Issue 参数
  .build/               构建产物（aladdin 二进制，已 gitignore）
```
