package kb

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	MaxBranchCandidates = 50
	MaxUnionCandidates  = 100
	MaxPassages         = 5
)

// Candidate is transport-independent. Callers must supply only candidates from
// the same authorized search; selection is not an authorization boundary.
type Candidate struct {
	ChunkID, DocumentID string
	// Position is the chunk's zero-based order inside its document. It is
	// carried through fusion so a citation can point at the exact passage.
	Position                    int
	Title, HeadingPath, Locator string
	Filename, Format            string
	Content, ContentHash        string
	// Attributes and the validity window travel through fusion so a citation
	// keeps the document metadata even when the passage came from the semantic
	// branch.
	Attributes                json.RawMessage
	ValidFrom                 *time.Time
	ValidTo                   *time.Time
	Version                   string
	Global                    bool
	LexicalRank, SemanticRank int
	Score                     float64
}

func candidateKey(c Candidate) string {
	if c.ChunkID != "" {
		return c.ChunkID
	}
	return fmt.Sprintf("%s/%d/%x", c.DocumentID, c.Position, sha256.Sum256([]byte(c.Content)))
}

// WeightedRRF fuses ordered branches, never their incomparable raw scores.
// Ranks start at one, absent branches contribute zero, and each chunk contributes
// at most once per branch. Invalid/disabled weights contribute nothing.
func WeightedRRF(lexical, semantic []Candidate, lexicalWeight, semanticWeight float64) []Candidate {
	union := make(map[string]Candidate, MaxUnionCandidates)
	for branch, candidates := range [][]Candidate{lexical, semantic} {
		weight := []float64{lexicalWeight, semanticWeight}[branch]
		if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
			continue
		}
		seen := make(map[string]bool)
		for i, candidate := range candidates[:min(len(candidates), MaxBranchCandidates)] {
			key := candidateKey(candidate)
			if seen[key] {
				continue
			}
			seen[key] = true
			current, exists := union[key]
			if !exists {
				current = candidate
				current.Score, current.LexicalRank, current.SemanticRank = 0, 0, 0
			}
			if branch == 0 {
				current.LexicalRank = i + 1
			} else {
				current.SemanticRank = i + 1
			}
			current.Score += weight / float64(60+i+1)
			union[key] = current
		}
	}
	result := make([]Candidate, 0, len(union))
	for _, candidate := range union {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.DocumentID != b.DocumentID {
			return a.DocumentID < b.DocumentID
		}
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		return candidateKey(a) < candidateKey(b)
	})
	return result
}

// SelectCandidates preserves ranking while removing exact/redundant evidence.
// Diversification prefers two passages per document, then fills unused slots
// from the best remaining distinct evidence without changing relative rank.
func SelectCandidates(ranked []Candidate, limit int) []Candidate {
	limit = min(max(limit, 0), MaxPassages)
	unique := make([]Candidate, 0, min(len(ranked), MaxUnionCandidates))
	chunks, contents, hashes := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for _, candidate := range ranked[:min(len(ranked), MaxUnionCandidates)] {
		key := candidateKey(candidate)
		// Hash actual content too: equal text is redundant even if stored hashes
		// differ. Do not normalize codes, punctuation or negation away.
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate.Content)))
		if chunks[key] || contents[digest] || (candidate.ContentHash != "" && hashes[candidate.ContentHash]) {
			continue
		}
		chunks[key], contents[digest] = true, true
		if candidate.ContentHash != "" {
			hashes[candidate.ContentHash] = true
		}
		redundant := false
		for _, prior := range unique {
			if prior.DocumentID == candidate.DocumentID && redundantPassage(prior, candidate) {
				redundant = true
				break
			}
		}
		if !redundant {
			unique = append(unique, candidate)
		}
	}
	chosen := make([]bool, len(unique))
	counts := make(map[string]int)
	count := 0
	for i, candidate := range unique {
		if count == limit {
			break
		}
		if counts[candidate.DocumentID] >= 2 {
			continue
		}
		chosen[i] = true
		count++
		counts[candidate.DocumentID]++
	}
	for i := range unique {
		if count == limit {
			break
		}
		if !chosen[i] {
			chosen[i] = true
			count++
		}
	}
	selected := make([]Candidate, 0, limit)
	for i, candidate := range unique {
		if chosen[i] {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func redundantPassage(a, b Candidate) bool {
	left, right := strings.Join(strings.Fields(a.Content), " "), strings.Join(strings.Fields(b.Content), " ")
	if left == right {
		return true
	}
	// Only suppress fully contained substantial passages. A high token-overlap
	// ratio alone can erase the one differing diagnosis code or a negation.
	if len(left) > len(right) {
		left, right = right, left
	}
	return len([]rune(left)) >= 80 && strings.Contains(right, left)
}
