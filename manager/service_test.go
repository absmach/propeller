package manager

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"testing"

	flpkg "github.com/absmach/propeller/pkg/fl"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	smqerrors "github.com/absmach/supermq/pkg/errors"
)

func TestAggregateJSONF64(t *testing.T) {
	svc := &service{
		logger: nil, // Not needed for aggregation tests
	}

	tests := []struct {
		name          string
		updates       []flpkg.UpdateEnvelope
		totalSamples  uint64
		expectedError string
		validate      func(t *testing.T, result flpkg.UpdateEnvelope)
	}{
		{
			name: "simple weighted average",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 10,
					UpdateB64:  encodeJSON([]float64{1.0, 2.0, 3.0}),
					Format:     "json-f64",
				},
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p2",
					NumSamples: 20,
					UpdateB64:  encodeJSON([]float64{2.0, 3.0, 4.0}),
					Format:     "json-f64",
				},
			},
			totalSamples: 30,
			validate: func(t *testing.T, result flpkg.UpdateEnvelope) {
				decoded, err := base64.StdEncoding.DecodeString(result.UpdateB64)
				if err != nil {
					wrappedErr := smqerrors.Wrap(smqerrors.New("failed to decode base64 result"), err)
					t.Fatalf("Failed to decode result: %v", wrappedErr)
				}

				var weights []float64
				err = json.Unmarshal(decoded, &weights)
				if err != nil {
					wrappedErr := smqerrors.Wrap(smqerrors.New("failed to unmarshal weights from JSON"), err)
					t.Fatalf("Failed to unmarshal weights: %v", wrappedErr)
				}

				// Expected: (1*10 + 2*20)/30, (2*10 + 3*20)/30, (3*10 + 4*20)/30
				// = (10+40)/30, (20+60)/30, (30+80)/30
				// = 50/30, 80/30, 110/30
				// = 1.666..., 2.666..., 3.666...
				if math.Abs(weights[0]-1.6666666666666667) > 0.0001 {
					t.Errorf("Expected weights[0] ≈ 1.6667, got %f", weights[0])
				}
				if math.Abs(weights[1]-2.6666666666666667) > 0.0001 {
					t.Errorf("Expected weights[1] ≈ 2.6667, got %f", weights[1])
				}
				if math.Abs(weights[2]-3.6666666666666667) > 0.0001 {
					t.Errorf("Expected weights[2] ≈ 3.6667, got %f", weights[2])
				}
				if result.NumSamples != 30 {
					t.Errorf("Expected NumSamples=30, got %d", result.NumSamples)
				}
				if result.Format != "json-f64" {
					t.Errorf("Expected Format=json-f64, got %s", result.Format)
				}
				if result.PropletID != "manager" {
					t.Errorf("Expected PropletID=manager, got %s", result.PropletID)
				}
			},
		},
		{
			name: "equal weights",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 5,
					UpdateB64:  encodeJSON([]float64{1.0, 2.0}),
					Format:     "json-f64",
				},
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p2",
					NumSamples: 5,
					UpdateB64:  encodeJSON([]float64{3.0, 4.0}),
					Format:     "json-f64",
				},
			},
			totalSamples: 10,
			validate: func(t *testing.T, result flpkg.UpdateEnvelope) {
				decoded, err := base64.StdEncoding.DecodeString(result.UpdateB64)
				if err != nil {
					wrappedErr := smqerrors.Wrap(smqerrors.New("failed to decode base64 result"), err)
					t.Fatalf("Failed to decode result: %v", wrappedErr)
				}

				var weights []float64
				err = json.Unmarshal(decoded, &weights)
				if err != nil {
					wrappedErr := smqerrors.Wrap(smqerrors.New("failed to unmarshal weights from JSON"), err)
					t.Fatalf("Failed to unmarshal weights: %v", wrappedErr)
				}

				// Expected: (1+3)/2, (2+4)/2 = 2.0, 3.0
				if math.Abs(weights[0]-2.0) > 0.0001 {
					t.Errorf("Expected weights[0] ≈ 2.0, got %f", weights[0])
				}
				if math.Abs(weights[1]-3.0) > 0.0001 {
					t.Errorf("Expected weights[1] ≈ 3.0, got %f", weights[1])
				}
			},
		},
		{
			name: "single update",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 100,
					UpdateB64:  encodeJSON([]float64{5.0, 6.0, 7.0}),
					Format:     "json-f64",
				},
			},
			totalSamples: 100,
			validate: func(t *testing.T, result flpkg.UpdateEnvelope) {
				decoded, err := base64.StdEncoding.DecodeString(result.UpdateB64)
				if err != nil {
					wrappedErr := smqerrors.Wrap(smqerrors.New("failed to decode base64 result"), err)
					t.Fatalf("Failed to decode result: %v", wrappedErr)
				}

				var weights []float64
				err = json.Unmarshal(decoded, &weights)
				if err != nil {
					wrappedErr := smqerrors.Wrap(smqerrors.New("failed to unmarshal weights from JSON"), err)
					t.Fatalf("Failed to unmarshal weights: %v", wrappedErr)
				}

				if len(weights) != 3 || weights[0] != 5.0 || weights[1] != 6.0 || weights[2] != 7.0 {
					t.Errorf("Expected weights=[5.0, 6.0, 7.0], got %v", weights)
				}
			},
		},
		{
			name: "zero total samples",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 0,
					UpdateB64:  encodeJSON([]float64{1.0}),
					Format:     "json-f64",
				},
			},
			totalSamples:  0,
			expectedError: "cannot aggregate: total_samples is zero",
		},
		{
			name: "mismatched dimensions",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 10,
					UpdateB64:  encodeJSON([]float64{1.0, 2.0}),
					Format:     "json-f64",
				},
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p2",
					NumSamples: 20,
					UpdateB64:  encodeJSON([]float64{3.0, 4.0, 5.0}),
					Format:     "json-f64",
				},
			},
			totalSamples:  30,
			expectedError: "cannot aggregate: mismatched vector dimensions",
		},
		{
			name: "empty vector",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 10,
					UpdateB64:  encodeJSON([]float64{}),
					Format:     "json-f64",
				},
			},
			totalSamples:  10,
			expectedError: "invalid vector: empty",
		},
		{
			name: "invalid base64",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 10,
					UpdateB64:  "invalid-base64!@#$",
					Format:     "json-f64",
				},
			},
			totalSamples:  10,
			expectedError: "invalid update_b64",
		},
		{
			name: "invalid json",
			updates: []flpkg.UpdateEnvelope{
				{
					JobID:      "job1",
					RoundID:    1,
					PropletID:  "p1",
					NumSamples: 10,
					UpdateB64:  base64.StdEncoding.EncodeToString([]byte("not json")),
					Format:     "json-f64",
				},
			},
			totalSamples:  10,
			expectedError: "invalid json-f64 payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.aggregateJSONF64("job1", 1, tt.updates, tt.totalSamples, "v1")

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error containing '%s', got nil", tt.expectedError)
				}
				if !contains(err.Error(), tt.expectedError) {
					t.Fatalf("Expected error containing '%s', got '%s'", tt.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.JobID != "job1" {
				t.Errorf("Expected JobID=job1, got %s", result.JobID)
			}
			if result.RoundID != 1 {
				t.Errorf("Expected RoundID=1, got %d", result.RoundID)
			}
			if result.GlobalVersion != "v1" {
				t.Errorf("Expected GlobalVersion=v1, got %s", result.GlobalVersion)
			}
			if result.Metrics["num_clients"].(int) != len(tt.updates) {
				t.Errorf("Expected num_clients=%d, got %d", len(tt.updates), result.Metrics["num_clients"].(int))
			}
			if result.Metrics["total_samples"].(int64) != int64(tt.totalSamples) {
				t.Errorf("Expected total_samples=%d, got %d", tt.totalSamples, result.Metrics["total_samples"].(int64))
			}

			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestAggregateConcat(t *testing.T) {
	svc := &service{
		logger: nil,
	}

	updates := []flpkg.UpdateEnvelope{
		{
			JobID:      "job1",
			RoundID:    1,
			PropletID:  "p1",
			NumSamples: 10,
			UpdateB64:  base64.StdEncoding.EncodeToString([]byte("update1")),
			Format:     "custom",
		},
		{
			JobID:      "job1",
			RoundID:    1,
			PropletID:  "p2",
			NumSamples: 20,
			UpdateB64:  base64.StdEncoding.EncodeToString([]byte("update2")),
			Format:     "custom",
		},
	}

	result, err := svc.aggregateConcat("job1", 1, updates, 30, "v1", "custom")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.JobID != "job1" {
		t.Errorf("Expected JobID=job1, got %s", result.JobID)
	}
	if result.RoundID != 1 {
		t.Errorf("Expected RoundID=1, got %d", result.RoundID)
	}
	if result.GlobalVersion != "v1" {
		t.Errorf("Expected GlobalVersion=v1, got %s", result.GlobalVersion)
	}
	if result.NumSamples != 30 {
		t.Errorf("Expected NumSamples=30, got %d", result.NumSamples)
	}
	if result.Format != "custom" {
		t.Errorf("Expected Format=custom, got %s", result.Format)
	}

	decoded, err := base64.StdEncoding.DecodeString(result.UpdateB64)
	if err != nil {
		wrappedErr := smqerrors.Wrap(smqerrors.New("failed to decode base64 result"), err)
		t.Fatalf("Failed to decode result: %v", wrappedErr)
	}

	// Should contain both updates separated by delimiter
	decodedStr := string(decoded)
	if !contains(decodedStr, "update1") {
		t.Errorf("Expected decoded string to contain 'update1', got: %s", decodedStr)
	}
	if !contains(decodedStr, "update2") {
		t.Errorf("Expected decoded string to contain 'update2', got: %s", decodedStr)
	}
}

func TestAggregateRound(t *testing.T) {
	svc := &service{
		logger: nil,
	}

	t.Run("json-f64 format", func(t *testing.T) {
		updates := []flpkg.UpdateEnvelope{
			{
				JobID:      "job1",
				RoundID:    1,
				PropletID:  "p1",
				NumSamples: 10,
				UpdateB64:  encodeJSON([]float64{1.0, 2.0}),
				Format:     "json-f64",
			},
		}

		result, err := svc.aggregateRound("job1", 1, updates, "json-f64", 10)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result.Format != "json-f64" {
			t.Errorf("Expected Format=json-f64, got %s", result.Format)
		}
	})

	t.Run("other format", func(t *testing.T) {
		updates := []flpkg.UpdateEnvelope{
			{
				JobID:      "job1",
				RoundID:    1,
				PropletID:  "p1",
				NumSamples: 10,
				UpdateB64:  base64.StdEncoding.EncodeToString([]byte("data")),
				Format:     "custom",
			},
		}

		result, err := svc.aggregateRound("job1", 1, updates, "custom", 10)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result.Format != "custom" {
			t.Errorf("Expected Format=custom, got %s", result.Format)
		}
	})
}

func encodeJSON(data []float64) string {
	jsonData, _ := json.Marshal(data)
	return base64.StdEncoding.EncodeToString(jsonData)
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
		aggregated:    make(map[string]bool),
	}
}
