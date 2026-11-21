package kb

import (
	"context"
	"errors"
	"testing"
)

type segmentLimitExtractor struct {
	segments []Segment
	err      error
}

func (e segmentLimitExtractor) Format() Format { return FormatText }
func (e segmentLimitExtractor) Extract(context.Context, []byte) ([]Segment, error) {
	return e.segments, e.err
}

func TestRegistrySegmentLimit(t *testing.T) {
	for _, n := range []int{MaxSegments, MaxSegments + 1} {
		r := NewRegistry(segmentLimitExtractor{segments: make([]Segment, n)})
		segments, err := r.Extract(context.Background(), FormatText, nil)
		if n == MaxSegments {
			if err != nil || len(segments) != n {
				t.Fatalf("at segment limit: count=%d err=%v", len(segments), err)
			}
		} else if !errors.Is(err, ErrTooLarge) || segments != nil {
			t.Fatalf("extractor overflow returned partial result: count=%d err=%v", len(segments), err)
		}
	}
}

func TestRegistryExtractorErrorHasNoPartialSegments(t *testing.T) {
	r := NewRegistry(segmentLimitExtractor{segments: []Segment{{Text: "partial"}}, err: ErrCorrupt})
	segments, err := r.Extract(context.Background(), FormatText, nil)
	if !errors.Is(err, ErrCorrupt) || segments != nil {
		t.Fatalf("extractor error: %#v, %v", segments, err)
	}
}
