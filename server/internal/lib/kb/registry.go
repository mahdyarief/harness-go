package kb

import (
	"context"
	"fmt"
)

// Registry maps a Format to its Extractor. Callers build it once at startup
// from the extractors they want enabled, so a format (PDF, for instance) stays
// off until it is registered.
type Registry struct {
	extractors map[Format]Extractor
}

func NewRegistry(extractors ...Extractor) *Registry {
	r := &Registry{extractors: map[Format]Extractor{}}
	for _, e := range extractors {
		if e != nil {
			r.extractors[e.Format()] = e
		}
	}
	return r
}

// Supports reports whether the format has a registered extractor.
func (r *Registry) Supports(f Format) bool {
	if r == nil {
		return false
	}
	_, ok := r.extractors[f]
	return ok
}

// Extract runs the extractor for f. An unregistered format is
// ErrUnsupportedFormat, not a panic.
func (r *Registry) Extract(ctx context.Context, f Format, data []byte) ([]Segment, error) {
	if r == nil {
		return nil, ErrUnsupportedFormat
	}
	e, ok := r.extractors[f]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, f)
	}
	if len(data) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	segments, err := e.Extract(ctx, data)
	if err != nil {
		return nil, err
	}
	if len(segments) > MaxSegments {
		return nil, ErrTooLarge
	}
	return segments, nil
}

// ExtractWithMetadata uses the optional richer extractor contract without
// changing Extractor. Legacy third-party extractors remain usable, but cannot
// claim complete coverage when they do not report their limitations.
func (r *Registry) ExtractWithMetadata(ctx context.Context, f Format, data []byte) ([]Segment, ExtractionMetadata, error) {
	if r == nil {
		return nil, ExtractionMetadata{}, ErrUnsupportedFormat
	}
	e, ok := r.extractors[f]
	if !ok {
		return nil, ExtractionMetadata{}, fmt.Errorf("%w: %s", ErrUnsupportedFormat, f)
	}
	if len(data) > MaxUploadBytes {
		return nil, ExtractionMetadata{}, ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, ExtractionMetadata{}, err
	}
	var segs []Segment
	var err error
	meta := newExtractionMetadata()
	switch e := e.(type) {
	case interface {
		ExtractWithMetadata(context.Context, []byte) ([]Segment, ExtractionMetadata, error)
	}:
		segs, meta, err = e.ExtractWithMetadata(ctx, data)
	case markdownExtractor:
		segs, err = e.extract(ctx, data, &meta)
	case textExtractor:
		segs, err = e.extract(ctx, data, &meta)
	default:
		segs, err = e.Extract(ctx, data)
		meta.ExtractorVersion = ""
		meta.warn("metadata_unavailable")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && len(segs) > MaxSegments {
		err = ErrTooLarge
	}
	if len(meta.Warnings) > 0 {
		meta.Status = "limited"
	}
	return extractionResult(segs, meta, err)
}
