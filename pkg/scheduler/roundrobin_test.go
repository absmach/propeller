// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package scheduler_test

import (
	"sync"
	"testing"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/task"
)

func TestRoundRobinConcurrentSelect(t *testing.T) {
	t.Parallel()

	rr := scheduler.NewRoundRobin()
	proplets := []proplet.Proplet{
		{ID: "a", Alive: true},
		{ID: "b", Alive: true},
		{ID: "c", Alive: true},
	}

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 100 {
				if _, err := rr.SelectProplet(task.Task{}, proplets); err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
	wg.Wait()
}
