<div align="center">

[English](README.md) · **简体中文**

<h1 align="center">
  <img src="./logo.png" alt="gui-for-zlib" width="200">
  <br>GUI for zlib<br>
</h1>

Z-Library 桌面客户端 —— 基于 [heartleo/zlib](https://github.com/heartleo/zlib) 的 Go 原生 GUI 实现。

</div>

---

## 简介

这是一个 Z-Library 的桌面客户端。核心逻辑基于 [heartleo/zlib](https://github.com/heartleo/zlib)，使用 [mygo](https://github.com/egoist/mygo) 构建原生 UI。

- **跨平台**：Windows、macOS、Linux。
- **启动快、内存小**：无 WebView2 / Electron 依赖。
- **与账户同步**：登录、收藏、历史、下载配额均与你的 Z-Library 账户同步。

## 功能特性

- **登录与会话**：邮箱 + 密码登录；会话持久化到本地，重启自动恢复；支持 Netscape 格式 Cookie 导入。
- **搜索**：关键词搜索 + 格式过滤（EPUB / PDF / MOBI / AZW3 等 11 种）。
- **书单**：推荐、最热、收藏（与账户双向同步）、历史。
- **书籍详情**：封面、简介（Markdown 渲染）、格式徽章。
- **下载引擎**：实时下载进度，可随时取消；502/503/504 自动重试、2 分钟无进度超时、文件名清洗（防路径穿越）。
- **下载完成通知**：系统通知；侧边栏「下载」图标实时显示进行中的任务数。
- **账户页**：头像、邮箱、每日下载配额 / 今日已用 / 剩余。
- **域名检测（doctor）**：并发探测全部白名单镜像，逐条显示可用性与延迟，一键切换可用镜像。
- **网络代理**：SOCKS5 / HTTP 代理全局转发（搜索、封面、下载、域名检测、登录预检），配置持久化。

## 快速开始

前置要求：Go 1.27+（`go.mod` 声明 `go 1.27.3`），Windows 上使用 Direct3D 11 无需额外运行时。

```sh
# 运行
go run .

# 构建
go build ./...

# 测试（core 单测用 httptest 模拟上游，不依赖网络）
go test ./...

# mygo 专项静态检查
go tool mygo vet .
```

首次启动时注册 `zlib://` 深链协议（`go run .` 下也生效）。登录信息与设置存放在
`~/.config/zlib/`（可用 `ZLIB_CONFIG_DIR` 重定向）。

## 打包发布

`mygo.json` 已配置产物名、图标与 URL scheme，直接：

```sh
go tool mygo build                          # 当前平台
go tool mygo build -platform windows/amd64,darwin/universal,linux/amd64
```

产物：

- Windows：`build/windows-amd64/` 下的可执行文件 + NSIS 安装包（含开始菜单快捷方式与卸载项）
- macOS：`.app` + `.dmg`（连同样式资源 `resources/app_icon.icon`）
- Linux：可执行文件、`.desktop`、`.deb`、`.tar.gz`

版本号从 `mygo.json` 的 `version` 注入，应用内「关于」与更新检查读取同一来源。

## 配置

### 环境变量

| 变量 | 说明 |
| --- | --- |
| `ZLIB_DOMAIN` | 指定镜像域名（白名单校验），优先级高于内置默认值 |
| `ZLIB_PROXY` | 全局代理 URL（如 `socks5://127.0.0.1:7890`），GUI 设置页覆盖 |
| `ZLIB_DOWNLOAD_DIR` | 默认下载目录 |
| `ZLIB_DOWNLOAD_RETRIES` | 下载失败重试次数 |
| `ZLIB_THEME` | 指定内置主题名（优先级高于 config 的 `gui.theme`） |
| `ZLIB_CONFIG_DIR` | 配置 / 会话目录，默认 `~/.config/zlib` |

启动时自动加载 `~/.config/zlib/.env`。

### 本地文件（`~/.config/zlib/`）

| 文件 | 内容 |
| --- | --- |
| `session.json` | 登录后的 Cookie 会话，与 CLI / Agent 插件共享 |
| `config.json` | 应用配置：`gui` 块（下载目录、默认格式、每页数量、代理）+ 上次登录邮箱 |
| `.env` | 环境变量持久化（如切换后的镜像域名） |

## 桌面集成

| 能力 | 行为 |
| --- | --- |
| `zlib://` 深链 | 仅接受 `zlib://book/{id}/{hash}`，解析后打开书籍详情 |
| 单实例锁 | 二次启动将参数转发给已运行实例并置前，不重复运行 |
| 系统通知 | 下载完成时发送，点击跳转下载页 |

## 项目结构

```
zlib-gui/
├── main.go                 # 装配：单实例、深链、通知、窗口与事件循环
├── mygo.json               # mygo 打包配置（name/identifier/version/urlSchemes）
├── go.mod / go.sum         # 固定 mygo v0.8.3
├── core/                   # 领域层（零 UI 依赖）
│   ├── client.go           #   HTTP 会话、Cookie、重定向剥凭据
│   ├── eapi*.go            #   EAPI 协议：登录/搜索/详情/书单/配额
│   ├── domain_policy.go    #   域名白名单
│   ├── domain_probe.go     #   /eapi/info/domains 健康探测状态机
│   ├── download.go         #   下载、重试、超时、文件名清洗
│   ├── store.go            #   session.json / config.json / .env 持久化
│   ├── cookies.go          #   Netscape Cookie 导入校验
│   ├── backend.go          #   组装 Service 实现
│   └── demo.go             #   测试用演示后端
├── app/                    # 界面层（mygo/ui）
│   ├── app.go              #   App 状态、View 入口、侧边栏、主题应用
│   ├── home.go             #   首页 / 账户页
│   ├── search.go           #   搜索与格式过滤
│   ├── library.go          #   推荐 / 最热 / 收藏 / 历史
│   ├── detail.go           #   书籍详情 Modal
│   ├── downloads.go        #   下载队列与进度
│   ├── doctor.go           #   域名检测
│   ├── settings.go         #   设置 / 代理 / 关于 / 更新
│   ├── login.go            #   登录 Modal + Cookie 导入
│   ├── desktop.go          #   深链解析、单实例置前
│   ├── theme.go            #   内置主题 palette → ui.Theme
│   └── updates.go          #   版本检查
└── resources/              # 应用图标
```

## 测试

- **core 单测**：依赖 `httptest` 模拟上游，覆盖 EAPI 映射与错误信封、重定向剥凭据、
  下载 / 重试 / 取消、文件名路径穿越、域名探测状态机、会话 / `.env` 往返、
  Cookie 过滤、配置 `gui` 块读写。
- **view 测试**：`ui.NewTester` 无窗口渲染，如同用户般点击 / 输入 / 断言语义文本，
  覆盖登录校验、搜索、推荐 / 最热加载、设置持久化、深链解析、下载完成通知等。
- **冒烟测试**：`main_test.go` 用真实 `core.NewBackend()` 构造并渲染，验证无网络、
  无会话时应用也能正常启动。

## 鸣谢

- [heartleo/zlib](https://github.com/heartleo/zlib) —— Z-Library CLI
- [mygo](https://github.com/egoist/mygo) —— Go 原生桌面 UI

## 许可证

本项目基于 [MIT License](LICENSE) 发布，第三方声明见 [NOTICE](NOTICE)。

## 免责声明

本项目仅用于学习与个人合法使用。请勿用于任何侵犯版权、违法的用途；
下载内容的使用方式由使用者自行负责。
