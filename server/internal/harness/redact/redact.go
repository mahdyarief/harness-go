// Package redact replaces consumer-supplied identifiers with stable
// pseudonyms before a request leaves the process and restores them on the
// response. The policy is a list of named patterns; the package ships no
// built-in policy of its own, because what counts as sensitive is domain
// knowledge the consumer owns.
package redact

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"hsg/internal/lib/ai"
)

// Pattern is one named identifier shape. Name is used in the pseudonym, so it
// should be short and stable ("EMAIL", "PHONE", "NIK").
type Pattern struct {
	Name string
	Re   *regexp.Regexp
}

// NewPattern compiles a named pattern.
func NewPattern(name, expr string) (Pattern, error) {
	re, err := regexp.Compile(expr)
	if err != nil {
		return Pattern{}, err
	}
	return Pattern{Name: name, Re: re}, nil
}

// Redactor is stateful for one run: the same original value always maps to
// the same pseudonym, and pseudonyms are numbered in first-appearance order
// so a replay of the same conversation produces identical output.
type Redactor struct {
	patterns []Pattern

	mu      sync.Mutex
	counts  map[string]int    // pattern name -> next ordinal
	byValue map[string]string // original -> pseudonym
	byToken map[string]string // pseudonym -> original
}

func New(patterns ...Pattern) *Redactor {
	return &Redactor{
		patterns: patterns,
		counts:   map[string]int{},
		byValue:  map[string]string{},
		byToken:  map[string]string{},
	}
}

// Mapping returns the pseudonym-to-original table. It exists for the run log
// and for restoring text outside the model path; it must never be serialized
// into an outbound request.
func (r *Redactor) Mapping() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.byToken))
	for token, original := range r.byToken {
		out[token] = original
	}
	return out
}

// Outbound returns a copy of messages with every identifier replaced. The
// input slice and its elements are never mutated.
func (r *Redactor) Outbound(messages []ai.Message) []ai.Message {
	out := make([]ai.Message, len(messages))
	for i, message := range messages {
		copied := message
		copied.Content = r.hide(message.Content)
		if len(message.ToolCalls) > 0 {
			calls := make([]ai.ToolCall, len(message.ToolCalls))
			copy(calls, message.ToolCalls)
			for j := range calls {
				calls[j].Function.Arguments = r.hide(calls[j].Function.Arguments)
			}
			copied.ToolCalls = calls
		}
		out[i] = copied
	}
	return out
}

// Inbound restores identifiers in the final answer and in tool arguments, so
// the tools and the user receive the real values.
func (r *Redactor) Inbound(result ai.ChatResult) ai.ChatResult {
	result.Content = r.show(result.Content)
	if len(result.ToolCalls) > 0 {
		calls := make([]ai.ToolCall, len(result.ToolCalls))
		copy(calls, result.ToolCalls)
		for i := range calls {
			calls[i].Function.Arguments = r.show(calls[i].Function.Arguments)
			calls[i].Function.Name = r.show(calls[i].Function.Name)
		}
		result.ToolCalls = calls
	}
	return result
}

func (r *Redactor) hide(text string) string {
	if text == "" || len(r.patterns) == 0 {
		return text
	}
	for _, pattern := range r.patterns {
		text = pattern.Re.ReplaceAllStringFunc(text, func(match string) string {
			return r.token(pattern.Name, match)
		})
	}
	return text
}

func (r *Redactor) token(name, value string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token, ok := r.byValue[value]; ok {
		return token
	}
	r.counts[name]++
	token := "<" + name + "_" + strconv.Itoa(r.counts[name]) + ">"
	r.byValue[value] = token
	r.byToken[token] = value
	return token
}

func (r *Redactor) show(text string) string {
	if text == "" {
		return text
	}
	r.mu.Lock()
	tokens := make([]string, 0, len(r.byToken))
	for token := range r.byToken {
		tokens = append(tokens, token)
	}
	r.mu.Unlock()
	// Sorted replacement keeps the output independent of map iteration order.
	// Tokens end with '>', so no token is a prefix of another.
	sort.Strings(tokens)
	for _, token := range tokens {
		r.mu.Lock()
		original, ok := r.byToken[token]
		r.mu.Unlock()
		if ok {
			text = strings.ReplaceAll(text, token, original)
		}
	}
	return text
}
