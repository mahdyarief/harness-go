package experiment

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Report is the whole experiment outcome. It holds no timestamps or wall-clock
// values, so the same results always encode to the same bytes.
type Report struct {
	SchemaVersion int          `json:"schema_version"`
	Arms          []ArmMetrics `json:"arms"`
}

// BuildReport computes the report for one run.
func BuildReport(res Results, ds Dataset, cfg MetricsConfig) Report {
	return Report{SchemaVersion: 1, Arms: Compute(res, ds, cfg)}
}

// WriteJSON encodes the report with stable indentation.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteMarkdown renders the summary and, per arm, the confusion matrix and
// cost rows. Tables and float formatting are fixed so replay is byte-identical.
func WriteMarkdown(w io.Writer, r Report) error {
	var b strings.Builder
	b.WriteString("# Experiment report\n")
	b.WriteString("\n## Summary\n\n")
	b.WriteString("| arm | items | errors | p50 ms | p95 ms | micro P | micro R | micro F1 | macro P | macro R | macro F1 | prompt tok | completion tok | cost usd | grounding | agreement |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, arm := range r.Arms {
		micro, macro := arm.Classification, arm.Classification
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %s | %s | %s | %s | %s | %s | %d | %d | %s | %s | %s |\n",
			mdCell(arm.Arm), arm.Items, arm.Errors,
			opt(arm.Latency.P50MS), opt(arm.Latency.P95MS),
			scorePart(micro, "micro", 0), scorePart(micro, "micro", 1), scorePart(micro, "micro", 2),
			scorePart(macro, "macro", 0), scorePart(macro, "macro", 1), scorePart(macro, "macro", 2),
			arm.Usage.PromptTokens, arm.Usage.CompletionTokens, costCell(arm.Usage),
			groundingCell(arm.Grounding), agreementCell(arm.Agreement))
	}
	for _, arm := range r.Arms {
		b.WriteString("\n## Confusion: ")
		b.WriteString(mdCell(arm.Arm))
		b.WriteString("\n\n")
		if arm.Classification == nil {
			b.WriteString("label path tidak diberikan\n")
			continue
		}
		writeConfusion(&b, *arm.Classification)
	}
	b.WriteString("\n## Cost\n\n| arm | items with cost | items missing cost | total usd |\n|---|---|---|---|\n")
	for _, arm := range r.Arms {
		fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", mdCell(arm.Arm), arm.Usage.ItemsWithCost, arm.Usage.ItemsMissingCost, costCell(arm.Usage))
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return nil
}

func writeConfusion(b *strings.Builder, c Classification) {
	b.WriteString("| actual \\ predicted |")
	for _, label := range c.Labels {
		b.WriteString(" ")
		b.WriteString(mdCell(label))
		b.WriteString(" |")
	}
	b.WriteString("\n|---|")
	for range c.Labels {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	counts := make(map[string]map[string]int, len(c.Confusion))
	for _, row := range c.Confusion {
		counts[row.Actual] = row.Predicted
	}
	for _, actual := range c.Labels {
		fmt.Fprintf(b, "| %s |", mdCell(actual))
		for _, predicted := range c.Labels {
			fmt.Fprintf(b, " %d |", counts[actual][predicted])
		}
		b.WriteString("\n")
	}
}

// WriteRatingCSV exports one row per arm×item for human rating of explanation
// quality. JSON cells are compacted so a spreadsheet keeps them on one line.
func WriteRatingCSV(w io.Writer, res Results, ds Dataset) error {
	labels := make(map[string]string, len(ds.Items))
	for _, item := range ds.Items {
		labels[item.ID] = string(item.Label)
	}
	c := csv.NewWriter(w)
	if err := c.Write([]string{"arm", "item_id", "run_id", "error_code", "error", "label", "output"}); err != nil {
		return err
	}
	for _, row := range res.Rows {
		output := string(row.Output)
		var compact any
		if len(row.Output) > 0 && json.Unmarshal(row.Output, &compact) == nil {
			if encoded, err := json.Marshal(compact); err == nil {
				output = string(encoded)
			}
		}
		if err := c.Write([]string{row.Arm, row.ItemID, row.RunID, row.ErrorCode, row.Err, labels[row.ItemID], output}); err != nil {
			return err
		}
	}
	c.Flush()
	return c.Error()
}

func scorePart(c *Classification, which string, part int) string {
	if c == nil {
		return "-"
	}
	s := c.Micro
	if which == "macro" {
		s = c.Macro
	}
	switch part {
	case 0:
		return opt(s.Precision)
	case 1:
		return opt(s.Recall)
	default:
		return opt(s.F1)
	}
}

func costCell(u Usage) string {
	if u.CostUSD == nil {
		if u.CostIncomplete {
			return "incomplete"
		}
		return "-"
	}
	return opt(u.CostUSD)
}

func groundingCell(g Grounding) string {
	if !g.Measured {
		return "n/a"
	}
	return fmt.Sprintf("%s (%d/%d)", opt(g.Rate), g.Grounded, g.Items)
}

func agreementCell(a Agreement) string {
	if !a.Measured {
		return "n/a"
	}
	return fmt.Sprintf("%s (%d/%d)", opt(a.Rate), a.Agreed, a.Items)
}

func opt(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(*v, 'g', -1, 64)
}

func mdCell(s string) string {
	if s == "" {
		return "(empty)"
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// sortKeys is used by tests to assert the map order the Markdown writer and
// the JSON encoder both produce.
func sortKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
