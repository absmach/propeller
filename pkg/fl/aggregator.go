package fl

type FedAvgAggregator struct{}

func NewFedAvgAggregator() Aggregator {
	return &FedAvgAggregator{}
}

func initializeAggregatedWeights(updates []Update) []float64 {
	if len(updates) == 0 || updates[0].Update == nil {
		return nil
	}

	if w, ok := updates[0].Update["w"].([]any); ok {
		return make([]float64, len(w))
	}

	return nil
}

func validateAndProcessUpdate(update Update, totalSamples int64) (weight float64, newTotalSamples int64, err error) {
	if update.Update == nil {
		return 0, totalSamples, nil
	}

	if update.NumSamples < 0 {
		return 0, totalSamples, ErrOverflow
	}

	if totalSamples > (int64(^uint64(0)>>1) - int64(update.NumSamples)) {
		return 0, totalSamples, ErrOverflow
	}

	weight = float64(update.NumSamples)
	newTotalSamples = totalSamples + int64(update.NumSamples)

	return weight, newTotalSamples, nil
}

func aggregateWeights(aggregatedW []float64, update Update, weight float64) {
	if w, ok := update.Update["w"].([]any); ok {
		for i, v := range w {
			if f, ok := v.(float64); ok {
				if i < len(aggregatedW) {
					aggregatedW[i] += f * weight
				}
			}
		}
	}
}

func aggregateBias(aggregatedB *float64, update Update, weight float64) {
	if b, ok := update.Update["b"].(float64); ok {
		*aggregatedB += b * weight
	}
}

func normalizeWeights(aggregatedW []float64, aggregatedB *float64, totalSamples int64) {
	if totalSamples > 0 {
		weightNorm := float64(totalSamples)
		for i := range aggregatedW {
			aggregatedW[i] /= weightNorm
		}
		*aggregatedB /= weightNorm
	}
}

func (f *FedAvgAggregator) Aggregate(updates []Update) (Model, error) {
	if len(updates) == 0 {
		return Model{}, ErrNoUpdates
	}

	aggregatedW := initializeAggregatedWeights(updates)
	var aggregatedB float64
	// Use int64 for totalSamples to prevent integer overflow on 32-bit systems
	// when aggregating updates from many clients with large sample counts.
	// update.NumSamples is int (32-bit on 32-bit systems), so we cast to int64.
	var totalSamples int64

	for _, update := range updates {
		weight, newTotalSamples, err := validateAndProcessUpdate(update, totalSamples)
		if err != nil {
			return Model{}, err
		}

		if weight == 0 {
			continue
		}

		totalSamples = newTotalSamples

		if aggregatedW != nil {
			aggregateWeights(aggregatedW, update, weight)
		}

		aggregateBias(&aggregatedB, update, weight)
	}

	normalizeWeights(aggregatedW, &aggregatedB, totalSamples)

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
