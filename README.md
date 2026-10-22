# OpenVow

> 私有工程的任务中台：人定契约，终端干活，平台只做派发、校验、归集、合并。

| | |
| --- | --- |
| 状态 | M0 开发中（`0.1.0-dev`） |
| 设计文档 | [docs/design](docs/design/README.md)（18 册，v1.2） |
| 路线图 | M0 → M4，见 [13-路线图与实现基线](docs/design/13-路线图与实现基线.md) |
| 未决问题 | [15-设计问题](docs/design/15-设计问题.md)（34 问，7 个没答案） |

## 它解决什么

三个真实痛点：大需求只能串行做、并行产出合不起来、多账号调用被集中管控。

对应的三个机制：**契约先冻结**（接口与测试在动手前定死）、**判卷的不是考生**（测试由独立任务产出、平台冻结执行）、**合并是任务**（集成失败会生成修复任务，而不是一句"集成失败"）。

## 快速开始

```bash
go run ./cmd/openvow serve --db ./openvow.db --repo . --addr 127.0.0.1:7711
curl http://127.0.0.1:7711/healthz
```

```
{"status":"ok","version":"0.1.0-dev","repo":"…","tasks":0}
```

## 目录

```
cmd/openvow/        入口（serve / version）
internal/store/     SQLite：迁移、查询、事务
internal/api/       HTTP 端点
docs/design/        设计文档集（唯一真源）
docs/               历史工作稿与架构图
tools/              文档构建脚本
```

## 进度（M0）

- [x] 仓库骨架、SQLite 六张表迁移、`/healthz`
- [x] `contract` 包：路径规范化、glob 匹配、冻结比对（含 fuzz 回归语料）
- [x] 提交流水线六步：幂等 → 租约 → L0/L1 → 落盘 → L3 → 落库
- [ ] HTTP 端点 + token 中间件
- [ ] CLI（task new / claim / submit）
- [ ] 验收脚本（五条标准）

逐行细节见 [M0 施工清单](docs/design/M0-施工清单.md)。

## 开发

需要 Go 1.22+（当前环境用 1.27.3）。模块代理走 `goproxy.cn`：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
go test ./...
go vet ./...
```

## 安全边界

**M0 在宿主机上执行终端提交的验收命令**，等于执行陌生代码。只在你信任的仓库与终端上运行，不要部署到共享环境。容器隔离是 M3 的事，见 [11-安全与审计](docs/design/11-安全与审计.md) §12.2。

## 现在该做什么

设计层面已收敛，缺的是证据。下一步不是再改文档，而是：

1. 写完 M0（`contract` → 流水线 → 端点 → CLI）；
2. 用一个**真实仓库**跑实验 A，拿到 P（并行度）与 R（可验收率）；
3. 把数字回写进 [18-实验记录](docs/design/18-实验记录.md)，再决定进 M1 还是收窄定位。
