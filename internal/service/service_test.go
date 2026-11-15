package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestGreet(t *testing.T) {
	svc := New("test", "v0.0.1")
	ctx := context.Background()

	greeting, err := svc.Greet(ctx, "  Wails  ")
	if err != nil {
		t.Fatalf("Greet() error = %v", err)
	}
	if !strings.Contains(greeting.Message, "Wails") {
		t.Fatalf("Greet() message = %q, 期望包含 Wails", greeting.Message)
	}
	if greeting.At.IsZero() {
		t.Fatal("Greet() 返回的时间为零值")
	}

	if _, err := svc.Greet(ctx, "   "); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("Greet(blank) error = %v, 期望 ErrEmptyName", err)
	}

	longName := strings.Repeat("界", MaxNameRunes+1)
	if _, err := svc.Greet(ctx, longName); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("Greet(long) error = %v, 期望 ErrNameTooLong", err)
	}

	// 边界值应通过：恰好 MaxNameRunes 个字符。
	if _, err := svc.Greet(ctx, strings.Repeat("界", MaxNameRunes)); err != nil {
		t.Fatalf("Greet(max) error = %v, 期望成功", err)
	}
}

func TestCounter(t *testing.T) {
	svc := New("test", "dev")
	ctx := context.Background()

	state, err := svc.Add(ctx, 5)
	if err != nil || state.Value != 5 {
		t.Fatalf("Add(5) = %+v, %v", state, err)
	}
	state, err = svc.Add(ctx, -2)
	if err != nil || state.Value != 3 {
		t.Fatalf("Add(-2) = %+v, %v", state, err)
	}

	if _, err := svc.Add(ctx, MaxCounterDelta+1); !errors.Is(err, ErrDeltaOutOfRange) {
		t.Fatalf("Add(out of range) error = %v, 期望 ErrDeltaOutOfRange", err)
	}

	state = svc.Reset(ctx)
	if state.Value != 0 {
		t.Fatalf("Reset() = %+v, 期望 0", state)
	}
	if got := svc.Counter(ctx); got.Value != 0 {
		t.Fatalf("Counter() = %+v, 期望 0", got)
	}
}

func TestCounterConcurrent(t *testing.T) {
	svc := New("test", "dev")
	ctx := context.Background()

	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := svc.Add(ctx, 1); err != nil {
				t.Errorf("Add(1) error = %v", err)
			}
		}()
	}
	wg.Wait()

	if got := svc.Counter(ctx).Value; got != workers {
		t.Fatalf("并发累加后 Counter = %d, 期望 %d", got, workers)
	}
}

func TestInfo(t *testing.T) {
	svc := New("test_go_wails", "v1.2.3")
	info := svc.Info(context.Background())

	if info.Name != "test_go_wails" || info.Version != "v1.2.3" {
		t.Fatalf("Info() = %+v", info)
	}
	if info.GoVersion == "" || info.OS == "" || info.Arch == "" || info.PID <= 0 {
		t.Fatalf("Info() 运行环境字段不完整: %+v", info)
	}
	if info.UptimeSeconds < 0 {
		t.Fatalf("Info() uptime = %d, 期望非负", info.UptimeSeconds)
	}
}
