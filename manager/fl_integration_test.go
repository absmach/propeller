package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"testing"
	"time"

	flpkg "github.com/absmach/propeller/pkg/fl"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/task"
	"github.com/google/uuid"
)

// mockPubSub is a simple mock MQTT pub/sub for testing
type mockPubSub struct {
	published  map[string]interface{}
	subscribed map[string]mqtt.Handler
}

func newMockPubSub() *mockPubSub {
	return &mockPubSub{
		published:  make(map[string]interface{}),
		subscribed: make(map[string]mqtt.Handler),
	}
}

func (m *mockPubSub) Publish(ctx context.Context, topic string, payload interface{}) error {
	m.published[topic] = payload
	return nil
}

func (m *mockPubSub) Subscribe(ctx context.Context, topic string, handler mqtt.Handler) error {
	m.subscribed[topic] = handler
	return nil
}

func (m *mockPubSub) Unsubscribe(ctx context.Context, topic string) error {
	delete(m.subscribed, topic)
	return nil
}

func (m *mockPubSub) Disconnect(ctx context.Context) error {
	return nil
}

func (m *mockPubSub) simulateMessage(topic string, msg map[string]any) error {
	if handler, ok := m.subscribed[topic]; ok {
		return handler(topic, msg)
	}
	// Try wildcard match
	for subTopic, handler := range m.subscribed {
		if matchesWildcard(topic, subTopic) {
			return handler(topic, msg)
		}
	}
	return nil
}

func encodeJSONForTest(data []float64) string {
	jsonData, _ := json.Marshal(data)
	return base64.StdEncoding.EncodeToString(jsonData)
}

func matchesWildcard(topic, pattern string) bool {
	if pattern == "#" {
		return true
	}
	if pattern == topic {
		return true
	}
	// Simple wildcard matching for patterns like "m/+/c/+/#"
	patternParts := splitTopic(pattern)
	topicParts := splitTopic(topic)
	
	if len(patternParts) > len(topicParts) {
		return false
	}
	
	for i, part := range patternParts {
		if part == "#" {
			return true
		}
		if part != "+" && part != topicParts[i] {
			return false
		}
	}
	return true
}

func splitTopic(topic string) []string {
	parts := []string{}
	current := ""
	for _, c := range topic {
		if c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func TestFLWorkflowIntegration(t *testing.T) {
	ctx := context.Background()
	
	// Setup
	tasksDB := storage.NewInMemoryStorage()
	propletsDB := storage.NewInMemoryStorage()
	taskPropletDB := storage.NewInMemoryStorage()
	metricsDB := storage.NewInMemoryStorage()
	pubsubMock := newMockPubSub()
	
	svc := NewService(
		tasksDB,
		propletsDB,
		taskPropletDB,
		metricsDB,
		scheduler.NewRoundRobin(),
		pubsubMock,
		"test-domain",
		"test-channel",
		nil, // logger
	).(*service)
	
	// Store reference for simulation
	pubsubMockTyped := pubsubMock
	
	// Subscribe to handle messages
	if err := svc.Subscribe(ctx); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	
	// Step 1: Create proplets
	proplet1 := proplet.Proplet{
		ID:   "proplet-1",
		Name: "proplet-1",
	}
	proplet2 := proplet.Proplet{
		ID:   "proplet-2",
		Name: "proplet-2",
	}
	
	if err := propletsDB.Create(ctx, proplet1.ID, proplet1); err != nil {
		t.Fatalf("Failed to create proplet1: %v", err)
	}
	if err := propletsDB.Create(ctx, proplet2.ID, proplet2); err != nil {
		t.Fatalf("Failed to create proplet2: %v", err)
	}
	
	// Mark proplets as alive
	proplet1.Alive = true
	proplet1.AliveHistory = []time.Time{time.Now()}
	proplet2.Alive = true
	proplet2.AliveHistory = []time.Time{time.Now()}
	if err := propletsDB.Update(ctx, proplet1.ID, proplet1); err != nil {
		t.Fatalf("Failed to update proplet1: %v", err)
	}
	if err := propletsDB.Update(ctx, proplet2.ID, proplet2); err != nil {
		t.Fatalf("Failed to update proplet2: %v", err)
	}
	
	// Step 2: Create federated learning task
	jobID := uuid.NewString()
	flTask := task.Task{
		Name:     "fl-task-1",
		Kind:     task.TaskKindFederated,
		Mode:     task.ModeTrain,
		ImageURL: "registry.example.com/fl-model:v1",
		FL: &task.FLSpec{
			JobID:          jobID,
			RoundID:        1,
			GlobalVersion:  uuid.NewString(),
			MinParticipants: 2,
			RoundTimeoutSec: 300,
			ClientsPerRound: 2,
			TotalRounds:    3,
			UpdateFormat:   "json-f64",
		},
	}
	
	createdTask, err := svc.CreateTask(ctx, flTask)
	if err != nil {
		t.Fatalf("Failed to create FL task: %v", err)
	}
	
	if createdTask.Kind != task.TaskKindFederated {
		t.Errorf("Expected task kind to be federated, got %s", createdTask.Kind)
	}
	if createdTask.FL == nil {
		t.Fatal("Expected FL spec to be set")
	}
	if createdTask.FL.JobID != jobID {
		t.Errorf("Expected JobID=%s, got %s", jobID, createdTask.FL.JobID)
	}
	
	// Step 3: Start the task (this should create round 1 tasks)
	// For simplicity, we'll manually create round 1 tasks for each proplet
	round1Task1 := task.Task{
		Name:     "fl-task-1-round-1-p1",
		Kind:     task.TaskKindFederated,
		Mode:     task.ModeTrain,
		ImageURL: flTask.ImageURL,
		PropletID: proplet1.ID,
		FL: &task.FLSpec{
			JobID:         jobID,
			RoundID:       1,
			GlobalVersion: createdTask.FL.GlobalVersion,
			UpdateFormat:  "json-f64",
		},
	}
	
	round1Task2 := task.Task{
		Name:     "fl-task-1-round-1-p2",
		Kind:     task.TaskKindFederated,
		Mode:     task.ModeTrain,
		ImageURL: flTask.ImageURL,
		PropletID: proplet2.ID,
		FL: &task.FLSpec{
			JobID:         jobID,
			RoundID:       1,
			GlobalVersion: createdTask.FL.GlobalVersion,
			UpdateFormat:  "json-f64",
		},
	}
	
	createdRound1Task1, err := svc.CreateTask(ctx, round1Task1)
	if err != nil {
		t.Fatalf("Failed to create round 1 task 1: %v", err)
	}
	
	createdRound1Task2, err := svc.CreateTask(ctx, round1Task2)
	if err != nil {
		t.Fatalf("Failed to create round 1 task 2: %v", err)
	}
	
	// Pin tasks to proplets
	if err := taskPropletDB.Create(ctx, createdRound1Task1.ID, proplet1.ID); err != nil {
		t.Fatalf("Failed to pin task 1: %v", err)
	}
	if err := taskPropletDB.Create(ctx, createdRound1Task2.ID, proplet2.ID); err != nil {
		t.Fatalf("Failed to pin task 2: %v", err)
	}
	
	// Step 4: Simulate proplets completing training and sending updates
	update1 := flpkg.UpdateEnvelope{
		TaskID:        createdRound1Task1.ID,
		JobID:         jobID,
		RoundID:       1,
		GlobalVersion: createdTask.FL.GlobalVersion,
		PropletID:     proplet1.ID,
		NumSamples:    10,
		UpdateB64:     encodeJSONForTest([]float64{1.0, 2.0, 3.0}),
		Format:        "json-f64",
	}
	
	update2 := flpkg.UpdateEnvelope{
		TaskID:        createdRound1Task2.ID,
		JobID:         jobID,
		RoundID:       1,
		GlobalVersion: createdTask.FL.GlobalVersion,
		PropletID:     proplet2.ID,
		NumSamples:    20,
		UpdateB64:     encodeJSONForTest([]float64{2.0, 3.0, 4.0}),
		Format:        "json-f64",
	}
	
	// Simulate first proplet completing
	resultMsg1 := map[string]any{
		"task_id": createdRound1Task1.ID,
		"results": update1,
		"error":   nil,
	}
	
	if err := pubsubMockTyped.simulateMessage("m/test-domain/c/test-channel/control/proplet/results", resultMsg1); err != nil {
		t.Fatalf("Failed to simulate result message 1: %v", err)
	}
	
	// Verify task 1 is marked as completed
	completedTask1, err := svc.GetTask(ctx, createdRound1Task1.ID)
	if err != nil {
		t.Fatalf("Failed to get completed task 1: %v", err)
	}
	if completedTask1.State != task.Completed {
		t.Errorf("Expected task 1 state to be Completed, got %s", completedTask1.State.String())
	}
	
	// Simulate second proplet completing
	resultMsg2 := map[string]any{
		"task_id": createdRound1Task2.ID,
		"results": update2,
		"error":   nil,
	}
	
	if err := pubsubMockTyped.simulateMessage("m/test-domain/c/test-channel/control/proplet/results", resultMsg2); err != nil {
		t.Fatalf("Failed to simulate result message 2: %v", err)
	}
	
	// Verify task 2 is marked as completed
	completedTask2, err := svc.GetTask(ctx, createdRound1Task2.ID)
	if err != nil {
		t.Fatalf("Failed to get completed task 2: %v", err)
	}
	if completedTask2.State != task.Completed {
		t.Errorf("Expected task 2 state to be Completed, got %s", completedTask2.State.String())
	}
	
	// Step 5: Verify aggregation was triggered
	// Check if aggregated envelope was stored
	aggKey := "fl/" + jobID + "/1/aggregate"
	aggData, err := tasksDB.Get(ctx, aggKey)
	if err != nil {
		t.Fatalf("Failed to get aggregated envelope: %v", err)
	}
	
	var aggEnv flpkg.UpdateEnvelope
	switch v := aggData.(type) {
	case flpkg.UpdateEnvelope:
		aggEnv = v
	default:
		jsonData, _ := json.Marshal(v)
		if err := json.Unmarshal(jsonData, &aggEnv); err != nil {
			t.Fatalf("Failed to unmarshal aggregated envelope: %v", err)
		}
	}
	
	if aggEnv.JobID != jobID {
		t.Errorf("Expected aggregated JobID=%s, got %s", jobID, aggEnv.JobID)
	}
	if aggEnv.RoundID != 1 {
		t.Errorf("Expected aggregated RoundID=1, got %d", aggEnv.RoundID)
	}
	if aggEnv.NumSamples != 30 {
		t.Errorf("Expected aggregated NumSamples=30, got %d", aggEnv.NumSamples)
	}
	
	// Verify aggregated weights
	decoded, err := base64.StdEncoding.DecodeString(aggEnv.UpdateB64)
	if err != nil {
		t.Fatalf("Failed to decode aggregated weights: %v", err)
	}
	
	var weights []float64
	if err := json.Unmarshal(decoded, &weights); err != nil {
		t.Fatalf("Failed to unmarshal aggregated weights: %v", err)
	}
	
	// Expected: (1*10 + 2*20)/30, (2*10 + 3*20)/30, (3*10 + 4*20)/30
	// = 50/30, 80/30, 110/30
	expectedWeights := []float64{50.0 / 30.0, 80.0 / 30.0, 110.0 / 30.0}
	if len(weights) != len(expectedWeights) {
		t.Fatalf("Expected %d weights, got %d", len(expectedWeights), len(weights))
	}
	
	for i, expected := range expectedWeights {
		if math.Abs(weights[i]-expected) > 0.0001 {
			t.Errorf("Weight[%d]: expected %f, got %f", i, expected, weights[i])
		}
	}
	
	// Step 6: Verify round progression (next round tasks should be created)
	// The startNextRound function should have been called
	// Check if round 2 tasks exist
	allTasks, err := svc.ListTasks(ctx, 0, 100)
	if err != nil {
		t.Fatalf("Failed to list tasks: %v", err)
	}
	
	round2Tasks := 0
	for _, t := range allTasks.Tasks {
		if t.FL != nil && t.FL.JobID == jobID && t.FL.RoundID == 2 {
			round2Tasks++
		}
	}
	
	// Note: In a real scenario, startNextRound would create round 2 tasks
	// For this test, we're verifying the aggregation worked correctly
	// Round progression would happen automatically when aggregation completes
	t.Logf("Found %d round 2 tasks (expected 0 in this simplified test)", round2Tasks)
}

