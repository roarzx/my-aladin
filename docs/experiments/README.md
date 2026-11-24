# 阿拉丁 Agent 专项实验

> 入口：先读 [`阿拉丁_Agent专项实验与优化实施手册.md`](./阿拉丁_Agent专项实验与优化实施手册.md)（v1.0）。
> 纪律：先证据后结论；未验证写「未确认」；缺失数据不补零；实验环境结果不写成线上改进。

## 目录

| 文件 | 内容 | 状态 |
|---|---|---|
| `inventory.md` | P0 源码与观测能力审计（调用链、数据来源、未接线清单、候选实验） | 已完成（`b121c4e`） |
| `questions.md` | 假设登记与优先级打分 | 已完成 |
| `experiment-001.md` | EX-001 执行权与迟到结果：预注册 + 结果 | 已完成 |
| `reports/baseline.md` | 可靠性基线报告 | 已完成 |
| `evidence/ex-001/` | Baseline/Treatment 日志与结构化结果 | 已完成 |

## 运行方法

```bash
# 依赖
make up && make migrate

# EX-001 故障注入（零模型成本，约 1 秒）
TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4' \
TEST_REDIS_ADDR='127.0.0.1:6379' \
  go test -tags=e2e ./tests/e2e/... -run TestRecovery -count=1 -v

# 全量回归
go test ./... -count=1
make test-e2e
make test-integration
```

## 结论速览（EX-001）

- 现状：过期/重复的 `turn_done` / `turn_fail` 会污染活动流、用量与 ACP 会话游标（Baseline 违规 7 处）。
- 修复：回执处理以条件更新的返回值作为围栏（`internal/issue/service.go:245-257`、`:301-311`）。
- 结果：5/5 Case 通过，主指标违规数 0，回归全绿。
- 未覆盖：外部副作用（push/PR）幂等、心跳租约与 epoch 未接线、用量缺失记 0。

## 下一步候选

1. **EX-2 `completed` 与真交付绑定**（见 `questions.md` 打分第二）。
2. 修复 `inventory.md` §7 的 D-1（主路径 push/PR 断链）后再考虑 Git 合入实验。
3. 接线 `hub.Lease` / `ResendLoop` 后，把 EX-001 扩展到心跳与丢帧场景。
