package dtos

import "testing"

func TestKnowledgePreviewQueryClamp(t *testing.T) {
	for _, tc := range []struct {
		offset, limit, wantOffset, wantLimit int
	}{
		{-1, 0, 0, 10},
		{3, 1, 3, 1},
		{0, 20, 0, 20},
		{0, 21, 0, 20},
		{0, 10000, 0, 20},
	} {
		q := KnowledgePreviewQuery{ListQuery: ListQuery{Offset: tc.offset, Limit: tc.limit}}
		q.Clamp()
		if q.Offset != tc.wantOffset || q.Limit != tc.wantLimit {
			t.Fatalf("Clamp(%d,%d) = (%d,%d), ingin (%d,%d)", tc.offset, tc.limit, q.Offset, q.Limit, tc.wantOffset, tc.wantLimit)
		}
	}
}
