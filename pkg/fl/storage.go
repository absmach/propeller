package fl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type PersistentStorage struct {
	roundsDir string
	modelsDir string
	mu        sync.RWMutex
}

func NewPersistentStorage(roundsDir, modelsDir string) (*PersistentStorage, error) {
	if err := os.MkdirAll(roundsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create rounds directory: %w", err)
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create models directory: %w", err)
	}

	return &PersistentStorage{
		roundsDir: roundsDir,
		modelsDir: modelsDir,
	}, nil
}

func (ps *PersistentStorage) SaveRound(roundID string, state *RoundState) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Sanitize roundID to prevent path traversal attacks
	sanitizedRoundID := sanitizeRoundID(roundID)
	if sanitizedRoundID == "" {
		return fmt.Errorf("invalid roundID: %s", roundID)
	}

	roundFile := filepath.Join(ps.roundsDir, fmt.Sprintf("round_%s.json", sanitizedRoundID))
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal round state: %w", err)
	}

	if err := os.WriteFile(roundFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write round file: %w", err)
	}

	return nil
}

func (ps *PersistentStorage) LoadRound(roundID string) (*RoundState, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	// Sanitize roundID to prevent path traversal attacks
	sanitizedRoundID := sanitizeRoundID(roundID)
	if sanitizedRoundID == "" {
		return nil, fmt.Errorf("invalid roundID: %s", roundID)
	}

	roundFile := filepath.Join(ps.roundsDir, fmt.Sprintf("round_%s.json", sanitizedRoundID))
	data, err := os.ReadFile(roundFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read round file: %w", err)
	}

	var state RoundState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal round state: %w", err)
	}

	return &state, nil
}

func (ps *PersistentStorage) ListRounds() ([]string, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	entries, err := os.ReadDir(ps.roundsDir)
	if err != nil {
		return nil, err
	}

	var roundIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var roundID string
		if _, err := fmt.Sscanf(entry.Name(), "round_%s.json", &roundID); err == nil {
			roundIDs = append(roundIDs, roundID)
		}
	}

	return roundIDs, nil
}

func (ps *PersistentStorage) SaveModel(version int, model Model) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	modelFile := filepath.Join(ps.modelsDir, fmt.Sprintf("model_v%d.json", version))
	data, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal model: %w", err)
	}

	if err := os.WriteFile(modelFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write model file: %w", err)
	}

	return nil
}

func (ps *PersistentStorage) LoadModel(version int) (*Model, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	modelFile := filepath.Join(ps.modelsDir, fmt.Sprintf("model_v%d.json", version))
	data, err := os.ReadFile(modelFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read model file: %w", err)
	}

	var model Model
	if err := json.Unmarshal(data, &model); err != nil {
		return nil, fmt.Errorf("failed to unmarshal model: %w", err)
	}

	return &model, nil
}

func (ps *PersistentStorage) ListModels() ([]int, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	entries, err := os.ReadDir(ps.modelsDir)
	if err != nil {
		return nil, err
	}

	var versions []int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "model_v%d.json", &version); err == nil {
			versions = append(versions, version)
		}
	}

	return versions, nil
}

// sanitizeRoundID removes path traversal sequences and other dangerous characters
// from roundID to prevent directory traversal attacks.
func sanitizeRoundID(roundID string) string {
	// Remove path separators and parent directory references
	sanitized := strings.ReplaceAll(roundID, "..", "")
	sanitized = strings.ReplaceAll(sanitized, "/", "")
	sanitized = strings.ReplaceAll(sanitized, "\\", "")
	
	// Remove any remaining whitespace and control characters
	sanitized = strings.TrimSpace(sanitized)
	
	// Only allow alphanumeric, hyphens, underscores, and dots (but not consecutive dots)
	// This ensures the roundID is safe for use in filenames
	var result strings.Builder
	for _, r := range sanitized {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || 
		   (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			result.WriteRune(r)
		}
	}
	
	final := result.String()
	// Ensure no consecutive dots remain
	final = strings.ReplaceAll(final, "..", "")
	
	return final
}
