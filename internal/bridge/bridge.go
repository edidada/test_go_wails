// Package bridge 提供前端与 Go 后端之间的通信层。
//
// 它对应 Wails 中 bindings 的角色：把统一信封的 JSON 调用分发到已注册的
// Handler，并把结果或错误编码为稳定协议。该层只依赖标准库，不感知业务。
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
)

// MaxRequestBytes 是 /api/call 允许的最大请求体（1 MiB）。
const MaxRequestBytes = 1 << 20

// ErrorCode 是稳定的协议错误码，前端可据此分支处理。
type ErrorCode string

const (
	// CodeInvalidJSON 表示请求体不是合法 JSON。
	CodeInvalidJSON ErrorCode = "invalid_json"
	// CodeInvalidParams 表示参数反序列化失败或业务校验失败。
	CodeInvalidParams ErrorCode = "invalid_params"
	// CodeMethodNotFound 表示方法未注册。
	CodeMethodNotFound ErrorCode = "method_not_found"
	// CodeInternal 表示服务端未知错误。
	CodeInternal ErrorCode = "internal_error"
)

// CodedError 携带协议错误码，供 HTTP 层映射状态码。
type CodedError struct {
	Code    ErrorCode
	Message string
	Err     error
}

// NewCodedError 创建不带底层原因的协议错误。
func NewCodedError(code ErrorCode, message string) *CodedError {
	return &CodedError{Code: code, Message: message}
}

// WrapCodedError 创建带底层原因的协议错误。
func WrapCodedError(code ErrorCode, message string, err error) *CodedError {
	return &CodedError{Code: code, Message: message, Err: err}
}

// Error 实现 error 接口。
func (e *CodedError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is / errors.As。
func (e *CodedError) Unwrap() error { return e.Err }

// Handler 是已注册方法的统一签名。
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Router 负责方法注册与 HTTP 分发，并发安全。
type Router struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	logger   *slog.Logger
}

// NewRouter 创建路由。logger 为空时使用 slog.Default()。
func NewRouter(logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{
		handlers: make(map[string]Handler),
		logger:   logger,
	}
}

// Handle 注册方法。方法名为空或重复注册属于编程错误，直接 panic。
func (r *Router) Handle(method string, h Handler) {
	if method == "" {
		panic("bridge: method must not be empty")
	}
	if h == nil {
		panic("bridge: handler must not be nil for method " + method)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[method]; exists {
		panic("bridge: duplicate method " + method)
	}
	r.handlers[method] = h
}

// Methods 返回已注册方法名的有序副本。
func (r *Router) Methods() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	methods := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		methods = append(methods, name)
	}
	sort.Strings(methods)
	return methods
}

func (r *Router) lookup(method string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[method]
	return h, ok
}

// ServeHTTP 实现 http.Handler，负责 /api/health 与 /api/call。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/api/health":
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, CodeMethodNotFound, "方法不被允许")
			return
		}
		writeJSON(w, http.StatusOK, response{OK: true, Data: healthPayload{
			Status:  "ok",
			Methods: r.Methods(),
		}})
	case "/api/call":
		r.handleCall(w, req)
	default:
		writeError(w, http.StatusNotFound, CodeMethodNotFound, "未知的 API 路径")
	}
}

type callRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (r *Router) handleCall(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, CodeMethodNotFound, "方法不被允许")
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, MaxRequestBytes)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()

	var call callRequest
	if err := decoder.Decode(&call); err != nil {
		message := "请求体必须是合法的 JSON"
		if errors.As(err, new(*http.MaxBytesError)) {
			message = fmt.Sprintf("请求体不能超过 %d 字节", MaxRequestBytes)
		} else if errors.Is(err, io.EOF) {
			message = "请求体不能为空"
		}
		r.logger.Warn("bridge: invalid request", "error", err)
		writeError(w, http.StatusBadRequest, CodeInvalidJSON, message)
		return
	}
	if err := ensureEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidJSON, "请求体只能包含一个 JSON 对象")
		return
	}
	if call.Method == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidParams, "method 不能为空")
		return
	}

	handler, ok := r.lookup(call.Method)
	if !ok {
		writeError(w, http.StatusNotFound, CodeMethodNotFound, "方法未注册: "+call.Method)
		return
	}

	data, err := handler(req.Context(), call.Params)
	if err != nil {
		var coded *CodedError
		if errors.As(err, &coded) {
			status := statusForCode(coded.Code)
			if status >= http.StatusInternalServerError {
				r.logger.Error("bridge: handler failed", "method", call.Method, "error", err)
			} else {
				r.logger.Warn("bridge: request rejected", "method", call.Method, "error", err)
			}
			writeError(w, status, coded.Code, coded.Message)
			return
		}
		r.logger.Error("bridge: handler failed", "method", call.Method, "error", err)
		writeError(w, http.StatusInternalServerError, CodeInternal, "服务内部错误")
		return
	}

	writeJSON(w, http.StatusOK, response{OK: true, Data: data})
}

func ensureEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}

// Bind 把业务函数适配为 Handler，并负责参数反序列化。
// P 为参数结构体；无参数的方法传入 struct{} 即可。
func Bind[P any, R any](fn func(ctx context.Context, params P) (R, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params P
		if len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&params); err != nil {
				return nil, WrapCodedError(CodeInvalidParams, "参数格式不正确", err)
			}
			if err := ensureEOF(decoder); err != nil {
				return nil, WrapCodedError(CodeInvalidParams, "参数格式不正确", err)
			}
		}
		return fn(ctx, params)
	}
}

type response struct {
	OK    bool      `json:"ok"`
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type healthPayload struct {
	Status  string   `json:"status"`
	Methods []string `json:"methods"`
}

func writeError(w http.ResponseWriter, status int, code ErrorCode, message string) {
	writeJSON(w, status, response{OK: false, Error: &apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, payload response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func statusForCode(code ErrorCode) int {
	switch code {
	case CodeInvalidJSON, CodeInvalidParams:
		return http.StatusBadRequest
	case CodeMethodNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
