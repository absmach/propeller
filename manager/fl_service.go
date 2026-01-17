package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/fxamacker/cbor/v2"
)

// ConfigureExperiment configures an FL experiment with the FL Coordinator (Step 1 in diagram)
// Manager acts as Orchestrator/Experiment Config and sends configuration to FL Coordinator
// After configuration, Manager can optionally trigger round start to orchestrate WASM execution
func (svc *service) ConfigureExperiment(ctx context.Context, config ExperimentConfig) error {
	if config.RoundID == "" {
		return pkgerrors.ErrInvalidData
	}

	// Validate required fields for orchestration
	if len(config.Participants) == 0 {
		return fmt.Errorf("participants list is required for orchestration")
	}
	if config.TaskWasmImage == "" {
		return fmt.Errorf("task_wasm_image is required for WASM orchestration")
	}
	if config.ModelRef == "" {
		return fmt.Errorf("model_ref is required")
	}

	// Send experiment configuration to FL Coordinator via HTTP
	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return fmt.Errorf("FL_COORDINATOR_URL must be configured for HTTP-based FL coordination")
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal experiment config: %w", err)
	}

	// Send to HTTP coordinator
	url := fmt.Sprintf("%s/experiments", svc.flCoordinatorURL)
	resp, err := svc.httpClient.Post(url, "application/json", bytes.NewBuffer(configJSON))
	if err != nil {
		return fmt.Errorf("failed to configure experiment with HTTP coordinator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("HTTP coordinator returned error: %d", resp.StatusCode)
	}

	svc.logger.InfoContext(ctx, "Configured experiment with FL Coordinator",
		"experiment_id", config.ExperimentID,
		"round_id", config.RoundID)

	// After successful configuration, trigger round start to orchestrate WASM execution
	// This publishes to fl/rounds/start which handleRoundStart will process
	// Note: This is Manager's internal orchestration mechanism, not part of FL protocol
	roundStartMsg := map[string]any{
		"round_id":        config.RoundID,
		"model_uri":       config.ModelRef,
		"task_wasm_image": config.TaskWasmImage,
		"participants":    config.Participants,
		"hyperparams":     config.Hyperparams,
	}

	if err := svc.pubsub.Publish(ctx, "fl/rounds/start", roundStartMsg); err != nil {
		svc.logger.WarnContext(ctx, "Failed to trigger round start after configuration",
			"round_id", config.RoundID, "error", err)
		// Don't fail the configuration if round start trigger fails
	} else {
		svc.logger.InfoContext(ctx, "Triggered round start for orchestration",
			"round_id", config.RoundID,
			"participants", len(config.Participants))
	}

	return nil
}

// GetFLTask forwards task request to FL Coordinator
// 
// IMPORTANT: Clients MUST call FL Coordinator directly (Step 3 in workflow diagram)
// This endpoint exists ONLY for compatibility scenarios
// 
// Correct client flow: Client → FL Coordinator (GET /task)
// NOT: Client → Manager → FL Coordinator
func (svc *service) GetFLTask(ctx context.Context, roundID, propletID string) (FLTask, error) {
	if roundID == "" {
		return FLTask{}, pkgerrors.ErrInvalidData
	}

	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return FLTask{}, fmt.Errorf("FL_COORDINATOR_URL must be configured")
	}

	url := fmt.Sprintf("%s/task?round_id=%s&proplet_id=%s", svc.flCoordinatorURL, roundID, propletID)
	resp, err := svc.httpClient.Get(url)
	if err != nil {
		return FLTask{}, fmt.Errorf("failed to forward task request to coordinator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return FLTask{}, fmt.Errorf("coordinator returned error: %d", resp.StatusCode)
	}

	var taskResp struct {
		Task FLTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return FLTask{}, fmt.Errorf("failed to decode coordinator response: %w", err)
	}

	svc.logger.InfoContext(ctx, "Forwarded FL task request to coordinator", "round_id", roundID, "proplet_id", propletID)
	return taskResp.Task, nil
}

// PostFLUpdate forwards update to FL Coordinator
//
// IMPORTANT: Clients MUST call FL Coordinator directly (Step 7 in workflow diagram)
// This endpoint exists ONLY for compatibility scenarios
//
// Correct client flow: Client → FL Coordinator (POST /update)
// NOT: Client → Manager → FL Coordinator
func (svc *service) PostFLUpdate(ctx context.Context, update FLUpdate) error {
	if update.RoundID == "" {
		return pkgerrors.ErrInvalidData
	}

	// Validate update has data
	if update.Update == nil || len(update.Update) == 0 {
		return fmt.Errorf("update data is empty")
	}

	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return fmt.Errorf("FL_COORDINATOR_URL must be configured")
	}

	// Forward to HTTP coordinator
	updateJSON, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal update: %w", err)
	}

	url := fmt.Sprintf("%s/update", svc.flCoordinatorURL)
	resp, err := svc.httpClient.Post(url, "application/json", bytes.NewBuffer(updateJSON))
	if err != nil {
		return fmt.Errorf("failed to forward update to HTTP coordinator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP coordinator returned error: %d", resp.StatusCode)
	}

	svc.logger.InfoContext(ctx, "Forwarded FL update to HTTP coordinator",
		"round_id", update.RoundID,
		"proplet_id", update.PropletID)
	return nil
}

// PostFLUpdateCBOR forwards CBOR-encoded update to FL Coordinator
func (svc *service) PostFLUpdateCBOR(ctx context.Context, updateData []byte) error {
	var update FLUpdate

	// Decode CBOR
	if err := cbor.Unmarshal(updateData, &update); err != nil {
		return fmt.Errorf("failed to decode CBOR update: %w", err)
	}

	return svc.PostFLUpdate(ctx, update)
}

// GetRoundStatus forwards round status request to FL Coordinator
func (svc *service) GetRoundStatus(ctx context.Context, roundID string) (RoundStatus, error) {
	if roundID == "" {
		return RoundStatus{}, pkgerrors.ErrInvalidData
	}

	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return RoundStatus{}, fmt.Errorf("FL_COORDINATOR_URL must be configured")
	}

	url := fmt.Sprintf("%s/rounds/%s/complete", svc.flCoordinatorURL, roundID)
	resp, err := svc.httpClient.Get(url)
	if err != nil {
		return RoundStatus{}, fmt.Errorf("failed to forward round status request to coordinator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RoundStatus{}, fmt.Errorf("coordinator returned error: %d", resp.StatusCode)
	}

	var statusResp struct {
		Status RoundStatus `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return RoundStatus{}, fmt.Errorf("failed to decode coordinator response: %w", err)
	}

	svc.logger.InfoContext(ctx, "Forwarded round status request to coordinator", "round_id", roundID)
	return statusResp.Status, nil
}

// Note: Round completion notifications are handled by the FL Coordinator directly.
// Coordinators should publish MQTT notifications to "fl/rounds/next" topic when rounds complete.
// This keeps the architecture simple: Coordinator → MQTT Broker → Clients
// See ROUND_COMPLETION_NOTIFICATION_FLOW.md for details.
