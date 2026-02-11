package dag

import (
	"errors"
	"fmt"
	"slices"

	"github.com/absmach/propeller/task"
)

var (
	ErrCircularDependency = errors.New("circular dependency detected")
	ErrMissingDependency  = errors.New("dependency task not found")
)

func ValidateDAG(tasks []task.Task) error {
	taskMap := make(map[string]task.Task)
	for i := range tasks {
		t := &tasks[i]
		taskMap[t.ID] = *t
	}

	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(taskID string) error
	dfs = func(taskID string) error {
		visited[taskID] = true
		recStack[taskID] = true

		t, exists := taskMap[taskID]
		if !exists {
			return nil
		}

		for _, depID := range t.DependsOn {
			if !visited[depID] {
				if err := dfs(depID); err != nil {
					return err
				}
			} else if recStack[depID] {
				return fmt.Errorf("%w: cycle detected involving tasks %s and %s", ErrCircularDependency, taskID, depID)
			}
		}

		recStack[taskID] = false

		return nil
	}

	for i := range tasks {
		t := &tasks[i]
		if !visited[t.ID] {
			if err := dfs(t.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

func ValidateDependenciesExist(tasks []task.Task) error {
	taskMap := make(map[string]bool)
	for i := range tasks {
		t := &tasks[i]
		taskMap[t.ID] = true
	}

	for i := range tasks {
		t := &tasks[i]
		for _, depID := range t.DependsOn {
			if !taskMap[depID] {
				return fmt.Errorf("%w: task %s depends on %s which does not exist", ErrMissingDependency, t.ID, depID)
			}
		}
	}

	return nil
}

func TopologicalSort(tasks []task.Task) ([]task.Task, error) {
	if err := ValidateDAG(tasks); err != nil {
		return nil, err
	}

	taskMap := make(map[string]task.Task)
	inDegree := make(map[string]int)

	for i := range tasks {
		t := &tasks[i]
		taskMap[t.ID] = *t
		inDegree[t.ID] = 0
	}

	for i := range tasks {
		t := &tasks[i]
		for range t.DependsOn {
			inDegree[t.ID]++
		}
	}

	queue := make([]string, 0)
	for taskID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, taskID)
		}
	}

	result := make([]task.Task, 0, len(tasks))
	visited := make(map[string]bool)

	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]

		if visited[currentID] {
			continue
		}

		visited[currentID] = true
		result = append(result, taskMap[currentID])

		for i := range tasks {
			t := &tasks[i]
			if slices.Contains(t.DependsOn, currentID) {
				inDegree[t.ID]--
				if inDegree[t.ID] == 0 {
					queue = append(queue, t.ID)
				}
			}
		}
	}

	if len(result) != len(tasks) {
		return nil, errors.New("topological sort failed: not all tasks processed (possible cycle)")
	}

	return result, nil
}

func GetReadyTasks(tasks []task.Task, completed map[string]task.State) []task.Task {
	ready := make([]task.Task, 0)

	for i := range tasks {
		t := &tasks[i]
		if t.State == task.Completed || t.State == task.Failed || t.State == task.Skipped {
			continue
		}

		allDepsSatisfied := true
		for _, depID := range t.DependsOn {
			depState, exists := completed[depID]
			if !exists || (depState != task.Completed && depState != task.Failed && depState != task.Skipped) {
				allDepsSatisfied = false

				break
			}
		}

		if allDepsSatisfied {
			ready = append(ready, *t)
		}
	}

	return ready
}
