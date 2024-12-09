package node

import (
	"github.com/google/uuid"
)

type Role uint8

const (
	ManagerRole Role = iota
	PropletRole
)

type Node struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Memory          uint64 `json:"memory"`
	MemoryAllocated uint64 `json:"memory_allocated"`
	Disk            uint64 `json:"disk"`
	DiskAllocated   uint64 `json:"disk_allocated"`
	TaskCount       uint64 `json:"task_count"`
	Role            Role   `json:"role"`
}

type NodePage struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
	Total  uint64 `json:"total"`
	Nodes  []Node `json:"nodes"`
}

func NewNode(name, url string, role Role) *Node {
	return &Node{
		ID:   uuid.NewString(),
		Name: name,
		URL:  url,
		Role: role,
	}
}
