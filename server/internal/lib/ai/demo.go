package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

var ErrOutput = errors.New("invalid AI output")
var ErrTool = errors.New("unknown or invalid tool")

// Result describes extraction from a fixed fictional sentence, never a claim decision.
type Result struct {
	Label      string `json:"label"`
	EvidenceID string `json:"evidence_id"`
	Quote      string `json:"quote"`
}

const demoID = "synthetic-1"
const demoQuote = "The fictional parcel is blue."

func (g *Gateway) CheckConnection(ctx context.Context) error {
	result, err := g.Chat(ctx, []Message{
		{Role: "system", Content: "Reply briefly to the fixed fictional example."},
		{Role: "user", Content: demoQuote},
	}, ChatOptions{})
	if err != nil {
		return err
	}
	if result.FinishReason != "stop" || strings.TrimSpace(result.Content) == "" {
		return ErrOutput
	}
	return nil
}

// SyntheticDemo takes no user text and fails closed to abstain. An error signals
// why no result was accepted; callers must not interpret abstention as a decision.
func (g *Gateway) SyntheticDemo(ctx context.Context) (Result, error) {
	abstain := Result{Label: "abstain"}
	if g == nil || g.client == nil {
		return abstain, ErrConfig
	}
	ctx, cancel := context.WithTimeout(ctx, g.config.Timeout)
	defer cancel()
	evidence, err := runTool("synthetic_evidence", json.RawMessage(`{}`))
	if err != nil {
		return abstain, err
	}
	request := map[string]any{
		"model": g.config.Model, "max_tokens": g.config.MaxTokens,
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "synthetic_extraction", "strict": true, "schema": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"label", "evidence_id", "quote"},
			"properties": map[string]any{"label": map[string]any{"type": "string", "enum": []string{"blue", "not_blue", "abstain"}}, "evidence_id": map[string]any{"type": "string"}, "quote": map[string]any{"type": "string"}},
		}}},
		"messages": []map[string]string{{"role": "system", "content": "Extract the parcel color from the fictional evidence. Return only a JSON object with exactly three string fields: label, evidence_id, quote. Set label to blue if the evidence states blue, not_blue if it states another color, or abstain if uncertain. For a supported label, copy the evidence identifier into evidence_id and the exact evidence sentence into quote. For abstain use empty evidence_id and quote. Do not make medical or claim decisions."}, {"role": "user", "content": evidence}},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return abstain, ErrProvider
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return abstain, ErrProvider
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.config.APIKey)
	resp, err := g.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return abstain, ctx.Err()
		}
		return abstain, ErrProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return abstain, ErrProvider
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, g.config.MaxResponseBytes+1))
	if err != nil || int64(len(data)) > g.config.MaxResponseBytes {
		if ctx.Err() != nil {
			return abstain, ctx.Err()
		}
		return abstain, ErrProvider
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content   string          `json:"content"`
				Refusal   string          `json:"refusal"`
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &response) != nil || len(response.Choices) != 1 || response.Choices[0].FinishReason != "stop" || response.Choices[0].Message.Refusal != "" || len(response.Choices[0].Message.ToolCalls) > 0 && string(response.Choices[0].Message.ToolCalls) != "null" || response.Usage.CompletionTokens < 0 || response.Usage.CompletionTokens > g.config.MaxTokens {
		return abstain, ErrOutput
	}
	var result Result
	if err := strictResult([]byte(response.Choices[0].Message.Content), &result); err != nil {
		return abstain, ErrOutput
	}
	switch result.Label {
	case "abstain":
		if result.EvidenceID != "" || result.Quote != "" {
			return abstain, ErrOutput
		}
	case "blue":
		if result.EvidenceID != demoID || result.Quote != demoQuote {
			return abstain, ErrOutput
		}
	default:
		return abstain, ErrOutput
	}
	return result, nil
}

// The bounded registry has one read-only tool and an empty input schema.
func runTool(name string, input json.RawMessage) (string, error) {
	if name != "synthetic_evidence" || strings.TrimSpace(string(input)) != "{}" {
		return "", ErrTool
	}
	return demoID + ": " + demoQuote, nil
}

func strictResult(data []byte, out *Result) error {
	d := json.NewDecoder(bytes.NewReader(data))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return ErrOutput
	}
	seen := map[string]bool{}
	for d.More() {
		t, err = d.Token()
		if err != nil {
			return ErrOutput
		}
		k, ok := t.(string)
		if !ok || seen[k] {
			return ErrOutput
		}
		seen[k] = true
		value, err := d.Token()
		v, ok := value.(string)
		if err != nil || !ok {
			return ErrOutput
		}
		switch k {
		case "label":
			out.Label = v
		case "evidence_id":
			out.EvidenceID = v
		case "quote":
			out.Quote = v
		default:
			return ErrOutput
		}
	}
	t, err = d.Token()
	if err != nil || t != json.Delim('}') || len(seen) != 3 {
		return ErrOutput
	}
	if _, err = d.Token(); err != io.EOF {
		return ErrOutput
	}
	return nil
}
