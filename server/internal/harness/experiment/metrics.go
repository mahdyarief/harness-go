package experiment

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"hsg/internal/harness/evidence"
)

// Score is one precision/recall/F1 triple. Precision and recall are nil when
// their denominator is zero, so "not predicted" and "zero precision" differ.
type Score struct {
	Precision *float64 `json:"precision"`
	Recall    *float64 `json:"recall"`
	F1        *float64 `json:"f1"`
	Support   int      `json:"support"`
}

// ConfusionRow is one actual class and how often it was predicted as each
// class. Predicted keys are sorted by the JSON encoder and by the Markdown
// writer, so the same input always renders the same bytes.
type ConfusionRow struct {
	Actual    string         `json:"actual"`
	Predicted map[string]int `json:"predicted"`
}

// Classification is the generic label comparison. Labels are the sorted union
// of observed actual and predicted values, so an unexpected prediction still
// shows up in the matrix.
type Classification struct {
	Labels        []string       `json:"labels"`
	Micro         Score          `json:"micro"`
	Macro         Score          `json:"macro"`
	Confusion     []ConfusionRow `json:"confusion"`
	Scored        int            `json:"scored"`
	MissingLabel  int            `json:"missing_label"`
	MissingOutput int            `json:"missing_output"`
}

// Latency reports nearest-rank percentiles: p50 is the ceil(0.5n)-th value,
// not an interpolated one, so a five-sample run has an observed median.
type Latency struct {
	Samples int      `json:"samples"`
	P50MS   *float64 `json:"p50_ms"`
	P95MS   *float64 `json:"p95_ms"`
}

// Usage sums billed tokens. CostUSD stays nil when any scored item reports a
// nil cost, and CostIncomplete records why, rather than reporting a sum that
// silently ignores unpriced items.
type Usage struct {
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	CostUSD          *float64 `json:"cost_usd"`
	CostIncomplete   bool     `json:"cost_incomplete"`
	ItemsWithCost    int      `json:"items_with_cost"`
	ItemsMissingCost int      `json:"items_missing_cost"`
}

// Grounding is the evidence grounding rate. Measured is false when the caller
// supplied no Ground hook, which is different from a measured rate of zero.
type Grounding struct {
	Measured bool     `json:"measured"`
	Items    int      `json:"items"`
	Grounded int      `json:"grounded"`
	Rate     *float64 `json:"rate"`
}

// Agreement is the human-agreement rate over items the caller could compare.
type Agreement struct {
	Measured bool     `json:"measured"`
	Items    int      `json:"items"`
	Agreed   int      `json:"agreed"`
	Rate     *float64 `json:"rate"`
}

// ArmMetrics is one arm's whole report section.
type ArmMetrics struct {
	Arm            string          `json:"arm"`
	Items          int             `json:"items"`
	Errors         int             `json:"errors"`
	Classification *Classification `json:"classification,omitempty"`
	Latency        Latency         `json:"latency"`
	Usage          Usage           `json:"usage"`
	Grounding      Grounding       `json:"grounding"`
	Agreement      Agreement       `json:"agreement"`
}

// MetricsConfig says how to read a label out of the JSON and which optional
// measurements to collect. An empty OutputPath means "same as LabelPath".
type MetricsConfig struct {
	LabelPath  string
	OutputPath string
	// Ground overrides evidence-based grounding when set.
	Ground func(output json.RawMessage) (grounded bool, ok bool)
	// EvidenceKinds enables the built-in grounding measure: an item counts
	// when the arm collected evidence of one of these kinds. It is ignored
	// when Ground is set or the list is empty.
	EvidenceKinds []string
	// CitationPath, when set, makes grounding stricter: the output at that
	// dot path must list refs, and every ref must appear in the run evidence.
	// An empty path accepts any collected evidence as grounding the item.
	CitationPath string
	Agree        func(output, label json.RawMessage) (agree bool, ok bool)
	// AgreeItem receives the dataset item id so a review-inbox adapter can
	// compare against that item's decided result. It wins over Agree.
	AgreeItem func(itemID string, output, label json.RawMessage) (agree bool, ok bool)
}

// Compute builds one ArmMetrics per arm, in the order the arms were given.
func Compute(res Results, ds Dataset, cfg MetricsConfig) []ArmMetrics {
	items := make(map[string]Item, len(ds.Items))
	for _, item := range ds.Items {
		items[item.ID] = item
	}
	out := make([]ArmMetrics, 0, len(res.ArmNames))
	for _, name := range res.ArmNames {
		rows := res.ForArm(name)
		out = append(out, computeArm(name, rows, items, cfg))
	}
	return out
}

func computeArm(name string, rows []ItemResult, items map[string]Item, cfg MetricsConfig) ArmMetrics {
	m := ArmMetrics{Arm: name, Items: len(rows)}
	for _, row := range rows {
		if row.Err != "" {
			m.Errors++
		}
	}
	if cfg.LabelPath != "" {
		m.Classification = classify(rows, items, cfg)
	}
	m.Latency = latencyOf(rows)
	m.Usage = usageOf(rows)
	m.Grounding = groundingOf(rows, cfg)
	m.Agreement = agreementOf(rows, items, cfg)
	return m
}

func classify(rows []ItemResult, items map[string]Item, cfg MetricsConfig) *Classification {
	outputPath := cfg.OutputPath
	if outputPath == "" {
		outputPath = cfg.LabelPath
	}
	c := &Classification{Labels: []string{}}
	counts := map[string]map[string]int{}
	actuals := map[string]int{}
	for _, row := range rows {
		item, ok := items[row.ItemID]
		if !ok {
			continue
		}
		actual, ok := fieldValue(item.Label, cfg.LabelPath)
		if !ok {
			c.MissingLabel++
			continue
		}
		predicted, ok := fieldValue(row.Output, outputPath)
		if !ok {
			c.MissingOutput++
			predicted = ""
		}
		if counts[actual] == nil {
			counts[actual] = map[string]int{}
		}
		counts[actual][predicted]++
		actuals[actual]++
		c.Scored++
	}
	labelSet := map[string]bool{}
	predictedTotals := map[string]int{}
	for actual, preds := range counts {
		labelSet[actual] = true
		for predicted, n := range preds {
			labelSet[predicted] = true
			predictedTotals[predicted] += n
		}
	}
	for label := range labelSet {
		c.Labels = append(c.Labels, label)
	}
	sort.Strings(c.Labels)
	var tp, fp, fn int
	var macroP, macroR, macroF float64
	for _, label := range c.Labels {
		hit := counts[label][label]
		tp += hit
		fp += predictedTotals[label] - hit
		fn += actuals[label] - hit
		p := ratio(hit, predictedTotals[label])
		r := ratio(hit, actuals[label])
		macroP += p
		macroR += r
		macroF += f1(p, r)
	}
	c.Confusion = make([]ConfusionRow, 0, len(c.Labels))
	for _, actual := range c.Labels {
		row := ConfusionRow{Actual: actual, Predicted: map[string]int{}}
		for predicted, n := range counts[actual] {
			if n > 0 {
				row.Predicted[predicted] = n
			}
		}
		c.Confusion = append(c.Confusion, row)
	}
	if c.Scored > 0 {
		c.Micro = Score{
			Precision: ratioPtr(tp, tp+fp),
			Recall:    ratioPtr(tp, tp+fn),
			F1:        f1Pair(tp, tp+fp, tp, tp+fn),
			Support:   c.Scored,
		}
	}
	if n := len(c.Labels); n > 0 {
		c.Macro = Score{
			Precision: ptr(macroP / float64(n)),
			Recall:    ptr(macroR / float64(n)),
			F1:        ptr(macroF / float64(n)),
			Support:   c.Scored,
		}
	}
	return c
}

// fieldValue walks a dot path and returns a scalar as text. Objects, arrays,
// nulls, and missing paths are not class labels, so they report ok=false.
func fieldValue(raw json.RawMessage, path string) (string, bool) {
	if len(raw) == 0 || path == "" {
		return "", false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var current any
	if err := dec.Decode(&current); err != nil {
		return "", false
	}
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = obj[part]
		if !ok {
			return "", false
		}
	}
	switch v := current.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return "", false
	}
}

func latencyOf(rows []ItemResult) Latency {
	l := Latency{Samples: len(rows)}
	if len(rows) == 0 {
		return l
	}
	values := make([]float64, 0, len(rows))
	for _, row := range rows {
		values = append(values, float64(row.Stats.Latency)/float64(time.Millisecond))
	}
	sort.Float64s(values)
	l.P50MS = ptr(nearestRank(values, 0.50))
	l.P95MS = ptr(nearestRank(values, 0.95))
	return l
}

func nearestRank(sorted []float64, p float64) float64 {
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func usageOf(rows []ItemResult) Usage {
	var u Usage
	sum := 0.0
	for _, row := range rows {
		u.PromptTokens += row.Stats.PromptTokens
		u.CompletionTokens += row.Stats.CompletionTokens
		if row.Stats.Cost == nil {
			u.ItemsMissingCost++
			continue
		}
		u.ItemsWithCost++
		sum += *row.Stats.Cost
	}
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	if u.ItemsMissingCost > 0 {
		u.CostIncomplete = true
	} else {
		u.CostUSD = ptr(sum)
	}
	return u
}

func groundingOf(rows []ItemResult, cfg MetricsConfig) Grounding {
	if cfg.Ground != nil {
		g := Grounding{Measured: true}
		for _, row := range rows {
			if row.Err != "" || len(row.Output) == 0 {
				continue
			}
			grounded, ok := cfg.Ground(row.Output)
			if !ok {
				continue
			}
			g.Items++
			if grounded {
				g.Grounded++
			}
		}
		return finishGrounding(g)
	}
	if len(cfg.EvidenceKinds) == 0 {
		return Grounding{}
	}
	registry := evidence.NewRegistry(cfg.EvidenceKinds...)
	g := Grounding{Measured: true}
	for _, row := range rows {
		if row.Err != "" || len(row.Output) == 0 {
			continue
		}
		seen := map[string]bool{}
		for _, item := range row.Stats.Evidence {
			if registry.Valid(item.Kind) && item.Ref != "" {
				seen[item.Ref] = true
			}
		}
		grounded := len(seen) > 0
		if cfg.CitationPath != "" {
			refs, ok := citationRefs(row.Output, cfg.CitationPath)
			grounded = ok && len(refs) > 0
			for _, ref := range refs {
				if !seen[ref] {
					grounded = false
					break
				}
			}
		}
		g.Items++
		if grounded {
			g.Grounded++
		}
	}
	return finishGrounding(g)
}

func finishGrounding(g Grounding) Grounding {
	if g.Items > 0 {
		g.Rate = ptr(float64(g.Grounded) / float64(g.Items))
	}
	return g
}

// citationRefs reads a list of citations at path. Each entry may be a string
// ref or an object with a "ref" or "id" field.
func citationRefs(raw json.RawMessage, path string) ([]string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var current any
	if err := dec.Decode(&current); err != nil {
		return nil, false
	}
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	list, ok := current.([]any)
	if !ok {
		return nil, false
	}
	refs := make([]string, 0, len(list))
	for _, entry := range list {
		switch item := entry.(type) {
		case string:
			if item != "" {
				refs = append(refs, item)
			}
		case map[string]any:
			for _, key := range []string{"ref", "id"} {
				if text, ok := item[key].(string); ok && text != "" {
					refs = append(refs, text)
					break
				}
			}
		}
	}
	return refs, true
}

func agreementOf(rows []ItemResult, items map[string]Item, cfg MetricsConfig) Agreement {
	a := Agreement{Measured: cfg.Agree != nil || cfg.AgreeItem != nil}
	if !a.Measured {
		return a
	}
	for _, row := range rows {
		item, ok := items[row.ItemID]
		if !ok || row.Err != "" || len(row.Output) == 0 {
			continue
		}
		// The human-agreement adapter supplies its own reference, so a
		// dataset label is optional on that path.
		if cfg.AgreeItem == nil && len(item.Label) == 0 {
			continue
		}
		var same, compared bool
		if cfg.AgreeItem != nil {
			same, compared = cfg.AgreeItem(row.ItemID, row.Output, item.Label)
		} else {
			same, compared = cfg.Agree(row.Output, item.Label)
		}
		if !compared {
			continue
		}
		a.Items++
		if same {
			a.Agreed++
		}
	}
	if a.Items > 0 {
		a.Rate = ptr(float64(a.Agreed) / float64(a.Items))
	}
	return a
}

func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func ratioPtr(num, den int) *float64 {
	if den == 0 {
		return nil
	}
	return ptr(float64(num) / float64(den))
}

func f1(p, r float64) float64 {
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func f1Pair(tpNum, tpDen, rNum, rDen int) *float64 {
	if tpDen == 0 || rDen == 0 {
		return nil
	}
	return ptr(f1(float64(tpNum)/float64(tpDen), float64(rNum)/float64(rDen)))
}

func ptr(v float64) *float64 { return &v }
