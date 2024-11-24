package main

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
	"github.com/google/uuid"
)

//go:embed add.wasm
var addWasm []byte

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t := task.Task{
		ID:    uuid.New().String(),
		Name:  "Addition",
		State: task.Pending,
		Function: task.Function{
			File:   addWasm,
			Name:   "add",
			Inputs: []uint64{5, 10},
		},
	}

	fmt.Printf("task: %s\n", t.Name)

	w := worker.NewWasmWorker("Wasm-Worker-1")
	w.StartTask(ctx, t)
	results, err := w.RunTask(ctx, t.ID)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("results: %v\n", results)
}
