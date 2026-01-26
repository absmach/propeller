package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/fxamacker/cbor/v2"
)

func (svc *service) ConfigureExperiment(ctx context.Context, config ExperimentConfig) error {
	if config.RoundID == "" {
		return pkgerrors.ErrInvalidData
	}

	if len(config.Participants) == 0 {
		return errors.New("participants list is required for orchestration")
	}
	if config.TaskWasmImage == "" {
		return errors.New("task_wasm_image is required for WASM orchestration")
	}
	if config.ModelRef == "" {
		return errors.New("model_ref is required")
	}

	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return errors.New("COORDINATOR_URL must be configured for HTTP-based FL coordination")
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal experiment config: %w", err)
	}

	url := svc.flCoordinatorURL + "/experiments"
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

	roundStartMsg := map[string]any{
		"round_id":        config.RoundID,
		"model_uri":       config.ModelRef,
		"task_wasm_image": config.TaskWasmImage,
		"participants":    config.Participants,
		"hyperparams":     config.Hyperparams,
	}

	topic := svc.baseTopic + "/fl/rounds/start"
	if err := svc.pubsub.Publish(ctx, topic, roundStartMsg); err != nil {
		svc.logger.WarnContext(ctx, "Failed to trigger round start after configuration",
			"round_id", config.RoundID, "error", err)
	} else {
		svc.logger.InfoContext(ctx, "Triggered round start for orchestration",
			"round_id", config.RoundID,
			"participants", len(config.Participants))
	}

	return nil
}

func (svc *service) GetFLTask(ctx context.Context, roundID, propletID string) (FLTask, error) {
	if roundID == "" {
		return FLTask{}, pkgerrors.ErrInvalidData
	}

	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return FLTask{}, errors.New("COORDINATOR_URL must be configured")
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

func (svc *service) PostFLUpdate(ctx context.Context, update FLUpdate) error {
	if update.RoundID == "" {
		return pkgerrors.ErrInvalidData
	}

	if len(update.Update) == 0 {
		return errors.New("update data is empty")
	}

	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return errors.New("COORDINATOR_URL must be configured")
	}

	updateJSON, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal update: %w", err)
	}

	url := svc.flCoordinatorURL + "/update"
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

func (svc *service) PostFLUpdateCBOR(ctx context.Context, updateData []byte) error {
	var update FLUpdate

	if err := cbor.Unmarshal(updateData, &update); err != nil {
		return fmt.Errorf("failed to decode CBOR update: %w", err)
	}

	return svc.PostFLUpdate(ctx, update)
}

func (svc *service) GetRoundStatus(ctx context.Context, roundID string) (RoundStatus, error) {
	if roundID == "" {
		return RoundStatus{}, pkgerrors.ErrInvalidData
	}

	if svc.flCoordinatorURL == "" || svc.httpClient == nil {
		return RoundStatus{}, errors.New("COORDINATOR_URL must be configured")
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
