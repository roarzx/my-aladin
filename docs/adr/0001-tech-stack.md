# ADR-0001：Go + MySQL + Redis + React

## 背景

需要单机可运行、可验证的多 Agent 编排系统；执行面是长驻进程，控制面是有状态 API。

## 决策

- 控制面/执行面/CLI 用一个 Go module 与三个入口（`cmd/server`、`cmd/daemon`、`cmd/aladdin`）。
- 存储：MySQL 8.0 保存可重建冷数据；Redis 7 只保存可推导热状态。
- 前端：React 18 + TypeScript + Vite（独立 workspace）。
- 不引入消息队列与工作流引擎：任务推进由 Runner + 任务锁 + Watchdog 完成。

## 后果

- 部署简单、测试可在 Docker 内闭环；并发扩展需要多副本与更细的锁/队列设计。
- Redis 丢失后必须按 9.4 重建，锁重新竞争，租约过期进入对账。
