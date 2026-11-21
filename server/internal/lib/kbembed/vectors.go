package kbembed

import (
	"encoding/json"
	"math"
	"strconv"
)

func decodeVectors(body []byte, count int) ([][]float32, error) {
	bad := &providerError{code: "invalid_response"}
	var response struct {
		Object string `json:"object"`
		Model  string `json:"model"`
		Data   []struct {
			Object    string            `json:"object"`
			Index     *int              `json:"index"`
			Embedding []json.RawMessage `json:"embedding"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.Object != "list" || response.Model != model || len(response.Data) != count {
		return nil, bad
	}
	vectors := make([][]float32, count)
	for _, item := range response.Data {
		if item.Object != "embedding" || item.Index == nil || *item.Index < 0 || *item.Index >= count || vectors[*item.Index] != nil || len(item.Embedding) != dimensions {
			return nil, bad
		}
		norm := 0.0
		values := make([]float64, dimensions)
		for i, number := range item.Embedding {
			value, err := strconv.ParseFloat(string(number), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, bad
			}
			values[i] = value
			norm = math.Hypot(norm, value)
		}
		// Enforce the normalized dense profile rather than silently repairing it.
		if norm == 0 || math.IsInf(norm, 0) || math.Abs(norm-1) > 1e-3 {
			return nil, bad
		}
		vector := make([]float32, dimensions)
		for i, value := range values {
			vector[i] = float32(value / norm)
		}
		vectors[*item.Index] = vector
	}
	return vectors, nil
}
