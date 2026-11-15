package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type addParams struct {
	A int `json:"a"`
	B int `json:"b"`
}

type result struct {
	status int
	body   testResponse
	raw    string
}

type testResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newTestServer(t *testing.T) (*httptest.Server, *Router) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(logger)
	router.Handle("math.add", Bind(func(_ context.Context, p addParams) (int, error) {
		return p.A + p.B, nil
	}))
	router.Handle("boom", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, errors.New("数据库连接失败")
	})
	router.Handle("reject", func(_ context.Context, _ json.RawMessage) (any, error) {
		return nil, NewCodedError(CodeInvalidParams, "参数被拒绝")
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, router
}

func post(t *testing.T, server *httptest.Server, path, body string) result {
	t.Helper()
	resp, err := http.Post(server.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s error = %v", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	var payload testResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("响应不是 JSON: %q (%v)", raw, err)
	}
	return result{status: resp.StatusCode, body: payload, raw: string(raw)}
}

func TestCallSuccess(t *testing.T) {
	server, _ := newTestServer(t)
	got := post(t, server, "/api/call", `{"method":"math.add","params":{"a":20,"b":22}}`)

	if got.status != http.StatusOK {
		t.Fatalf("status = %d, 期望 200; body = %s", got.status, got.raw)
	}
	if !got.body.OK {
		t.Fatalf("ok = false; body = %s", got.raw)
	}
	if string(got.body.Data) != "42" {
		t.Fatalf("data = %s, 期望 42", got.body.Data)
	}
}

func TestCallWithoutParams(t *testing.T) {
	server, _ := newTestServer(t)
	got := post(t, server, "/api/call", `{"method":"math.add"}`)
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, 期望 200; body = %s", got.status, got.raw)
	}
	if string(got.body.Data) != "0" {
		t.Fatalf("data = %s, 期望 0", got.body.Data)
	}
}

func TestCallUnknownMethod(t *testing.T) {
	server, _ := newTestServer(t)
	got := post(t, server, "/api/call", `{"method":"nope"}`)

	if got.status != http.StatusNotFound {
		t.Fatalf("status = %d, 期望 404", got.status)
	}
	if got.body.Error == nil || got.body.Error.Code != string(CodeMethodNotFound) {
		t.Fatalf("error = %+v, 期望 method_not_found", got.body.Error)
	}
}

func TestCallInvalidJSON(t *testing.T) {
	server, _ := newTestServer(t)

	for _, body := range []string{"", "{", `{"method":1}`, `{"method":"math.add"} {}`} {
		got := post(t, server, "/api/call", body)
		if got.status != http.StatusBadRequest {
			t.Errorf("body %q status = %d, 期望 400", body, got.status)
		}
		if got.body.Error == nil || got.body.Error.Code != string(CodeInvalidJSON) {
			t.Errorf("body %q error = %+v, 期望 invalid_json", body, got.body.Error)
		}
	}
}

func TestCallInvalidParams(t *testing.T) {
	server, _ := newTestServer(t)

	for _, body := range []string{
		`{"method":"math.add","params":{"a":"x"}}`,
		`{"method":"math.add","params":{"unknown":1}}`,
		`{"method":"math.add","params":[1,2]}`,
	} {
		got := post(t, server, "/api/call", body)
		if got.status != http.StatusBadRequest {
			t.Errorf("body %q status = %d, 期望 400", body, got.status)
		}
		if got.body.Error == nil || got.body.Error.Code != string(CodeInvalidParams) {
			t.Errorf("body %q error = %+v, 期望 invalid_params", body, got.body.Error)
		}
	}
}

func TestCallCodedError(t *testing.T) {
	server, _ := newTestServer(t)
	got := post(t, server, "/api/call", `{"method":"reject"}`)

	if got.status != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400", got.status)
	}
	if got.body.Error == nil || got.body.Error.Message != "参数被拒绝" {
		t.Fatalf("error = %+v", got.body.Error)
	}
}

func TestCallInternalError(t *testing.T) {
	server, _ := newTestServer(t)
	got := post(t, server, "/api/call", `{"method":"boom"}`)

	if got.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, 期望 500", got.status)
	}
	if got.body.Error == nil || got.body.Error.Code != string(CodeInternal) {
		t.Fatalf("error = %+v, 期望 internal_error", got.body.Error)
	}
	if strings.Contains(got.raw, "数据库") {
		t.Fatalf("内部错误细节泄漏到响应: %s", got.raw)
	}
}

func TestCallMethodNotAllowed(t *testing.T) {
	server, _ := newTestServer(t)
	resp, err := http.Get(server.URL + "/api/call")
	if err != nil {
		t.Fatalf("GET /api/call error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, 期望 405", resp.StatusCode)
	}
}

func TestRequestTooLarge(t *testing.T) {
	server, _ := newTestServer(t)
	body := fmt.Sprintf(`{"method":"math.add","params":{"a":1},"padding":%q}`, strings.Repeat("x", MaxRequestBytes))
	got := post(t, server, "/api/call", body)

	if got.status != http.StatusBadRequest {
		t.Fatalf("status = %d, 期望 400", got.status)
	}
	if got.body.Error == nil || got.body.Error.Code != string(CodeInvalidJSON) {
		t.Fatalf("error = %+v, 期望 invalid_json", got.body.Error)
	}
}

func TestHealth(t *testing.T) {
	server, router := newTestServer(t)
	resp, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health error = %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Status  string   `json:"status"`
			Methods []string `json:"methods"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("解析健康检查响应失败: %v", err)
	}
	if !payload.OK || payload.Data.Status != "ok" {
		t.Fatalf("健康检查响应 = %+v", payload)
	}
	if len(payload.Data.Methods) != len(router.Methods()) {
		t.Fatalf("methods = %v, 期望 %v", payload.Data.Methods, router.Methods())
	}
}

func TestHandleDuplicateMethodPanics(t *testing.T) {
	router := NewRouter(nil)
	router.Handle("a", func(context.Context, json.RawMessage) (any, error) { return nil, nil })

	defer func() {
		if recover() == nil {
			t.Fatal("重复注册方法应 panic")
		}
	}()
	router.Handle("a", func(context.Context, json.RawMessage) (any, error) { return nil, nil })
}

func TestBindNullParams(t *testing.T) {
	handler := Bind(func(_ context.Context, p addParams) (int, error) {
		return p.A + p.B, nil
	})
	got, err := handler(context.Background(), json.RawMessage("null"))
	if err != nil {
		t.Fatalf("Bind(null) error = %v", err)
	}
	if got != 0 {
		t.Fatalf("Bind(null) = %v, 期望 0", got)
	}
}
