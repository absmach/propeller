package controllers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	flpkg "github.com/absmach/propeller/pkg/fl"
)

func AggregateFLUpdates(
	updates []flpkg.UpdateEnvelope,
	algorithm string,
) (flpkg.UpdateEnvelope, error) {
	if len(updates) == 0 {
		return flpkg.UpdateEnvelope{}, errors.New("no updates to aggregate")
	}

	format := updates[0].Format
	if format == "" {
		format = "json-f64"
	}

	switch format {
	case "json-f64":
		return aggregateJSONF64(updates)
	default:
		return aggregateConcat(updates, format)
	}
}

func aggregateJSONF64(updates []flpkg.UpdateEnvelope) (flpkg.UpdateEnvelope, error) {
	var totalSamples uint64
	for _, u := range updates {
		totalSamples += u.NumSamples
	}

	if totalSamples == 0 {
		return flpkg.UpdateEnvelope{}, errors.New("cannot aggregate: total_samples is zero")
	}

	var sum []float64
	var dim int
	var haveDim bool

	for i := range updates {
		u := updates[i]

		raw, err := base64.StdEncoding.DecodeString(u.UpdateB64)
		if err != nil {
			return flpkg.UpdateEnvelope{}, fmt.Errorf("invalid update_b64: %w", err)
		}

		var vec []float64
		if err := json.Unmarshal(raw, &vec); err != nil {
			return flpkg.UpdateEnvelope{}, fmt.Errorf("invalid json-f64 payload: %w", err)
		}

		if !haveDim {
			dim = len(vec)
			if dim == 0 {
				return flpkg.UpdateEnvelope{}, errors.New("invalid vector: empty")
			}
			sum = make([]float64, dim)
			haveDim = true
		}
		if len(vec) != dim {
			return flpkg.UpdateEnvelope{}, errors.New("cannot aggregate: mismatched vector dimensions")
		}

		w := float64(u.NumSamples)
		for j := range vec {
			sum[j] += vec[j] * w
		}
	}

	den := float64(totalSamples)
	for i := range sum {
		sum[i] /= den
	}

	avgJSON, err := json.Marshal(sum)
	if err != nil {
		return flpkg.UpdateEnvelope{}, err
	}

	baseUpdate := updates[0]

	return flpkg.UpdateEnvelope{
		JobID:         baseUpdate.JobID,
		RoundID:       baseUpdate.RoundID,
		GlobalVersion: baseUpdate.GlobalVersion,
		PropletID:     "aggregator",
		NumSamples:    totalSamples,
		UpdateB64:     base64.StdEncoding.EncodeToString(avgJSON),
		Metrics: map[string]any{
			"num_clients":   len(updates),
			"total_samples": totalSamples,
			"algorithm":     "fedavg",
		},
		Format: "json-f64",
	}, nil
}

func aggregateConcat(updates []flpkg.UpdateEnvelope, format string) (flpkg.UpdateEnvelope, error) {
	const delim = "\n---PROP-UPDATE---\n"

	var totalSamples uint64
	var buf []byte

	for i, u := range updates {
		raw, err := base64.StdEncoding.DecodeString(u.UpdateB64)
		if err != nil {
			return flpkg.UpdateEnvelope{}, fmt.Errorf("invalid update_b64: %w", err)
		}
		if i > 0 {
			buf = append(buf, []byte(delim)...)
		}
		buf = append(buf, raw...)
		totalSamples += u.NumSamples
	}

	baseUpdate := updates[0]

	return flpkg.UpdateEnvelope{
		JobID:         baseUpdate.JobID,
		RoundID:       baseUpdate.RoundID,
		GlobalVersion: baseUpdate.GlobalVersion,
		PropletID:     "aggregator",
		NumSamples:    totalSamples,
		UpdateB64:     base64.StdEncoding.EncodeToString(buf),
		Metrics: map[string]any{
			"num_clients":   len(updates),
			"total_samples": totalSamples,
			"algorithm":     "concat",
		},
		Format: format,
	}, nil
}
