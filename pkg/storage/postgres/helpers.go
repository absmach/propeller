package postgres

import (
	"encoding/json"
	"time"
)

func jsonBytes(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}

	return json.Marshal(v)
}

func jsonUnmarshal(data []byte, v any) error {
	if data == nil {
		return nil
	}

	return json.Unmarshal(data, v)
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}
