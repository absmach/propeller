package proplet

import (
	"errors"
	"time"
)

var ErrInvalidStatus = errors.New("invalid proplet status")

type Status uint8

const (
	ActiveStatus Status = iota
	InactiveStatus
)

const (
	AliveTimeout = 10 * time.Second

	Active   = "active"
	Inactive = "inactive"
	// unknown is unexported: it is only used as the default String() return value
	// and is not a valid ?status= filter value (ToStatus rejects it with ErrInvalidStatus).
	unknown = "unknown"
)

func (s Status) String() string {
	switch s {
	case ActiveStatus:
		return Active
	case InactiveStatus:
		return Inactive
	default:
		return unknown
	}
}

func ToStatus(status string) (Status, error) {
	switch status {
	case Active:
		return ActiveStatus, nil
	case Inactive:
		return InactiveStatus, nil
	default:
		return Status(0), ErrInvalidStatus
	}
}

type PropletMetadata struct {
	Description      string   `json:"description,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	Location         string   `json:"location,omitempty"`
	IP               string   `json:"ip,omitempty"`
	Environment      string   `json:"environment,omitempty"`
	OS               string   `json:"os,omitempty"`
	Hostname         string   `json:"hostname,omitempty"`
	CPUArch          string   `json:"cpu_arch,omitempty"`
	TotalMemoryBytes uint64   `json:"total_memory_bytes,omitempty"`
	PropletVersion   string   `json:"proplet_version,omitempty"`
	WasmRuntime      string   `json:"wasm_runtime,omitempty"`
}

type Proplet struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	TaskCount    uint64          `json:"task_count"`
	Alive        bool            `json:"alive"`
	AliveHistory []time.Time     `json:"alive_at"`
	Metadata     PropletMetadata `json:"metadata"`
}

func (p *Proplet) SetAlive() {
	if len(p.AliveHistory) > 0 {
		lastAlive := p.AliveHistory[len(p.AliveHistory)-1]
		if time.Since(lastAlive) <= AliveTimeout {
			p.Alive = true

			return
		}
	}
	p.Alive = false
}

type PropletPage struct {
	Offset   uint64    `json:"offset"`
	Limit    uint64    `json:"limit"`
	Total    uint64    `json:"total"`
	Proplets []Proplet `json:"proplets"`
}

type PropletView struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	TaskCount   uint64          `json:"task_count"`
	Alive       bool            `json:"alive"`
	LastAliveAt *time.Time      `json:"last_alive_at,omitempty"`
	Metadata    PropletMetadata `json:"metadata"`
}

func (p *Proplet) View() PropletView {
	v := PropletView{
		ID:        p.ID,
		Name:      p.Name,
		TaskCount: p.TaskCount,
		Alive:     p.Alive,
		Metadata:  p.Metadata,
	}
	if n := len(p.AliveHistory); n > 0 {
		t := p.AliveHistory[n-1]
		v.LastAliveAt = &t
	}

	return v
}

type PropletPageView struct {
	Offset   uint64        `json:"offset"`
	Limit    uint64        `json:"limit"`
	Total    uint64        `json:"total"`
	Proplets []PropletView `json:"proplets"`
}

func (pp PropletPage) View() PropletPageView {
	views := make([]PropletView, len(pp.Proplets))
	for i := range pp.Proplets {
		views[i] = pp.Proplets[i].View()
	}

	return PropletPageView{
		Offset:   pp.Offset,
		Limit:    pp.Limit,
		Total:    pp.Total,
		Proplets: views,
	}
}

type PropletAliveHistoryPage struct {
	Offset  uint64      `json:"offset"`
	Limit   uint64      `json:"limit"`
	Total   uint64      `json:"total"`
	History []time.Time `json:"history"`
}

type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}
