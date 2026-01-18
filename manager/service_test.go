package manager

import (
	"testing"

	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
)

// NOTE: The aggregation methods (aggregateJSONF64, aggregateConcat, aggregateRound)
// referenced in these tests don't exist in the current service implementation.
// Aggregation is now handled by the external FL Coordinator.
// These tests are kept for reference but are skipped.
// TODO: Remove these tests or move them to test aggregation helpers if re-implemented.

func TestAggregateJSONF64(t *testing.T) {
	t.Skip("Aggregation methods removed - aggregation is now handled by FL Coordinator")
}

func TestAggregateConcat(t *testing.T) {
	t.Skip("Aggregation methods removed - aggregation is now handled by FL Coordinator")
}

func TestAggregateRound(t *testing.T) {
	t.Skip("Aggregation methods removed - aggregation is now handled by FL Coordinator")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr))))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Test helper to create a mock service
func createTestService() *service {
	return &service{
		tasksDB:       storage.NewInMemoryStorage(),
		propletsDB:    storage.NewInMemoryStorage(),
		taskPropletDB: storage.NewInMemoryStorage(),
		metricsDB:     storage.NewInMemoryStorage(),
		scheduler:     scheduler.NewRoundRobin(),
	}
}
