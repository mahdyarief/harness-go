package harness

import (
	"context"
	"encoding/json"
	"fmt"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/review"
	"hsg/internal/harness/services"
)

// QueueReview applies a review policy to a finished outcome and, when the
// policy flags it, stores an inbox item. It is the entry point a profile or
// team run uses to make an uncertain result reviewable; a nil policy or store
// is a no-op. The boolean reports whether an item was created.
func QueueReview(
	ctx context.Context,
	store review.Store,
	policy review.Policy,
	runID, subject, role string,
	out services.Outcome,
) (review.Item, bool, error) {
	if store == nil || policy == nil || !policy(out) {
		return review.Item{}, false, nil
	}
	if subject == "" {
		subject = "review"
	}
	if role == "" {
		role = "reviewer"
	}
	result := out.Structured
	if len(result) == 0 {
		encoded, err := json.Marshal(map[string]string{"content": out.Content})
		if err != nil {
			return review.Item{}, false, err
		}
		result = encoded
	}
	citations := make([]evidence.Citation, 0, len(out.Evidence))
	for _, item := range out.Evidence {
		citations = append(citations, evidence.Citation{Kind: item.Kind, Ref: item.Ref})
	}
	item, err := store.Create(ctx, review.Item{
		RunID:    runID,
		Subject:  subject,
		Result:   result,
		Evidence: citations,
		Reason:   reasonFor(out),
		Role:     role,
	})
	if err != nil {
		return review.Item{}, false, err
	}
	return item, true, nil
}

func reasonFor(out services.Outcome) string {
	if out.Partial {
		return "hasil parsial memerlukan tinjauan manusia"
	}
	return "kebijakan menandai hasil ini untuk tinjauan"
}

// ReviewPolicyError wraps a queue failure so a caller can log it without
// failing the run: review is an audit path, not a reason to lose an answer.
func ReviewPolicyError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("antrean tinjauan: %w", err)
}
