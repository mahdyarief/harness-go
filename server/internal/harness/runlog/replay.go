package runlog

import (
	"encoding/json"

	"hsg/internal/harness/services"
)

// Projection is the outcome rebuilt from a run's event log. It mirrors the
// fields a consumer compares against a live result: member content/structured
// output, token usage, and the reduce decision. Duration is not derivable from
// the log (wall-clock timing is not an event) and is left out.
type Projection struct {
	Started          bool
	Ended            bool
	Status           string
	Members          []ProjectedMember
	PromptTokens     int
	CompletionTokens int
	// CostUSD sums the priced model calls; CostKnown is false when no call
	// carried a price, so an unpriced run is not read as free.
	CostUSD    float64
	CostKnown  bool
	Structured json.RawMessage
	Decision   string
}

// ProjectedMember is one member's rebuilt outcome.
type ProjectedMember struct {
	Name             string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	CostKnown        bool
	Content          string
	Structured       json.RawMessage
}

// Project rebuilds a Projection from a run's events in seq order.
func Project(events []services.RunLogEvent) Projection {
	p := Projection{}
	for _, event := range events {
		member := event.Member
		switch event.Type {
		case EventTypeRunStart:
			p.Started = true
		case EventTypeRunEnd:
			p.Ended = true
			var body struct {
				Status string `json:"status"`
			}
			_ = json.Unmarshal(event.Payload, &body)
			p.Status = body.Status
		case EventTypeModelCall:
			var call ModelCall
			_ = json.Unmarshal(event.Payload, &call)
			p.PromptTokens += call.PromptTokens
			p.CompletionTokens += call.CompletionTokens
			m := p.member(member)
			m.PromptTokens += call.PromptTokens
			m.CompletionTokens += call.CompletionTokens
			if call.CostUSD != nil {
				p.CostUSD += *call.CostUSD
				p.CostKnown = true
				m.CostUSD += *call.CostUSD
				m.CostKnown = true
			}
		case EventTypeToolResult:
			var result ToolResult
			_ = json.Unmarshal(event.Payload, &result)
			m := p.member(member)
			m.Content += result.Result
		case EventTypeStructured:
			var submit StructuredSubmit
			_ = json.Unmarshal(event.Payload, &submit)
			m := p.member(member)
			m.Structured = json.RawMessage(submit.Arguments)
			if member == "" {
				p.Structured = json.RawMessage(submit.Arguments)
			}
		case EventTypeReduceDecision:
			var decision ReduceDecision
			_ = json.Unmarshal(event.Payload, &decision)
			p.Decision = decision.Rule
		}
	}
	return p
}

func (p *Projection) member(name string) *ProjectedMember {
	for i := range p.Members {
		if p.Members[i].Name == name {
			return &p.Members[i]
		}
	}
	p.Members = append(p.Members, ProjectedMember{Name: name})
	return &p.Members[len(p.Members)-1]
}
