package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edidada/test_go_wails/internal/bridge"
	"github.com/edidada/test_go_wails/internal/service"
)

type callResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bridge.NewRouter(logger)
	Register(router, service.New("test_go_wails", "test"))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func call(t *testing.T, server *httptest.Server, method string, params any) (int, callResponse) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"method": method, "params": params})
	if err != nil {
		t.Fatalf("序列化请求失败: %v", err)
	}
	resp, err := http.Post(server.URL+"/api/call", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/call error = %v", err)
	}
	defer resp.Body.Close()

	var payload callResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	return resp.StatusCode, payload
}

func TestRegisterAllMethods(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := bridge.NewRouter(logger)
	Register(router, service.New("test_go_wails", "test"))

	want := []string{"app.info", "counter.add", "counter.get", "counter.reset", "greeter.greet"}
	got := router.Methods()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("methods = %v, 期望 %v", got, want)
	}
}

func TestGreet(t *testing.T) {
	server := newServer(t)

	status, payload := call(t, server, "greeter.greet", map[string]string{"name": "Wails"})
	if status != http.StatusOK || !payload.OK {
		t.Fatalf("status = %d, payload = %+v", status, payload)
	}
	var greeting service.Greeting
	if err := json.Unmarshal(payload.Data, &greeting); err != nil {
		t.Fatalf("解析 greeting 失败: %v", err)
	}
	if !strings.Contains(greeting.Message, "Wails") {
		t.Fatalf("message = %q", greeting.Message)
	}

	status, payload = call(t, server, "greeter.greet", map[string]string{"name": "  "})
	if status != http.StatusBadRequest {
		t.Fatalf("空名字 status = %d, 期望 400", status)
	}
	if payload.Error == nil || payload.Error.Code != string(bridge.CodeInvalidParams) {
		t.Fatalf("空名字 error = %+v", payload.Error)
	}

	status, payload = call(t, server, "greeter.greet", map[string]string{
		"name": strings.Repeat("界", service.MaxNameRunes+1),
	})
	if status != http.StatusBadRequest || payload.Error == nil {
		t.Fatalf("超长名字 status = %d, error = %+v", status, payload.Error)
	}
}

func TestCounterLifecycle(t *testing.T) {
	server := newServer(t)

	status, payload := call(t, server, "counter.add", map[string]int64{"delta": 7})
	if status != http.StatusOK || !payload.OK {
		t.Fatalf("counter.add status = %d, payload = %+v", status, payload)
	}

	status, payload = call(t, server, "counter.get", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("counter.get status = %d", status)
	}
	var state service.CounterState
	if err := json.Unmarshal(payload.Data, &state); err != nil {
		t.Fatalf("解析 counter 失败: %v", err)
	}
	if state.Value != 7 {
		t.Fatalf("counter = %d, 期望 7", state.Value)
	}

	status, payload = call(t, server, "counter.reset", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("counter.reset status = %d", status)
	}
	if err := json.Unmarshal(payload.Data, &state); err != nil {
		t.Fatalf("解析 counter 失败: %v", err)
	}
	if state.Value != 0 {
		t.Fatalf("reset 后 counter = %d, 期望 0", state.Value)
	}

	status, payload = call(t, server, "counter.add", map[string]int64{"delta": service.MaxCounterDelta + 1})
	if status != http.StatusBadRequest || payload.Error == nil {
		t.Fatalf("越界 delta status = %d, error = %+v", status, payload.Error)
	}
}

func TestAppInfo(t *testing.T) {
	server := newServer(t)

	status, payload := call(t, server, "app.info", map[string]any{})
	if status != http.StatusOK || !payload.OK {
		t.Fatalf("status = %d, payload = %+v", status, payload)
	}
	var info service.Info
	if err := json.Unmarshal(payload.Data, &info); err != nil {
		t.Fatalf("解析 info 失败: %v", err)
	}
	if info.Name != "test_go_wails" || info.Version != "test" {
		t.Fatalf("info = %+v", info)
	}
}
