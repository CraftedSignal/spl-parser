package spl

import (
	"reflect"
	"testing"
)

func TestExtractConditions_StatsAggregations(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected []Aggregation
	}{
		{
			name:     "default count column",
			query:    `index=main action=failure | stats count by user | where count > 5`,
			expected: []Aggregation{{Command: "stats", Function: "count", Column: "count"}},
		},
		{
			name:     "alias",
			query:    `index=main action=failure | stats count as Failed by user | where Failed > 5`,
			expected: []Aggregation{{Command: "stats", Function: "count", Column: "Failed"}},
		},
		{
			name:     "default column of an aggregated field",
			query:    `index=main | stats dc(src) by user`,
			expected: []Aggregation{{Command: "stats", Function: "dc", Field: "src", Column: "dc(src)"}},
		},
		{
			name:  "several aggregations",
			query: `index=main | stats dc(src) as Sources, sum(bytes) as "Total Bytes" by user`,
			expected: []Aggregation{
				{Command: "stats", Function: "dc", Field: "src", Column: "Sources"},
				{Command: "stats", Function: "sum", Field: "bytes", Column: "Total Bytes"},
			},
		},
		{
			name:     "expression argument",
			query:    `index=main | stats count(eval(action="failure")) as failures by user`,
			expected: []Aggregation{{Command: "stats", Function: "count", Column: "failures"}},
		},
		{
			name:     "eventstats",
			query:    `index=main | eventstats count by user | where count > 5`,
			expected: []Aggregation{{Command: "eventstats", Function: "count", Column: "count"}},
		},
		{
			name:  "top",
			query: `index=main | top user | where count > 5`,
			expected: []Aggregation{
				{Command: "top", Function: "count", Column: "count"},
				{Command: "top", Function: "percent", Column: "percent"},
			},
		},
		{
			name:     "subsearch aggregation belongs to the subsearch",
			query:    `index=main [search index=other | stats count by user | fields user]`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractConditions(tt.query)
			got := make([]Aggregation, len(result.Aggregations))
			for i, aggregation := range result.Aggregations {
				aggregation.PipeStage = 0
				got[i] = aggregation
			}
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("aggregations = %+v, want %+v", result.Aggregations, tt.expected)
			}
		})
	}
}

func TestExtractConditions_FilterOnStatsDefaultColumn(t *testing.T) {
	for _, command := range []string{"where", "search"} {
		t.Run(command, func(t *testing.T) {
			result := ExtractConditions(`index=main action=failure | stats count by user | ` + command + ` count > 5`)
			if len(result.Aggregations) != 1 {
				t.Fatalf("expected one aggregation, got %+v", result.Aggregations)
			}
			stage := result.Aggregations[0].PipeStage
			for _, condition := range result.Conditions {
				switch condition.Field {
				case "action":
					if condition.PipeStage >= stage {
						t.Errorf("filter before stats has stage %d, aggregation stage %d", condition.PipeStage, stage)
					}
				case "count":
					if condition.Operator != ">" || condition.Value != "5" || condition.PipeStage <= stage {
						t.Errorf("unexpected filter on the count column: %+v (aggregation stage %d)", condition, stage)
					}
					return
				}
			}
			t.Fatalf("filter on the count column was not extracted: %+v", result.Conditions)
		})
	}
}
