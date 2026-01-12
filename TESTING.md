# Testing Guide

This document describes the test suite for Propeller's Federated Learning implementation.

## Unit Tests

### FL Aggregation Tests (`manager/service_test.go`)

Tests for the FedAvg aggregation algorithm:

- **TestAggregateJSONF64**: Tests weighted averaging of model updates in JSON-F64 format
  - Simple weighted average with different sample counts
  - Equal weights scenario
  - Single update scenario
  - Error cases: zero samples, mismatched dimensions, empty vectors, invalid base64/json

- **TestAggregateConcat**: Tests concatenation-based aggregation for custom formats
  - Verifies updates are concatenated with proper delimiter
  - Validates metadata (job_id, round_id, total_samples)

- **TestAggregateRound**: Tests the aggregation router
  - Routes to json-f64 aggregator for json-f64 format
  - Routes to concat aggregator for other formats

### Running Unit Tests

```bash
# Run all aggregation tests
go test -v ./manager -run TestAggregate

# Run specific test
go test -v ./manager -run TestAggregateJSONF64
```

## Integration Tests

### FL Workflow Integration Test (`manager/fl_integration_test.go`)

**TestFLWorkflowIntegration** tests the complete FL workflow end-to-end:

1. **Setup**: Creates mock storage and MQTT pub/sub
2. **Proplet Registration**: Registers two proplets
3. **Task Creation**: Creates a federated learning task with FL spec
4. **Round 1 Tasks**: Creates round 1 tasks for each proplet
5. **Update Simulation**: Simulates proplets completing training and sending updates
6. **Aggregation Verification**: Verifies FedAvg aggregation produces correct weighted average
7. **State Verification**: Verifies tasks are marked as completed

### Running Integration Tests

```bash
# Run integration test
go test -v ./manager -run TestFLWorkflowIntegration

# Run with timeout
go test -v ./manager -run TestFLWorkflowIntegration -timeout 30s
```

## Test Coverage

The test suite covers:

- ✅ FedAvg weighted averaging (correctness)
- ✅ Update format validation
- ✅ Error handling (invalid inputs, mismatched dimensions)
- ✅ Round progression logic
- ✅ Task state management
- ✅ Update envelope validation
- ✅ End-to-end workflow (proplet → manager → aggregation)

## Mock Components

### mockPubSub

A simple in-memory MQTT pub/sub implementation for testing:
- Stores published messages
- Routes messages to subscribed handlers
- Supports wildcard topic matching

## Future Test Additions

Potential areas for additional testing:

- Round timeout handling
- Partial round completion (less than min_clients)
- Multiple concurrent FL jobs
- Model distribution via Proxy
- Rust proplet FL execution
- Network failure scenarios
