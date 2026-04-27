package middleware_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/manager/middleware"
	managermocks "github.com/absmach/propeller/manager/mocks"
	"github.com/absmach/propeller/pkg/plugin"
	"github.com/absmach/propeller/pkg/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockPlugin struct {
	mock.Mock
}

func (m *mockPlugin) Name() string { return "mock-plugin" }

func (m *mockPlugin) Authorize(ctx context.Context, req plugin.AuthorizeRequest) (plugin.AuthorizeResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(plugin.AuthorizeResponse)

	return resp, args.Error(1)
}

func (m *mockPlugin) Enrich(ctx context.Context, req plugin.EnrichRequest) (plugin.EnrichResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(plugin.EnrichResponse)

	return resp, args.Error(1)
}

func (m *mockPlugin) OnBeforePropletSelect(ctx context.Context, req plugin.PropletSelectRequest) (plugin.PropletSelectResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(plugin.PropletSelectResponse)

	return resp, args.Error(1)
}

func (m *mockPlugin) OnBeforeDispatch(ctx context.Context, req plugin.DispatchRequest) (plugin.DispatchResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(plugin.DispatchResponse)

	return resp, args.Error(1)
}

func (m *mockPlugin) OnTaskStart(ctx context.Context, evt plugin.TaskEvent) error {
	return m.Called(ctx, evt).Error(0)
}

func (m *mockPlugin) OnTaskComplete(ctx context.Context, evt plugin.TaskEvent) error {
	return m.Called(ctx, evt).Error(0)
}

func (m *mockPlugin) Close(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

type mockRegistry struct {
	plugins []plugin.Plugin
}

func (r *mockRegistry) List() []plugin.Plugin { return r.plugins }

func (r *mockRegistry) Close(_ context.Context) error { return nil }

func newPluginMiddleware(t *testing.T, pl plugin.Plugin) (*managermocks.MockService, manager.Service) {
	t.Helper()
	svc := managermocks.NewMockService(t)
	reg := &mockRegistry{plugins: []plugin.Plugin{pl}}

	return svc, middleware.Plugin(reg, slog.Default(), svc)
}

func TestPluginMiddleware_CreateTask_Authorize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc      string
		authResp  plugin.AuthorizeResponse
		authErr   error
		wantErr   bool
		svcCalled bool
	}{
		{
			desc:      "plugin allows create",
			authResp:  plugin.AuthorizeResponse{Allow: true},
			authErr:   nil,
			wantErr:   false,
			svcCalled: true,
		},
		{
			desc:      "plugin denies create",
			authResp:  plugin.AuthorizeResponse{Allow: false, Reason: "denied"},
			authErr:   nil,
			wantErr:   true,
			svcCalled: false,
		},
		{
			desc:      "plugin authorize returns error",
			authResp:  plugin.AuthorizeResponse{},
			authErr:   errors.New("auth error"),
			wantErr:   true,
			svcCalled: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			pl := new(mockPlugin)
			pl.On("Authorize", mock.Anything, mock.Anything).Return(tc.authResp, tc.authErr)
			pl.On("Enrich", mock.Anything, mock.Anything).Return(plugin.EnrichResponse{}, nil).Maybe()

			svc, mw := newPluginMiddleware(t, pl)

			if tc.svcCalled {
				svc.On("CreateTask", mock.Anything, mock.Anything).Return(task.Task{}, nil)
			}

			_, err := mw.CreateTask(context.Background(), task.Task{Name: "t1"})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPluginMiddleware_CreateTask_Enrich(t *testing.T) {
	t.Parallel()

	prio := 99
	cases := []struct {
		desc      string
		enrichRes plugin.EnrichResponse
		wantEnv   map[string]string
		wantPrio  int
	}{
		{
			desc:      "enrich injects env",
			enrichRes: plugin.EnrichResponse{Env: map[string]string{"FOO": "bar"}},
			wantEnv:   map[string]string{"FOO": "bar"},
			wantPrio:  0,
		},
		{
			desc:      "enrich sets priority",
			enrichRes: plugin.EnrichResponse{Priority: &prio},
			wantEnv:   nil,
			wantPrio:  99,
		},
		{
			desc:      "no enrichment",
			enrichRes: plugin.EnrichResponse{},
			wantEnv:   nil,
			wantPrio:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			pl := new(mockPlugin)
			pl.On("Authorize", mock.Anything, mock.Anything).Return(plugin.AuthorizeResponse{Allow: true}, nil)
			pl.On("Enrich", mock.Anything, mock.Anything).Return(tc.enrichRes, nil)

			svc, mw := newPluginMiddleware(t, pl)
			svc.On("CreateTask", mock.Anything, mock.MatchedBy(func(t task.Task) bool {
				if tc.wantPrio != 0 {
					return t.Priority == tc.wantPrio
				}
				for k, v := range tc.wantEnv {
					if t.Env[k] != v {
						return false
					}
				}

				return true
			})).Return(task.Task{}, nil)

			_, err := mw.CreateTask(context.Background(), task.Task{Name: "t1"})
			require.NoError(t, err)
		})
	}
}

func TestPluginMiddleware_StartTask_Authorize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc     string
		authResp plugin.AuthorizeResponse
		authErr  error
		wantErr  bool
	}{
		{
			desc:     "plugin allows start",
			authResp: plugin.AuthorizeResponse{Allow: true},
			wantErr:  false,
		},
		{
			desc:     "plugin denies start",
			authResp: plugin.AuthorizeResponse{Allow: false, Reason: "not allowed"},
			wantErr:  true,
		},
		{
			desc:    "plugin returns error on start",
			authErr: errors.New("boom"),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			pl := new(mockPlugin)
			svc, mw := newPluginMiddleware(t, pl)

			svc.On("GetTask", mock.Anything, "task-1").Return(task.Task{ID: "task-1", Name: "t"}, nil)
			pl.On("Authorize", mock.Anything, mock.Anything).Return(tc.authResp, tc.authErr)

			if !tc.wantErr {
				svc.On("StartTask", mock.Anything, "task-1").Return(nil)
				pl.On("OnTaskStart", mock.Anything, mock.Anything).Return(nil).Maybe()
			}

			err := mw.StartTask(context.Background(), "task-1")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPluginMiddleware_StartTask_NotifyStart(t *testing.T) {
	t.Parallel()

	pl := new(mockPlugin)
	svc, mw := newPluginMiddleware(t, pl)

	svc.On("GetTask", mock.Anything, "task-1").Return(task.Task{ID: "task-1", Name: "t"}, nil)
	pl.On("Authorize", mock.Anything, mock.Anything).Return(plugin.AuthorizeResponse{Allow: true}, nil)
	svc.On("StartTask", mock.Anything, "task-1").Return(nil)
	pl.On("OnTaskStart", mock.Anything, mock.Anything).Return(nil)

	err := mw.StartTask(context.Background(), "task-1")
	require.NoError(t, err)
}

func TestPluginMiddleware_DeleteTask_Authorize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc     string
		authResp plugin.AuthorizeResponse
		wantErr  bool
	}{
		{
			desc:     "plugin allows delete",
			authResp: plugin.AuthorizeResponse{Allow: true},
			wantErr:  false,
		},
		{
			desc:     "plugin denies delete",
			authResp: plugin.AuthorizeResponse{Allow: false, Reason: "no"},
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			pl := new(mockPlugin)
			svc, mw := newPluginMiddleware(t, pl)

			svc.On("GetTask", mock.Anything, "task-1").Return(task.Task{ID: "task-1", Name: "t"}, nil)
			pl.On("Authorize", mock.Anything, mock.Anything).Return(tc.authResp, nil)

			if !tc.wantErr {
				svc.On("DeleteTask", mock.Anything, "task-1").Return(nil)
			}

			err := mw.DeleteTask(context.Background(), "task-1")
			if tc.wantErr {
				require.Error(t, err)
				assert.Equal(t, "no", err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPluginMiddleware_StopTask_Authorize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc     string
		authResp plugin.AuthorizeResponse
		wantErr  bool
	}{
		{
			desc:     "plugin allows stop",
			authResp: plugin.AuthorizeResponse{Allow: true},
			wantErr:  false,
		},
		{
			desc:     "plugin denies stop",
			authResp: plugin.AuthorizeResponse{Allow: false, Reason: "nope"},
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			pl := new(mockPlugin)
			svc, mw := newPluginMiddleware(t, pl)

			svc.On("GetTask", mock.Anything, "task-1").Return(task.Task{ID: "task-1", Name: "t"}, nil)
			pl.On("Authorize", mock.Anything, mock.Anything).Return(tc.authResp, nil)

			if !tc.wantErr {
				svc.On("StopTask", mock.Anything, "task-1").Return(nil)
			}

			err := mw.StopTask(context.Background(), "task-1")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPluginMiddleware_EmptyRegistry(t *testing.T) {
	t.Parallel()

	svc := managermocks.NewMockService(t)
	reg := &mockRegistry{plugins: nil}
	mw := middleware.Plugin(reg, slog.Default(), svc)

	svc.On("CreateTask", mock.Anything, mock.Anything).Return(task.Task{Name: "t1"}, nil)

	got, err := mw.CreateTask(context.Background(), task.Task{Name: "t1"})
	require.NoError(t, err)
	assert.Equal(t, "t1", got.Name)
}
