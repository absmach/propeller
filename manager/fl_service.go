package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/absmach/propeller/pkg/fl"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/fxamacker/cbor/v2"
)

// RoundState represents the state of a federated learning round
type RoundState struct {
	RoundID   string
	ModelRef  string
	KOfN      int
	TimeoutS  int
	StartTime time.Time
	Updates   []fl.Update
	Completed bool
	mu        sync.Mutex
}

var (
	rounds       = make(map[string]*RoundState)
	roundsMu     sync.RWMutex
	models       = make(map[int]Model)
	modelsMu     sync.RWMutex
	modelVersion = 0
	modelMu      sync.Mutex
	aggregator   fl.Aggregator = fl.NewFedAvgAggregator() // Default to FedAvg
)

// SetAggregator sets the aggregator implementation
func SetAggregator(agg fl.Aggregator) {
	aggregator = agg
}

// init initializes the model registry with a default model
func init() {
	// Initialize with default model v0
	models[0] = Model{
		Version: 0,
		Data: map[string]interface{}{
			"w": []float64{0.0, 0.0, 0.0},
			"b": 0.0,
		},
		Metadata: map[string]interface{}{
			"initial": true,
		},
		CreatedAt: time.Now(),
	}
}

// GetFLTask returns a federated learning task for a client (GET /task)
func (svc *service) GetFLTask(ctx context.Context, roundID, propletID string) (FLTask, error) {
	if roundID == "" {
		return FLTask{}, pkgerrors.ErrInvalidData
	}

	roundsMu.RLock()
	round, exists := rounds[roundID]
	roundsMu.RUnlock()

	if !exists {
		// Initialize round if it doesn't exist
		roundsMu.Lock()
		round = &RoundState{
			RoundID:   roundID,
			ModelRef:  "fl/models/global_model_v0",
			KOfN:      3,
			TimeoutS:  60,
			StartTime: time.Now(),
			Updates:   make([]fl.Update, 0),
			Completed: false,
		}
		rounds[roundID] = round
		roundsMu.Unlock()
		svc.logger.InfoContext(ctx, "Initialized round from task request", "round_id", roundID)
	}

	// Get current model version
	modelMu.Lock()
	currentVersion := modelVersion
	modelMu.Unlock()

	modelRef := fmt.Sprintf("fl/models/global_model_v%d", currentVersion)

	task := FLTask{
		RoundID:  roundID,
		ModelRef: modelRef,
		Config: map[string]interface{}{
			"proplet_id": propletID,
		},
		Hyperparams: map[string]interface{}{
			"epochs":      1,
			"lr":          0.01,
			"batch_size":  16,
		},
	}

	return task, nil
}

// PostFLUpdate receives and buffers a federated learning update (POST /update)
func (svc *service) PostFLUpdate(ctx context.Context, update FLUpdate) error {
	roundID := update.RoundID
	if roundID == "" {
		return pkgerrors.ErrInvalidData
	}

	update.ReceivedAt = time.Now()

	roundsMu.Lock()
	round, exists := rounds[roundID]

	if !exists {
		// Lazy initialization
		modelRef := update.BaseModelURI
		if modelRef == "" {
			modelRef = "fl/models/global_model_v0"
		}

		round = &RoundState{
			RoundID:   roundID,
			ModelRef:   modelRef,
			KOfN:       3,
			TimeoutS:   60,
			StartTime:   time.Now(),
			Updates:    make([]fl.Update, 0),
			Completed:  false,
		}
		rounds[roundID] = round
		svc.logger.InfoContext(ctx, "Lazy initialized round from update", "round_id", roundID)
	}
	roundsMu.Unlock()

	round.mu.Lock()
	defer round.mu.Unlock()

	if round.Completed {
		svc.logger.WarnContext(ctx, "Received update for completed round", "round_id", roundID)
		return fmt.Errorf("round %s is already completed", roundID)
	}

	// Validate update
	if update.Update == nil || len(update.Update) == 0 {
		return fmt.Errorf("update data is empty")
	}

	// Buffer update
	round.Updates = append(round.Updates, update)
	svc.logger.InfoContext(ctx, "Buffered FL update", 
		"round_id", roundID, 
		"proplet_id", update.PropletID,
		"total_updates", len(round.Updates),
		"k_of_n", round.KOfN)

	// Persist round state if storage is available
	if svc.flStorage != nil {
		// Convert to storage format
		storageState := &fl.RoundState{
			RoundID:   round.RoundID,
			ModelRef:  round.ModelRef,
			KOfN:      round.KOfN,
			TimeoutS:  round.TimeoutS,
			StartTime: round.StartTime,
			Updates:   round.Updates,
			Completed: round.Completed,
		}
		if err := svc.flStorage.SaveRound(roundID, storageState); err != nil {
			svc.logger.WarnContext(ctx, "Failed to persist round state", "error", err)
		}
	}

	// Check if we have enough updates for aggregation
	if len(round.Updates) >= round.KOfN {
		svc.logger.InfoContext(ctx, "Round complete: k_of_n reached", 
			"round_id", roundID, 
			"updates", len(round.Updates))
		round.Completed = true
		
		// Trigger aggregation asynchronously
		go svc.aggregateAndAdvance(ctx, round)
	}

	return nil
}

// PostFLUpdateCBOR receives a CBOR-encoded update (POST /update_cbor)
func (svc *service) PostFLUpdateCBOR(ctx context.Context, updateData []byte) error {
	var update FLUpdate
	
	// Decode CBOR
	if err := cbor.Unmarshal(updateData, &update); err != nil {
		return fmt.Errorf("failed to decode CBOR update: %w", err)
	}

	return svc.PostFLUpdate(ctx, update)
}

// GetRoundStatus returns the status of a federated learning round
func (svc *service) GetRoundStatus(ctx context.Context, roundID string) (RoundStatus, error) {
	roundsMu.RLock()
	round, exists := rounds[roundID]
	roundsMu.RUnlock()

	if !exists {
		return RoundStatus{}, fmt.Errorf("round %s not found", roundID)
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	modelMu.Lock()
	currentVersion := modelVersion
	modelMu.Unlock()

	return RoundStatus{
		RoundID:      roundID,
		Completed:    round.Completed,
		NumUpdates:   len(round.Updates),
		KOfN:         round.KOfN,
		ModelVersion: currentVersion,
	}, nil
}

// aggregateAndAdvance performs aggregation and advances to next model version
func (svc *service) aggregateAndAdvance(ctx context.Context, round *RoundState) {
	round.mu.Lock()
	updates := make([]fl.Update, len(round.Updates))
	copy(updates, round.Updates)
	round.mu.Unlock()

	if len(updates) == 0 {
		svc.logger.ErrorContext(ctx, "No updates to aggregate", "round_id", round.RoundID)
		return
	}

	svc.logger.InfoContext(ctx, "Aggregating updates", 
		"round_id", round.RoundID, 
		"num_updates", len(updates))

	// Use aggregator to perform aggregation
	aggregatedModel, err := aggregator.Aggregate(updates)

	if err != nil {
		svc.logger.ErrorContext(ctx, "Aggregation failed", 
			"round_id", round.RoundID, 
			"error", err)
		return
	}

	// Increment model version
	modelMu.Lock()
	modelVersion++
	newVersion := modelVersion
	modelMu.Unlock()

	// Store model
	model := Model{
		Version:   newVersion,
		Data:      aggregatedModel.Data,
		Metadata:  aggregatedModel.Metadata,
		CreatedAt: time.Now(),
	}

	if err := svc.StoreModel(ctx, model); err != nil {
		svc.logger.ErrorContext(ctx, "Failed to store aggregated model", 
			"round_id", round.RoundID, 
			"version", newVersion, 
			"error", err)
		return
	}

	// Persist model if storage is available
	if svc.flStorage != nil {
		storageModel := fl.Model{
			Data:     aggregatedModel.Data,
			Metadata: aggregatedModel.Metadata,
		}
		if err := svc.flStorage.SaveModel(newVersion, storageModel); err != nil {
			svc.logger.WarnContext(ctx, "Failed to persist model", "version", newVersion, "error", err)
		}
	}

	svc.logger.InfoContext(ctx, "Aggregated model stored", 
		"round_id", round.RoundID, 
		"version", newVersion)
}

// loadPersistedState loads rounds and models from persistent storage
func (svc *service) loadPersistedState() {
	if svc.flStorage == nil {
		return
	}

	// Load models
	modelVersions, err := svc.flStorage.ListModels()
	if err == nil {
		for _, version := range modelVersions {
			storageModel, err := svc.flStorage.LoadModel(version)
			if err == nil {
				modelsMu.Lock()
				models[version] = Model{
					Version:   version,
					Data:      storageModel.Data,
					Metadata:  storageModel.Metadata,
					CreatedAt: time.Now(), // Approximate
				}
				if version > modelVersion {
					modelVersion = version
				}
				modelsMu.Unlock()
				svc.logger.Info("Loaded persisted model", "version", version)
			}
		}
	}

	// Load rounds (for recovery)
	roundIDs, err := svc.flStorage.ListRounds()
	if err == nil {
		for _, roundID := range roundIDs {
			storageState, err := svc.flStorage.LoadRound(roundID)
			if err == nil && !storageState.Completed {
				roundsMu.Lock()
				rounds[roundID] = &RoundState{
					RoundID:   storageState.RoundID,
					ModelRef:  storageState.ModelRef,
					KOfN:      storageState.KOfN,
					TimeoutS:  storageState.TimeoutS,
					StartTime: storageState.StartTime,
					Updates:   storageState.Updates,
					Completed: storageState.Completed,
				}
				roundsMu.Unlock()
				svc.logger.Info("Loaded persisted round", "round_id", roundID)
			}
		}
	}
}


// GetModel retrieves a model from the registry
func (svc *service) GetModel(ctx context.Context, version int) (Model, error) {
	modelsMu.RLock()
	model, exists := models[version]
	modelsMu.RUnlock()

	if !exists {
		// Try to load from persistent storage
		if svc.flStorage != nil {
			storageModel, err := svc.flStorage.LoadModel(version)
			if err == nil {
				model = Model{
					Version:   version,
					Data:      storageModel.Data,
					Metadata:  storageModel.Metadata,
					CreatedAt: time.Now(),
				}
				// Cache in memory
				modelsMu.Lock()
				models[version] = model
				modelsMu.Unlock()
				return model, nil
			}
		}
		return Model{}, fmt.Errorf("model version %d not found", version)
	}

	return model, nil
}

// StoreModel stores a model in the registry
func (svc *service) StoreModel(ctx context.Context, model Model) error {
	modelsMu.Lock()
	models[model.Version] = model
	modelsMu.Unlock()

	svc.logger.InfoContext(ctx, "Model stored in registry", "version", model.Version)
	return nil
}

// ListModels returns all available model versions
func (svc *service) ListModels(ctx context.Context) ([]int, error) {
	modelsMu.RLock()
	versions := make([]int, 0, len(models))
	for v := range models {
		versions = append(versions, v)
	}
	modelsMu.RUnlock()

	return versions, nil
}

// checkRoundTimeouts checks for round timeouts and triggers aggregation if needed
func (svc *service) checkRoundTimeouts() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		roundsMu.RLock()
		for roundID, round := range rounds {
			round.mu.Lock()
			if !round.Completed {
				elapsed := now.Sub(round.StartTime)
				if elapsed >= time.Duration(round.TimeoutS)*time.Second {
					svc.logger.Warn("Round timeout exceeded",
						"round_id", roundID,
						"timeout_s", round.TimeoutS,
						"updates", len(round.Updates))
					round.Completed = true
					if len(round.Updates) > 0 {
						go svc.aggregateAndAdvance(context.Background(), round)
					}
				}
			}
			round.mu.Unlock()
		}
		roundsMu.RUnlock()
	}
}
