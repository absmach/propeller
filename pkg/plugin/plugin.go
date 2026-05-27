package plugin

import (
	"context"
	"time"

	"github.com/absmach/propeller/pkg/task"
)

type Action string

const (
	ActionCreate Action = "create"
	ActionStart  Action = "start"
	ActionStop   Action = "stop"
	ActionDelete Action = "delete"
	ActionUpdate Action = "update"
)

type TaskInfo struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind,omitempty"`
	ImageURL  string            `json:"image_url,omitempty"`
	Inputs    []string          `json:"inputs,omitempty"`
	CLIArgs   []string          `json:"cli_args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	PropletID string            `json:"proplet_id,omitempty"`
	Priority  int               `json:"priority,omitempty"`
	Daemon    bool              `json:"daemon,omitempty"`
	Encrypted bool              `json:"encrypted,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitzero"`
}

type AuthContext struct {
	UserID string `json:"user_id,omitempty"`
	Token  string `json:"-"`
	Action Action `json:"action"`
}

type AuthorizeRequest struct {
	Context AuthContext `json:"context"`
	Task    TaskInfo    `json:"task"`
}

type AuthorizeResponse struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
}

type EnrichRequest struct {
	Context AuthContext `json:"context"`
	Task    TaskInfo    `json:"task"`
}

type EnrichResponse struct {
	Env      map[string]string `json:"env,omitempty"`
	Priority *int              `json:"priority,omitempty"`
	Inputs   []string          `json:"inputs,omitempty"`
	ImageURL string            `json:"image_url,omitempty"`
}

type TaskEvent struct {
	Task    TaskInfo `json:"task"`
	Success bool     `json:"success,omitempty"`
	Result  string   `json:"result,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type PropletInfo struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Tags             []string `json:"tags,omitempty"`
	TotalMemoryBytes uint64   `json:"total_memory_bytes,omitempty"`
	Location         string   `json:"location,omitempty"`
}

type PropletSelectRequest struct {
	Context AuthContext `json:"context"`
	Task    TaskInfo    `json:"task"`
}

type PropletSelectResponse struct {
	Allow          bool     `json:"allow"`
	Reason         string   `json:"reason,omitempty"`
	RequiredTags   []string `json:"required_tags,omitempty"`
	MinMemoryBytes *uint64  `json:"min_memory_bytes,omitempty"`
}

type DispatchRequest struct {
	Context AuthContext `json:"context"`
	Task    TaskInfo    `json:"task"`
	Proplet PropletInfo `json:"proplet"`
}

type DispatchResponse struct {
	Allow    bool              `json:"allow"`
	Reason   string            `json:"reason,omitempty"`
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
}

type Plugin interface {
	Name() string
	Authorize(ctx context.Context, req AuthorizeRequest) (AuthorizeResponse, error)
	Enrich(ctx context.Context, req EnrichRequest) (EnrichResponse, error)
	OnBeforePropletSelect(ctx context.Context, req PropletSelectRequest) (PropletSelectResponse, error)
	OnBeforeDispatch(ctx context.Context, req DispatchRequest) (DispatchResponse, error)
	OnTaskStart(ctx context.Context, evt TaskEvent) error
	OnTaskComplete(ctx context.Context, evt TaskEvent) error
	Close(ctx context.Context) error
}

type Registry interface {
	List() []Plugin
	Close(ctx context.Context) error
}

// NewTaskInfo converts a task.Task to the TaskInfo passed to plugins.
func NewTaskInfo(t task.Task) TaskInfo {
	return TaskInfo{
		ID:        t.ID,
		Name:      t.Name,
		Kind:      string(t.Kind),
		ImageURL:  t.ImageURL,
		Inputs:    []string(t.Inputs),
		CLIArgs:   t.CLIArgs,
		Env:       t.Env,
		PropletID: t.PropletID,
		Priority:  t.Priority,
		Daemon:    t.Daemon,
		Encrypted: t.Encrypted,
		CreatedAt: t.CreatedAt,
	}
}
