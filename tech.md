# Shareport 技术说明（当前逻辑结构）

本文记录当前版本的核心模块划分、数据流与运行时行为，便于后续维护与排障。

## 1. 模块与目录

- `main.go`：程序入口；解析参数；在普通交互模式与 `--balancer-daemon` 后台守护模式之间切换。
- `cli/`：交互式 TUI 菜单、初始化向导、运行管理（启动/停止代理、启动/停止定时切换）。
- `config/`：SQLite 配置与运行态存储（pools/links/settings/runtime_state）。
- `components/`：ACME 证书、链接解析、调度池（round_robin/random/least_conn）与元数据。
- `core/`：将后端链接（vless/vmess/trojan）解析为代理 `outbounds`，并生成包含 API + routing 的代理配置 JSON。
- `runtime/`：运行时进程管理（代理 PID、balancer daemon PID）、以及通过代理 API 覆盖 balancer 出站的实现。
- `.shareport/`：默认运行目录（DB、生成的代理配置、日志等）。

## 2. 数据与持久化

默认 DB：`.shareport/shareport.db`（可通过 `--db` 指定）。

### 2.1 SQLite 表

由 `config/` 负责 schema 与读写，核心表：

- `pools`：池定义（name、policy）。
- `links`：每个 pool 的后端链接列表（按 position 排序）。
- `settings`：一些 UI/生成器配置（例如对外 public_host）。
- `runtime_state`：运行态（例如 `proxy_pid`、`balancer_daemon_pid` 等）。

### 2.2 运行态 key（runtime_state）

当前用到的关键 key：

- `proxy_pid`：代理进程 PID（`runtime/xray.go`）。
- `balancer_daemon_pid` / `balancer_daemon_running`：定时切换守护进程 PID 与标记（`runtime/balancer_daemon.go`）。

## 3. 代理配置生成（生成器）

入口：`cli/wizard.go` 的 `RunXraySetup(...)`。

关键步骤：

1. 从默认 pool（`cfg.DefaultPool`，为空则取第一个 pool）收集 links。
2. 交互式选择入站组合（协议/端口/SNI/Reality 等）。
3. `core.BuildOutboundsFromLinks(links)`：将 links（vless/vmess/trojan）解析为 `outbounds`，并按顺序打 tag：`node-1`、`node-2`…（`core/outbounds.go`）。
4. `core.BuildXrayConfig(...)`：生成代理 JSON，包含：
   - API：`RoutingService` + 127.0.0.1:10085 的 dokodemo-door inbound（`tag: api`）。
   - routing：
     - 一个 balancer：`tag: balancer-0`，`selector: ["node-1","node-2",...]`，并设置 `strategy: roundRobin` 作为兜底策略。
     - rules：
       - 业务入站（例如 `inbound-0`）全部走 `balancerTag: balancer-0`。
       - `api` 入站走 `outboundTag: api`。

生成输出默认写入：`.shareport/config.json`（可通过 `--xray-config` 指定）。

## 4. “routing 由均衡器完全接管”的实现

目标：业务流量不直接"固定走某个 outboundTag"，而是始终通过 `balancer-0`；并且由 shareport 在运行时通过代理 API 去"指定 balancer 当前应使用的出站 tag"。

### 4.1 API 覆盖机制（代理 balancer override）

使用命令：`xray api bo --server 127.0.0.1:10085 -b balancer-0 <outboundTag>`。

封装位置：`runtime/balancer.go`

- `XrayAPIAddr = 127.0.0.1:10085`
- `XrayBalancerTag = balancer-0`
- `DefaultOutboundTag = node-1`
- `SetXrayBalancerOverrideWithRetry(...)`：对 `bo` 做短时间重试（应对 API 刚启动时不可用）。
- `EnsureDefaultOutbound(...)`：在“未启动定时切换”场景下，强制把 `balancer-0` 指向 `node-1`。

### 4.2 默认行为（定时切换未启动时使用 node-1）

为了避免代理自己的 `strategy: roundRobin` 在未启用定时切换时仍然分流：

- 启动代理后立即调用 `EnsureDefaultOutbound()`，强制默认 `node-1`（`cli/balancer.go`）。
- 进入“均衡器管理”且检测到定时切换未运行时，也会再次 `EnsureDefaultOutbound()` 做兜底（`cli/balancer.go`）。
- 停止定时切换后，会把出站复位到 `node-1`（`cli/balancer.go`）。

## 5. 定时切换（balancer daemon）

### 5.1 启动/停止方式

TUI 菜单入口：运行管理 → 均衡器管理（`cli/balancer.go`）。

- “启动定时切换”：调用 `runtime.StartBalancerDaemon(...)`。
- “停止定时切换”：调用 `runtime.StopBalancerDaemon(...)`，并可选择复位到 `node-1`。

### 5.2 进程模型

`StartBalancerDaemon(...)` 会启动一个“分离的子进程”执行：

`shareport --balancer-daemon --db <dbPath> --xray-bin <xrayBin>`

关键点：

- 父进程会 `Wait()` 回收子进程，避免出现僵尸进程导致“看似在运行但实际不切换”。
- `IsBalancerDaemonRunning(...)` 在 Linux 下会额外检测 `/proc/<pid>/stat` 的 `Z`（僵尸态），避免误判。

### 5.3 切换逻辑

入口：`runtime.RunBalancerDaemon(...)`

行为：

1. 启动时写入 `runtime_state`：`balancer_daemon_pid` / `balancer_daemon_running`。
2. 从 DB 加载 config，构建 pools，并选择默认 pool。
3. 从 DB 加载切换策略（off/interval/per_connection，interval 支持 fixed/random）。
4. 立即执行一次切换（让“启动定时切换”有立刻可见的效果）。
5. interval 模式：使用 `time.Timer` 触发下一次切换，间隔由 fixed/random 决定。
6. per_connection 模式：tail 代理访问日志（`runtime.log`），检测新连接事件后触发切换。
7. 监听 SIGHUP 重新加载配置与切换策略；SIGTERM/SIGINT 退出时清理 runtime_state。

注意：代理日志里 `[inbound-0 -> node-X]` 只会对"新建连接"体现；已存在的长连接不会突然跳转。
## 6. 日志

### 6.1 代理日志

代理日志写入：

- `.shareport/runtime.log`（或跟随 `--db` 的目录：`<dbDir>/runtime.log`）

实现：`runtime/xray.go` 中 `StartProxy(...)` 将 `cmd.Stdout/cmd.Stderr` 重定向到该文件。

### 6.2 定时切换守护日志

定时切换守护进程输出到：

- `.shareport/balancer-daemon.log`（或跟随 `--db` 的目录：`<dbDir>/balancer-daemon.log`）

实现：`runtime/balancer_daemon.go` 中 `RunBalancerDaemon(...)` 设置 `log.SetOutput(...)`，并在每次成功切换后记录 `switched outbound to node-X`。
