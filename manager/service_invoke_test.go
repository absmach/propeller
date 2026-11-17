package manager_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testTenantID  = "test-tenant"
	testChannelID = "test-channel"
	testBaseTopic = "m/test-tenant/c/test-channel"
	testInvokeRes = testBaseTopic + "/control/proplet/invoke_results"
)

func TestInvokeBroadcastRetries(t *testing.T) {
	t.Parallel()

	const notPrecompiled = "task x has not been precompiled; deploy it as a latent task first"

	t.Run("skips stale proplet and succeeds on healthy one", func(t *testing.T) {
		t.Parallel()

		staleID := uuid.NewString()
		healthyID := uuid.NewString()

		svc, _ := broadcastInvokeService(t, func(propletID string) (string, string) {
			if propletID == staleID {
				return "", notPrecompiled
			}

			return "hello from " + healthyID, ""
		}, staleID, healthyID)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		created, err := svc.CreateTask(ctx, task.Task{
			Name:      "latent-fn",
			Latent:    true,
			Broadcast: true,
		})
		require.NoError(t, err)

		results, err := svc.InvokeTask(ctx, created.ID, []string{"world"}, nil)
		require.NoError(t, err)
		require.Equal(t, "hello from "+healthyID, results)
	})

	t.Run("re-deploys latent when every proplet lost the precompiled cache", func(t *testing.T) {
		t.Parallel()

		staleA := uuid.NewString()
		staleB := uuid.NewString()

		svc, redeploys := broadcastInvokeService(t, func(string) (string, string) {
			return "", notPrecompiled
		}, staleA, staleB)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		created, err := svc.CreateTask(ctx, task.Task{
			Name:      "latent-fn",
			Latent:    true,
			Broadcast: true,
		})
		require.NoError(t, err)

		_, err = svc.InvokeTask(ctx, created.ID, []string{"world"}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "has not been precompiled")

		got := redeploys()
		require.Len(t, got, 2)
		ids := map[string]bool{}
		for _, r := range got {
			broadcast, ok := r["broadcast"].(bool)
			require.True(t, ok)
			require.False(t, broadcast, "re-deploy must be targeted, not broadcast")

			pid, ok := r["proplet_id"].(string)
			require.True(t, ok)
			require.NotEmpty(t, pid)
			ids[pid] = true
		}
		require.True(t, ids[staleA])
		require.True(t, ids[staleB])
	})

	t.Run("re-deploy preserves WASI security metadata", func(t *testing.T) {
		t.Parallel()

		stale := uuid.NewString()
		svc, redeploys := broadcastInvokeService(t, func(string) (string, string) {
			return "", notPrecompiled
		}, stale)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		policy := "arguments = [\"--verbose\"]"
		created, err := svc.CreateTask(ctx, task.Task{
			Name:      "latent-fn",
			Latent:    true,
			Broadcast: true,
			Metadata: task.Metadata{
				task.MetadataElasticKey: map[string]any{
					task.ElasticWasiSecurity: policy,
				},
			},
		})
		require.NoError(t, err)

		_, err = svc.InvokeTask(ctx, created.ID, []string{"world"}, nil)
		require.Error(t, err)

		got := redeploys()
		require.Len(t, got, 1)
		metadata, ok := got[0]["metadata"].(map[string]any)
		require.True(t, ok, "re-deploy must carry the reserved elastic metadata")
		require.Equal(t, policy, metadata[task.ElasticWasiSecurity])
	})
}

func broadcastInvokeService(
	t *testing.T,
	respond func(propletID string) (results, errMsg string),
	propletIDs ...string,
) (service manager.Service, redeploys func() []map[string]any) {
	t.Helper()

	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	require.NoError(t, err)
	for _, id := range propletIDs {
		require.NoError(t, repos.Proplets.Create(context.Background(), proplet.Proplet{
			ID:           id,
			Name:         id,
			AliveHistory: []time.Time{time.Now()},
		}))
	}

	pubsub := mocks.NewMockPubSub(t)

	var mu sync.Mutex
	var redeployPayloads []map[string]any
	var invokeHandler mqtt.Handler

	pubsub.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			topic, _ := args.Get(1).(string)
			handler, _ := args.Get(2).(mqtt.Handler)
			if strings.HasSuffix(topic, "/#") {
				mu.Lock()
				invokeHandler = handler
				mu.Unlock()
			}
		}).
		Return(nil).Maybe()
	pubsub.On("Unsubscribe", mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Disconnect", mock.Anything).Return(nil).Maybe()

	svc, _, _ := manager.NewService(
		repos, scheduler.NewRoundRobin(), pubsub,
		testTenantID, testChannelID, "", slog.Default(), nil,
	)
	require.NoError(t, svc.Subscribe(context.Background()))

	pubsub.On("Publish", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			topic, _ := args.Get(1).(string)
			switch {
			case strings.HasSuffix(topic, "/control/manager/invoke"):
				payload, _ := args.Get(2).(map[string]any)
				invocationID, _ := payload["invocation_id"].(string)
				propletID, _ := payload["proplet_id"].(string)
				if invocationID == "" {
					return
				}
				results, errMsg := respond(propletID)
				msg := map[string]any{"invocation_id": invocationID}
				if errMsg != "" {
					msg["error"] = errMsg
				}
				if results != "" {
					msg["results"] = results
				}
				mu.Lock()
				handler := invokeHandler
				mu.Unlock()
				_ = handler(testInvokeRes, msg)
			case strings.HasSuffix(topic, "/control/manager/start"):
				raw, err := json.Marshal(args.Get(2))
				if err != nil {
					return
				}
				var payload struct {
					Broadcast bool           `json:"broadcast"`
					Latent    bool           `json:"latent"`
					PropletID string         `json:"proplet_id"`
					Metadata  map[string]any `json:"metadata"`
				}
				if err := json.Unmarshal(raw, &payload); err != nil {
					return
				}
				if !payload.Latent || payload.Broadcast {
					return
				}
				mu.Lock()
				redeployPayloads = append(redeployPayloads, map[string]any{
					"broadcast":  payload.Broadcast,
					"proplet_id": payload.PropletID,
					"metadata":   payload.Metadata,
				})
				mu.Unlock()
			}
		}).
		Return(nil).Maybe()

	return svc, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		out := make([]map[string]any, len(redeployPayloads))
		copy(out, redeployPayloads)

		return out
	}
}
