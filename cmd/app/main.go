// test_go_wails 演示「Go 后端 + Web 前端」的分层结构。
//
// 与 Wails 的差异仅在传输外壳：这里把系统 WebView 替换为本机回环 HTTP，
// 因此可以在任意没有 GTK/WebKit/CGO 的环境中编译和测试。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/edidada/test_go_wails/internal/api"
	"github.com/edidada/test_go_wails/internal/bridge"
	"github.com/edidada/test_go_wails/internal/service"
	"github.com/edidada/test_go_wails/internal/webassets"
)

const appName = "test_go_wails"

// 由 -ldflags -X 注入。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("进程退出", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	addr := flag.String("addr", "127.0.0.1:0", "监听地址，例如 127.0.0.1:34115")
	noOpen := flag.Bool("no-open", false, "启动后不自动打开浏览器")
	showVersion := flag.Bool("version", false, "打印构建信息后退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s (commit %s, built %s, %s)\n", appName, version, commit, date, runtime.Version())
		return nil
	}

	svc := service.New(appName, version)
	router := bridge.NewRouter(logger)
	api.Register(router, svc)

	mux := http.NewServeMux()
	mux.Handle("/api/", router)
	mux.Handle("/", webassets.Handler())

	server := &http.Server{
		Handler:           withRequestLogging(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", *addr, err)
	}
	defer listener.Close()

	url := "http://" + listener.Addr().String() + "/"
	logger.Info("服务已启动",
		"url", url,
		"version", version,
		"commit", commit,
		"methods", router.Methods(),
	)
	if host, _, splitErr := net.SplitHostPort(listener.Addr().String()); splitErr == nil && host != "127.0.0.1" && host != "::1" {
		logger.Warn("当前监听地址不是回环地址，且未启用鉴权，请勿在不可信网络中暴露", "addr", listener.Addr().String())
	}
	if !*noOpen {
		if err := openBrowser(url); err != nil {
			logger.Info("自动打开浏览器失败，请手动访问", "url", url, "error", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		logger.Info("收到退出信号，正在优雅关闭")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务异常: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭失败: %w", err)
	}
	logger.Info("已退出")
	return nil
}

// withRequestLogging 记录每个请求的方法、路径、状态码与耗时。
func withRequestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration", time.Since(started).Round(time.Microsecond).String(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// openBrowser 用系统默认浏览器打开 url。失败不影响服务本身。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
