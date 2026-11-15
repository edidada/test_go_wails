package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MaxNameRunes 是问候接口允许的最大名字长度（按 Unicode 字符计）。
const MaxNameRunes = 32

// MaxCounterDelta 是计数器单次允许调整的最大绝对值。
const MaxCounterDelta int64 = 1_000_000

var (
	// ErrEmptyName 表示名字为空或只有空白字符。
	ErrEmptyName = errors.New("name must not be empty")
	// ErrNameTooLong 表示名字超过 MaxNameRunes。
	ErrNameTooLong = errors.New("name is too long")
	// ErrDeltaOutOfRange 表示计数器增量超出允许范围。
	ErrDeltaOutOfRange = errors.New("delta is out of range")
)

// Info 描述应用与运行环境。
type Info struct {
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	GoVersion     string    `json:"goVersion"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"startedAt"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
}

// Greeting 是问候接口的返回值。
type Greeting struct {
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// CounterState 是计数器的当前状态。
type CounterState struct {
	Value     int64     `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Service 聚合应用的全部业务能力，并发安全。
type Service struct {
	name      string
	version   string
	startedAt time.Time

	mu      sync.Mutex
	counter int64
	updated time.Time
}

// New 创建业务服务。
func New(name, version string) *Service {
	now := time.Now().UTC()
	return &Service{
		name:      name,
		version:   version,
		startedAt: now,
		updated:   now,
	}
}

// Info 返回应用与运行环境信息。
func (s *Service) Info(_ context.Context) Info {
	now := time.Now().UTC()
	return Info{
		Name:          s.name,
		Version:       s.version,
		GoVersion:     runtime.Version(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		PID:           os.Getpid(),
		StartedAt:     s.startedAt,
		UptimeSeconds: int64(now.Sub(s.startedAt).Seconds()),
	}
}

// Greet 返回一句来自 Go 后端的问候。
func (s *Service) Greet(_ context.Context, name string) (Greeting, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Greeting{}, ErrEmptyName
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return Greeting{}, ErrNameTooLong
	}
	return Greeting{
		Message: fmt.Sprintf("你好，%s！这条消息来自 Go 后端。", name),
		At:      time.Now().UTC(),
	}, nil
}

// Add 调整计数器并返回最新状态。
func (s *Service) Add(_ context.Context, delta int64) (CounterState, error) {
	if delta < -MaxCounterDelta || delta > MaxCounterDelta {
		return CounterState{}, ErrDeltaOutOfRange
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter += delta
	s.updated = time.Now().UTC()
	return CounterState{Value: s.counter, UpdatedAt: s.updated}, nil
}

// Reset 将计数器归零。
func (s *Service) Reset(_ context.Context) CounterState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter = 0
	s.updated = time.Now().UTC()
	return CounterState{Value: s.counter, UpdatedAt: s.updated}
}

// Counter 返回计数器当前状态。
func (s *Service) Counter(_ context.Context) CounterState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CounterState{Value: s.counter, UpdatedAt: s.updated}
}
