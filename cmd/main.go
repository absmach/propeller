package main

import (
	"context"
	_ "embed"
	"log"

	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
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

	log.Printf("task: %s\n", t.Name)

	w := proplet.NewWasmProplet("Wasm-Proplet-1")
	if err := w.StartTask(ctx, t); err != nil {
		log.Println(err)
	}
	results, err := w.RunTask(ctx, t.ID)
	if err != nil {
		log.Println(err)
	}
	log.Printf("results: %v\n", results)
}
