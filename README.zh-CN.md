# Shareport

一个功能强大的代理配置管理工具，具备负载均衡能力，提供交互式 TUI 界面、自动证书管理和灵活的后端切换策略。

## 免责声明

本项目仅供学习和个人使用，不能用于非法用途。

## 功能特性

- **基于池的链接管理**：将后端代理链接组织到不同的池中，支持多种调度策略
- **单链接流量信息**：为每条后端链接保存可选的流量额度(GB)与每月重置时间
- **多种调度策略**：支持轮询、随机和最少连接策略
- **代理配置生成器**：交互式向导，支持 VLESS/Trojan 入站的代理配置生成
- **均衡器守护进程**：支持可配置间隔（固定/随机）的定时后端切换
- **ACME 证书管理**：自动 Let's Encrypt 证书签发和续期（HTTP-01/DNS-01）
- **TUI 界面**：用户友好的终端配置管理界面
- **国际化支持**：多语言支持（中文和英文）
- **运行时管理**：轻松启动/停止代理和均衡器守护进程

说明：
- 入站（向导生成）：`vless`、`trojan`
- 后端链接/出站（pool 中）：`vless`、`vmess`、`trojan`
- 传输层：向导支持 `tcp`/`ws`/`http`/`xhttp`
- 目前只测试了 `vless + tcp + none` 和 `vless + tcp + reality`

## 安装

### 环境要求

- Go 1.25 或更高版本
- 代理二进制文件（默认为 PATH 中的 `xray`）

### 从源码构建

```bash
git clone https://github.com/aimerick/shareport.git
cd shareport
go build -o shareport
```

## Release 自动化构建（GitHub Actions）

推送形如 `v0.1.0` 的 tag 会触发 GitHub Actions 自动编译 Linux/macOS/Windows 的二进制文件，并自动上传到 GitHub Release。

```bash
git tag v0.1.0
git push origin v0.1.0
```

Release 说明会在你发布 tag release 时通过 Release Drafter 自动生成（基于 PR labels）。

## 快速开始

1. **初始化配置**

```bash
./shareport --init
```

这将启动交互式向导来设置初始配置。

2. **运行 Shareport**

```bash
./shareport
```

## 使用说明

### 命令行选项

| 选项 | 描述 |
|------|------|
| `--print-next` | 打印下一个后端链接并退出 |
| `--generate` | 交互式代理配置生成器模式 |
| `--balancer-daemon` | 运行均衡器调度守护进程（内部使用） |
| `--db <path>` | SQLite 数据库路径（默认：`.shareport/shareport.db`） |
| `--init` | 强制运行交互式初始化向导以重建配置 |
| `--xray-config <path>` | 代理配置输出路径（默认：`.shareport/config.json`） |
| `--xray-bin <path>` | 代理二进制文件路径或名称（默认：`xray`） |
| `--lang <lang>` | 语言（zh 或 en，默认：zh） |

### 环境变量

| 变量 | 描述 |
|------|------|
| `SHAREPORT_LANG` | 语言偏好（zh 或 en） |

## 配置

### 目录结构

```
.shareport/
├── shareport.db          # SQLite 数据库
├── config.json            # 生成的代理配置
├── runtime.log            # 代理日志
└── balancer-daemon.log   # 均衡器守护进程日志
```

### 池配置

一个池包含后端代理链接和调度策略：

```json
{
  "default_pool": "pool1",
  "pools": [
    {
      "name": "pool1",
      "policy": "round_robin",
      "links": [
        "vless://...",
        "vmess://...",
        "trojan://..."
      ]
    }
  ]
}
```

### 支持的策略

- `round_robin`：按顺序轮询链接
- `random`：随机选择链接
- `least_conn`：选择活动连接最少的链接

## TUI 菜单

### 主菜单

1. **查看当前配置** - 显示当前配置
2. **管理 Pools** - 添加、删除或设置默认池
3. **管理后端链接** - 向池中添加或删除链接
4. **运行管理** - 启动/停止代理和均衡器
5. **重新生成配置** - 交互式代理配置生成器
6. **证书续期** - 续期 ACME 证书
7. **退出** - 退出程序

### 运行管理

- **均衡器管理**：启动/停止均衡器和定时切换
- **更换调度策略**：更新池调度策略

## 均衡器切换策略

均衡器支持多种切换策略：

| 策略 | 描述 |
|------|------|
| 关闭 | 默认使用 node-1（不切换） |
| 时间间隔（固定） | 以固定时间间隔切换（10-1800 秒） |
| 时间间隔（随机） | 在随机时间范围内切换 |
| 每次连接 | 每次新连接时触发切换（基于代理访问日志） |

## 证书管理

Shareport 支持通过 ACME 自动签发证书：

### 支持的验证类型

- **HTTP-01**：需要 80 端口可访问
- **DNS-01**：手动 DNS TXT 记录验证

### 证书续期

使用主菜单中的"证书续期"选项来续期过期的证书。

## 开发

### 项目结构

```
shareport/
├── main.go              # 程序入口和 CLI 参数解析
├── cli/                 # TUI 界面和向导
├── components/          # ACME、链接解析、池逻辑、元数据
├── config/              # 配置和数据库处理
├── i18n/                # 国际化
├── runtime/             # 运行时进程管理
├── ui/                  # TUI 工具
└── core/                # 代理配置生成
```

### 数据库架构

- `pools` - 池定义（名称、策略）
- `links` - 每个池的后端链接（按位置排序）
- `settings` - UI/生成器配置
- `runtime_state` - 运行时状态（PID、守护进程状态）

## 许可证

[在此指定您的许可证]

## 贡献

欢迎贡献！请随时提交 Pull Request。
