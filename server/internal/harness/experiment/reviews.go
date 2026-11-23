package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"hsg/internal/harness/review"
)

// ReviewLister is the read side of the review inbox that this package needs.
// Both review.Memory and the Postgres store satisfy it.
type ReviewLister interface {
	List(ctx context.Context, status string, limit, offset int) ([]review.Item, int, error)
}

// DecidedResults maps each review item's Subject to the latest human decision
// result. Subject is the consumer's item id, which is what the dataset uses.
func DecidedResults(ctx context.Context, store ReviewLister, pageSize int) (map[string]json.RawMessage, error) {
	if pageSize < 1 {
		pageSize = 200
	}
	out := map[string]json.RawMessage{}
	for offset := 0; ; {
		items, total, err := store.List(ctx, review.StatusDecided, pageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Subject == "" || len(item.Decisions) == 0 {
				continue
			}
			latest := item.Decisions[len(item.Decisions)-1]
			if len(latest.Result) > 0 {
				out[item.Subject] = latest.Result
			}
		}
		offset += len(items)
		if len(items) == 0 || offset >= total {
			break
		}
	}
	return out, nil
}

// ReviewAgreement compares an arm's output with the decided human result for
// the same dataset item. Items without a decision are skipped, not counted as
// disagreement. A nil same function compares the JSON structurally.
func ReviewAgreement(final map[string]json.RawMessage, same func(output, human json.RawMessage) bool) func(itemID string, output, label json.RawMessage) (bool, bool) {
	return func(itemID string, output, _ json.RawMessage) (bool, bool) {
		human, ok := final[itemID]
		if !ok || len(human) == 0 || len(output) == 0 {
			return false, false
		}
		if same != nil {
			return same(output, human), true
		}
		return sameJSON(output, human), true
	}
}

func sameJSON(a, b json.RawMessage) bool {
	left, ok := decodeAny(a)
	if !ok {
		return false
	}
	right, ok := decodeAny(b)
	if !ok {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func decodeAny(raw json.RawMessage) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, false
	}
	return value, true
}
