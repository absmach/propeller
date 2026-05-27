//go:build cgo

package plugin_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/absmach/propeller/pkg/plugin"
)

const examplePluginPath = "../../examples/plugin-auth/target/wasm32-wasip1/release/plugin_auth.wasm"

func TestExamplePluginRoundTrip(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(examplePluginPath); err != nil {
		t.Skipf("example plugin not built: %v", err)
	}

	ctx := context.Background()
	p, err := plugin.LoadWasm(ctx, "plugin-auth", examplePluginPath, slog.Default())
	if err != nil {
		t.Fatalf("LoadWasm: %v", err)
	}
	t.Cleanup(func() {
		_ = p.Close(ctx)
	})

	t.Run("authorize denies empty task name", func(t *testing.T) {
		t.Parallel()

		resp, err := p.Authorize(ctx, plugin.AuthorizeRequest{
			Context: plugin.AuthContext{UserID: "alice", Action: plugin.ActionCreate},
			Task:    plugin.TaskInfo{Name: ""},
		})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if resp.Allow {
			t.Fatalf("expected deny, got allow")
		}
	})

	t.Run("authorize requires user_id on create", func(t *testing.T) {
		t.Parallel()

		resp, err := p.Authorize(ctx, plugin.AuthorizeRequest{
			Context: plugin.AuthContext{UserID: "", Action: plugin.ActionCreate},
			Task:    plugin.TaskInfo{Name: "example"},
		})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if resp.Allow {
			t.Fatalf("expected deny for missing user_id")
		}
	})

	t.Run("authorize allows valid request", func(t *testing.T) {
		t.Parallel()

		resp, err := p.Authorize(ctx, plugin.AuthorizeRequest{
			Context: plugin.AuthContext{UserID: "alice", Action: plugin.ActionCreate},
			Task:    plugin.TaskInfo{Name: "example"},
		})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if !resp.Allow {
			t.Fatalf("expected allow, got deny: %s", resp.Reason)
		}
	})

	t.Run("enrich stamps PROPELLER_CREATED_BY", func(t *testing.T) {
		t.Parallel()

		resp, err := p.Enrich(ctx, plugin.EnrichRequest{
			Context: plugin.AuthContext{UserID: "alice", Action: plugin.ActionCreate},
			Task:    plugin.TaskInfo{Name: "example"},
		})
		if err != nil {
			t.Fatalf("Enrich: %v", err)
		}
		if resp.Env["PROPELLER_PLUGIN_AUTHZ"] != "plugin-auth" {
			t.Fatalf("expected PROPELLER_PLUGIN_AUTHZ=plugin-auth, got %v", resp.Env)
		}
		if resp.Env["PROPELLER_CREATED_BY"] != "alice" {
			t.Fatalf("expected PROPELLER_CREATED_BY=alice, got %v", resp.Env)
		}
	})

	t.Run("start denied for different user", func(t *testing.T) {
		t.Parallel()

		resp, err := p.Authorize(ctx, plugin.AuthorizeRequest{
			Context: plugin.AuthContext{UserID: "bob", Action: plugin.ActionStart},
			Task: plugin.TaskInfo{
				Name: "example",
				Env:  map[string]string{"PROPELLER_CREATED_BY": "alice"},
			},
		})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if resp.Allow {
			t.Fatalf("expected deny for wrong user, got allow")
		}
	})

	t.Run("start allowed for task owner", func(t *testing.T) {
		t.Parallel()

		resp, err := p.Authorize(ctx, plugin.AuthorizeRequest{
			Context: plugin.AuthContext{UserID: "alice", Action: plugin.ActionStart},
			Task: plugin.TaskInfo{
				Name: "example",
				Env:  map[string]string{"PROPELLER_CREATED_BY": "alice"},
			},
		})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if !resp.Allow {
			t.Fatalf("expected allow for task owner, got deny: %s", resp.Reason)
		}
	})
}
