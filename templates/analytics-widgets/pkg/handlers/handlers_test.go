package handlers

import (
	"context"
	"testing"

	"github.com/hurtener/dockyard/templates/analytics-widgets/pkg/contracts"
)

// TestCreateChart_HappyAndEmpty exercises the create_chart handler on a
// realistic payload + the empty-series edge so the state-routing logic is
// covered.
func TestCreateChart_HappyAndEmpty(t *testing.T) {
	t.Parallel()
	in := contracts.CreateChartInput{
		Type: contracts.ChartType("bar"),
		Data: contracts.ChartData{
			Series:     []contracts.ChartSeries{{Name: "Revenue", Values: []float64{1, 2, 3}}},
			Categories: []string{"Jan", "Feb", "Mar"},
		},
		Title: "Revenue by month",
	}
	res, err := CreateChart(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateChart: %v", err)
	}
	if res.Structured.Kind != "chart" {
		t.Errorf("Kind = %q, want chart", res.Structured.Kind)
	}
	if res.Structured.State != contracts.WidgetStateReady {
		t.Errorf("State = %q, want ready", res.Structured.State)
	}

	empty := in
	empty.Data.Series = []contracts.ChartSeries{}
	out, err := CreateChart(context.Background(), empty)
	if err != nil {
		t.Fatalf("CreateChart empty: %v", err)
	}
	if out.Structured.State != contracts.WidgetStateEmpty {
		t.Errorf("empty-series State = %q, want empty", out.Structured.State)
	}
}

// TestCreateTable_HappyAndEmpty exercises the create_table handler.
func TestCreateTable_HappyAndEmpty(t *testing.T) {
	t.Parallel()
	in := contracts.CreateTableInput{
		Columns: []contracts.TableColumn{
			{Key: "name", Label: "Account", Type: contracts.TableColumnType("string"), Sortable: true},
			{Key: "arr", Label: "ARR", Type: contracts.TableColumnType("currency"), Sortable: true},
		},
		Rows: []map[string]any{
			{"name": "Acme", "arr": 120000},
			{"name": "Globex", "arr": 95000},
		},
	}
	res, err := CreateTable(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if res.Structured.Kind != "table" || res.Structured.State != contracts.WidgetStateReady {
		t.Errorf("table: Kind = %q, State = %q", res.Structured.Kind, res.Structured.State)
	}

	in.Rows = nil
	out, err := CreateTable(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateTable empty: %v", err)
	}
	if out.Structured.State != contracts.WidgetStateEmpty {
		t.Errorf("empty-rows State = %q, want empty", out.Structured.State)
	}
}

// TestCreateMetricCard_HappyAndEmpty exercises the create_metric_card handler.
func TestCreateMetricCard_HappyAndEmpty(t *testing.T) {
	t.Parallel()
	in := contracts.CreateMetricCardInput{
		Label:  "Customer health",
		Value:  87,
		Unit:   "/100",
		Delta:  &contracts.MetricDelta{Value: "+3", Tone: contracts.MetricDeltaTone("ok")},
		Series: []float64{82, 85, 84, 87},
	}
	res, err := CreateMetricCard(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateMetricCard: %v", err)
	}
	if res.Structured.Kind != "metric_card" || res.Structured.State != contracts.WidgetStateReady {
		t.Errorf("metric: Kind = %q, State = %q", res.Structured.Kind, res.Structured.State)
	}

	empty := contracts.CreateMetricCardInput{Label: " "}
	out, err := CreateMetricCard(context.Background(), empty)
	if err != nil {
		t.Fatalf("CreateMetricCard empty: %v", err)
	}
	if out.Structured.State != contracts.WidgetStateEmpty {
		t.Errorf("empty-label State = %q, want empty", out.Structured.State)
	}
}

// TestResolveTheme covers the theme normalisation path.
func TestResolveTheme(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   contracts.ThemeMode
		want contracts.ThemeMode
	}{
		{contracts.ThemeAuto, contracts.ThemeAuto},
		{contracts.ThemeLight, contracts.ThemeLight},
		{contracts.ThemeDark, contracts.ThemeDark},
		{"", contracts.ThemeAuto},
		{"unknown", contracts.ThemeAuto},
	}
	for _, tt := range tests {
		if got := resolveTheme(tt.in); got != tt.want {
			t.Errorf("resolveTheme(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
