package fl

type FedAvgAggregator struct{}

func NewFedAvgAggregator() Aggregator {
	return &FedAvgAggregator{}
}

func (f *FedAvgAggregator) Aggregate(updates []Update) (Model, error) {
	if len(updates) == 0 {
		return Model{}, ErrNoUpdates
	}

	var aggregatedW []float64
	var aggregatedB float64
	// Use int64 for totalSamples to prevent integer overflow on 32-bit systems
	// when aggregating updates from many clients with large sample counts.
	// update.NumSamples is int (32-bit on 32-bit systems), so we cast to int64.
	var totalSamples int64

	if len(updates) > 0 && updates[0].Update != nil {
		if w, ok := updates[0].Update["w"].([]any); ok {
			aggregatedW = make([]float64, len(w))
		}
	}

	for _, update := range updates {
		if update.Update == nil {
			continue
		}

		weight := float64(update.NumSamples)
		// Explicitly cast to int64 to prevent overflow when summing many updates
		newTotal := totalSamples + int64(update.NumSamples)
		if newTotal < totalSamples {
			return Model{}, ErrOverflow
		}
		totalSamples = newTotal

		if w, ok := update.Update["w"].([]any); ok {
			for i, v := range w {
				if f, ok := v.(float64); ok {
					if i < len(aggregatedW) {
						aggregatedW[i] += f * weight
					}
				}
			}
		}

		if b, ok := update.Update["b"].(float64); ok {
			aggregatedB += b * weight
		}
	}

	if totalSamples > 0 {
		weightNorm := float64(totalSamples)
		for i := range aggregatedW {
			aggregatedW[i] /= weightNorm
		}
		aggregatedB /= weightNorm
	}

	return Model{
		Data: map[string]any{
			"w": aggregatedW,
			"b": aggregatedB,
		},
		Metadata: map[string]any{
			"total_samples": totalSamples,
			"num_updates":   len(updates),
			"algorithm":     "FedAvg",
		},
	}, nil
}
