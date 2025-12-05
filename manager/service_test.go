package manager

import (
	"testing"
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
