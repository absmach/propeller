package plugin

import (
	"context"
	"time"
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
	Token  string `json:"token,omitempty"`
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
	Task TaskInfo `json:"task"`
}

type EnrichResponse struct {
	Env      map[string]string `json:"env,omitempty"`
	Priority *int              `json:"priority,omitempty"`
	Inputs   []string          `json:"inputs,omitempty"`
}

type TaskEvent struct {
	Task    TaskInfo `json:"task"`
	Success bool     `json:"success,omitempty"`
	Result  string   `json:"result,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type Plugin interface {
	Name() string
	Authorize(ctx context.Context, req AuthorizeRequest) (AuthorizeResponse, error)
	Enrich(ctx context.Context, req EnrichRequest) (EnrichResponse, error)
	OnTaskStart(ctx context.Context, evt TaskEvent) error
	OnTaskComplete(ctx context.Context, evt TaskEvent) error
	Close(ctx context.Context) error
}

type Registry interface {
	List() []Plugin
	Close(ctx context.Context) error
}
