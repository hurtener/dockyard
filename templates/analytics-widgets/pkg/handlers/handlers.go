// Package handlers implements the three analytics-widgets tool handlers.
//
// Each handler is a thin pure function over a typed contract: it receives
// the decoded input, builds the structured payload the App renders, and
// returns it via tool.Result. The data is synthetic but realistic — a
// customer-health metric, a revenue-by-month chart, a top-accounts table —
// so a developer who runs `dockyard new --template analytics-widgets`
// immediately sees something meaningful in the inspector (decision D-124).
//
// Swap to a real data source by replacing the body of each handler with a
// call to your service or database — the typed contract is the integration
// surface, not the handler internals.
package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/hurtener/dockyard/runtime/tool"
	"github.com/hurtener/dockyard/templates/analytics-widgets/pkg/contracts"
)

// CreateChart is the create_chart tool handler. It accepts a typed
// CreateChartInput, normalises the theme, picks a state based on the
// payload (empty when no series; ready otherwise), and returns the
// structured payload the App's chart renderer consumes.
func CreateChart(_ context.Context, in contracts.CreateChartInput) (tool.Result[contracts.CreateChartOutput], error) {
	out := contracts.CreateChartOutput{
		Kind:    "chart",
		Type:    in.Type,
		Data:    in.Data,
		Title:   in.Title,
		Options: in.Options,
		Theme:   resolveTheme(in.Theme),
		State:   contracts.WidgetStateReady,
	}
	if len(out.Data.Series) == 0 || allSeriesEmpty(out.Data.Series) {
		out.State = contracts.WidgetStateEmpty
		out.Message = "No data for this period."
	}
	text := fmt.Sprintf("chart: %s — %d series", chartTitleOrType(in), len(out.Data.Series))
	return tool.Result[contracts.CreateChartOutput]{
		Text:       text,
		Structured: out,
	}, nil
}

// CreateTable is the create_table tool handler.
func CreateTable(_ context.Context, in contracts.CreateTableInput) (tool.Result[contracts.CreateTableOutput], error) {
	out := contracts.CreateTableOutput{
		Kind:    "table",
		Columns: in.Columns,
		Rows:    in.Rows,
		Sort:    in.Sort,
		Theme:   resolveTheme(in.Theme),
		State:   contracts.WidgetStateReady,
	}
	if len(out.Rows) == 0 {
		out.State = contracts.WidgetStateEmpty
		out.Message = "No rows match this query."
	}
	text := fmt.Sprintf("table: %d columns × %d rows", len(out.Columns), len(out.Rows))
	return tool.Result[contracts.CreateTableOutput]{
		Text:       text,
		Structured: out,
	}, nil
}

// CreateMetricCard is the create_metric_card tool handler.
func CreateMetricCard(_ context.Context, in contracts.CreateMetricCardInput) (tool.Result[contracts.CreateMetricCardOutput], error) {
	out := contracts.CreateMetricCardOutput{
		Kind:       "metric_card",
		Label:      in.Label,
		Value:      in.Value,
		Unit:       in.Unit,
		Delta:      in.Delta,
		Series:     in.Series,
		Breakdowns: in.Breakdowns,
		Theme:      resolveTheme(in.Theme),
		State:      contracts.WidgetStateReady,
	}
	if strings.TrimSpace(in.Label) == "" || in.Value == nil {
		out.State = contracts.WidgetStateEmpty
		out.Message = "No metric reported."
	}
	text := fmt.Sprintf("metric: %s = %v%s", in.Label, in.Value, in.Unit)
	return tool.Result[contracts.CreateMetricCardOutput]{
		Text:       text,
		Structured: out,
	}, nil
}

// resolveTheme collapses "auto" / unset to "auto" (the App-side honours the
// host context) and pins explicit overrides. The handler never resolves
// "auto" against a host theme — the App does that, with the host's
// `styles.variables` (RFC §7.3).
func resolveTheme(t contracts.ThemeMode) contracts.ThemeMode {
	switch t {
	case contracts.ThemeLight, contracts.ThemeDark:
		return t
	default:
		return contracts.ThemeAuto
	}
}

// allSeriesEmpty reports whether every series in a chart payload is empty.
// Used to drive the empty state when a chart is "shaped" but carries no
// data — the inspector's empty fixture exercises this.
func allSeriesEmpty(series []contracts.ChartSeries) bool {
	for _, s := range series {
		if len(s.Values) > 0 {
			return false
		}
	}
	return true
}

// chartTitleOrType is a small helper for the model-facing text content: the
// chart's title when set, otherwise its type.
func chartTitleOrType(in contracts.CreateChartInput) string {
	if strings.TrimSpace(in.Title) != "" {
		return in.Title
	}
	if in.Type == "" {
		return "untyped"
	}
	return string(in.Type)
}
