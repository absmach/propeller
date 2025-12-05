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
	var totalSamples int64

	if len(updates) > 0 && updates[0].Update != nil {
		if w, ok := updates[0].Update["w"].([]interface{}); ok {
			aggregatedW = make([]float64, len(w))
		}
	}

	for _, update := range updates {
		if update.Update == nil {
			continue
		}

		weight := float64(update.NumSamples)
		totalSamples += int64(update.NumSamples)

		if w, ok := update.Update["w"].([]interface{}); ok {
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
		Data: map[string]interface{}{
			"w": aggregatedW,
			"b": aggregatedB,
		},
		Metadata: map[string]interface{}{
			"total_samples": totalSamples,
			"num_updates":    len(updates),
			"algorithm":      "FedAvg",
		},
	}, nil
}
