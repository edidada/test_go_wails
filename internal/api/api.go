// Package api 把业务服务注册到通信层，并负责业务错误到协议错误的映射。
package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/edidada/test_go_wails/internal/bridge"
	"github.com/edidada/test_go_wails/internal/service"
)

type greetParams struct {
	Name string `json:"name"`
}

type addParams struct {
	Delta int64 `json:"delta"`
}

// Register 注册全部对外方法。方法表见 docs/architecture.md。
func Register(router *bridge.Router, svc *service.Service) {
	router.Handle("app.info", bridge.Bind(func(ctx context.Context, _ struct{}) (service.Info, error) {
		return svc.Info(ctx), nil
	}))

	router.Handle("greeter.greet", bridge.Bind(func(ctx context.Context, params greetParams) (service.Greeting, error) {
		greeting, err := svc.Greet(ctx, params.Name)
		switch {
		case err == nil:
			return greeting, nil
		case errors.Is(err, service.ErrEmptyName):
			return greeting, bridge.NewCodedError(bridge.CodeInvalidParams, "name 不能为空")
		case errors.Is(err, service.ErrNameTooLong):
			return greeting, bridge.NewCodedError(
				bridge.CodeInvalidParams,
				fmt.Sprintf("name 不能超过 %d 个字符", service.MaxNameRunes),
			)
		default:
			return greeting, err
		}
	}))

	router.Handle("counter.add", bridge.Bind(func(ctx context.Context, params addParams) (service.CounterState, error) {
		state, err := svc.Add(ctx, params.Delta)
		if errors.Is(err, service.ErrDeltaOutOfRange) {
			return state, bridge.NewCodedError(
				bridge.CodeInvalidParams,
				fmt.Sprintf("delta 必须在 -%d 到 %d 之间", service.MaxCounterDelta, service.MaxCounterDelta),
			)
		}
		return state, err
	}))

	router.Handle("counter.reset", bridge.Bind(func(ctx context.Context, _ struct{}) (service.CounterState, error) {
		return svc.Reset(ctx), nil
	}))

	router.Handle("counter.get", bridge.Bind(func(ctx context.Context, _ struct{}) (service.CounterState, error) {
		return svc.Counter(ctx), nil
	}))
}
