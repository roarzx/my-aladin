# D14 验收证据 — 前端驾驶舱（最小）

## 新增

| 文件 | 作用 |
|---|---|
| `web/`（Vite + React 18 + TS，pnpm workspace） | 前端工程 |
| `web/src/api/issues.ts` | Issue/Plan/Events/Retry API 客户端与手写 TS 类型 |
| `web/src/hooks/useEventStream.ts` | 带游标事件流：`seq > cursor`、`eventId` 去重、断线指数退避 |
| `web/src/pages/IssueCockpit.tsx` | 概览 / 计划 / 恢复 / 执行时间线；failed 步骤显示重试入口 |

## 服务端数据源补齐

驾驶舱需要的两个读接口（§9.6）在本批次实现：

- `GET /issues/{id}`：任务 + 最新计划摘要 + 验收摘要（v1 为 `null`）
- `GET /issues/{id}/plans/{rev}`：计划全文与步骤；`?allowMissing=true` 首次规划前返回空计划

真实 server + MySQL 验证（节选）：

```json
GET /issues/{id}
{"data":{"acceptance":null,"plan":{"id":"3fc6…","revision":1,"status":"idle","stepCount":1},
 "task":{"id":"80a4…","status":"plan_confirming","version":1,…}}}

GET /issues/{id}/plans/1
{"data":{"plan":{"revision":1,"contentMd":"# plan","version":0},
 "steps":[{"ordinal":1,"title":"s1","agentRef":"a1","status":"pending","version":0}]}}

GET /issues/{id}/plans/9?allowMissing=true → {"data":{"plan":null,"steps":[]}}
GET /issues/{id}/plans/9                  → 404
```

## 测试

```bash
pnpm --filter web test
pnpm --filter web build
```

- 4 个用例通过：状态/步骤渲染、failed 步骤重试入口、**重复 eventId 不重复渲染、
  seq 有洞（2 → 4）时仍按 `seq > cursor` 合并**、点击重试调用接口并刷新
- `tsc -b && vite build` 通过

## 已知限制（v1）

- 实时更新使用 HTTP 轮询（1s）而非 SSE/WS：服务端尚无前端广播通道；
  游标语义与断线补拉一致——重连后从本地最大 seq 继续，不丢事件、不重复渲染。
- 页面只覆盖驾驶舱最小闭环；列表页/小队/运行中心按 F、G、H 阶段展开。
