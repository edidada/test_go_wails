# test_go_wails

[![ci](https://github.com/edidada/test_go_wails/actions/workflows/ci.yml/badge.svg)](https://github.com/edidada/test_go_wails/actions/workflows/ci.yml)

用**纯 Go、零第三方依赖**复刻 [Wails](https://github.com/wailsapp/wails) 的核心分层：Go 后端 + Web 前端 + 统一的 JSON 调用协议。

区别只在传输外壳：Wails 使用系统 WebView，本项目使用本机回环 HTTP + 系统浏览器，因此不需要 CGO、GTK/WebKit 或 Node，能在 Linux / macOS / Windows 的 GitHub Actions 上直接编译测试。

## 快速开始

要求 Go 1.24+（macOS 26 的 dyld 要求二进制包含 `LC_UUID`，Go 1.24 起默认生成）。

```bash
go run ./cmd/app
```

默认监听 `127.0.0.1:0`（随机空闲端口），启动后自动打开浏览器。常用参数：

```bash
go run ./cmd/app -addr 127.0.0.1:34115   # 固定端口
go run ./cmd/app -no-open                # 不打开浏览器
go run ./cmd/app -version                # 查看构建信息
```

## 结构

```text
cmd/app                 进程入口：参数、装配、优雅关闭、静态资源服务
internal/bridge         HTTP 传输、方法路由、协议错误、泛型适配
internal/api            方法注册与业务错误映射
internal/service        业务能力：AppInfo / Greeter / Counter
internal/webassets      内嵌前端（html/css/js，go:embed）
docs                    架构设计、概要设计
.github/workflows/ci.yml 三平台 CI
```

请求经过：浏览器 `fetch` → `POST /api/call` → `bridge.Router` → `internal/api` → `internal/service`。

## 调用协议

```bash
curl -X POST http://127.0.0.1:34115/api/call \
  -H 'Content-Type: application/json' \
  -d '{"method":"greeter.greet","params":{"name":"Wails"}}'
```

```json
{
  "ok": true,
  "data": {
    "message": "你好，Wails！这条消息来自 Go 后端。",
    "at": "2026-11-15T00:00:00Z"
  }
}
```

| 方法 | 参数 | 说明 |
| --- | --- | --- |
| `app.info` | — | 应用版本、Go 版本、OS/Arch、PID、运行时长 |
| `greeter.greet` | `{ "name": string }` | 问候，1–32 个 Unicode 字符 |
| `counter.add` | `{ "delta": number }` | 调整计数器，`\|delta\| ≤ 1,000,000` |
| `counter.reset` | — | 计数器归零 |
| `counter.get` | — | 读取计数器 |

错误统一返回 `{"ok":false,"error":{"code":"...","message":"..."}}`，错误码包括 `invalid_json`、`invalid_params`、`method_not_found`、`internal_error`。健康检查：`GET /api/health`。

## 验证

```bash
gofmt -l .
go vet ./...
go test -race ./...
go build -trimpath -o dist/test_go_wails ./cmd/app
```

## 文档

- [docs/architecture.md](docs/architecture.md)：背景、分层、协议、安全、演进路线
- [docs/high-level-design.md](docs/high-level-design.md)：模块职责、接口契约、时序、测试与 CI

## 与 Wails v3 的关系

| 本项目 | Wails v3 |
| --- | --- |
| `internal/bridge`（HTTP + JSON） | bindings / runtime |
| `internal/service` | 绑定给前端的 Go 服务 |
| `internal/webassets` + 系统浏览器 | WebView + 内嵌前端 |
| `cmd/app` | `application.New(...)` 生命周期 |

业务层与通信层解耦，后续要接入真实 Wails，只需新增一个桌面入口并把 `api.Register` 的方法挂到 Wails 服务上。

## CI

`ci.yml` 在 `main` 分支 push、PR、以及 `v*` tag 时运行：

1. `gofmt` 检查（Ubuntu）
2. `go vet ./...`
3. `go test -race ./...`
4. 三平台 `go build`
5. Ubuntu 冒烟测试：启动二进制 → 健康检查 → 调用 `greeter.greet`
6. 上传各平台二进制 artifact
