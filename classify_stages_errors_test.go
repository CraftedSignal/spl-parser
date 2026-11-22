package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestClassifyStagesRejectsSyntaxErrors(t *testing.T) {
	for _, query := range []string{
		`index=web | where x >`,
		`index=web | timechart span=1h count | where x >`,
	} {
		t.Run(query, func(t *testing.T) {
			if parsed := ExtractConditions(query); len(parsed.Errors) == 0 {
				t.Fatal("fixture must have a native parser syntax diagnostic")
			}
			assertClassifierSyntaxFailure(t, query)
		})
	}
}

func TestClassifyStagesRejectsLexerErrors(t *testing.T) {
	for _, query := range []string{`index=web & | stats count`, "index=web \x00 | stats count"} {
		t.Run(query, func(t *testing.T) {
			// Observe a real lexer diagnostic independently of classifier recovery.
			budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
			defer cancel()
			listener := &errorListener{budget: budget}
			preflight(query, budget, true, listener)
			if len(listener.errors) == 0 || !strings.Contains(listener.errors[0], "token recognition error") {
				t.Fatalf("fixture must have a lexer diagnostic, got %v", listener.errors)
			}
			assertClassifierSyntaxFailure(t, query)
		})
	}
}

func assertClassifierSyntaxFailure(t *testing.T, query string) {
	t.Helper()
	if stages := ClassifyPipelineStages(query); stages != nil {
		t.Errorf("wrapper returned partial stages: %+v", stages)
	}
	if stages, err := ClassifyPipelineStagesContext(context.Background(), query); stages != nil || err != nil {
		t.Errorf("context classifier = %+v, %v; want nil stages and nil syntax error", stages, err)
	}
}

func TestClassifyStagesPreservesGrammaticalStages(t *testing.T) {
	for _, tc := range []struct {
		query string
		last  string
		agg   bool
	}{
		{`index=web | stats count`, "stats", true},
		{`index=web | timechart span=1h count`, "timechart", true},
		{`index=web | where x="&"`, "where", false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			parts := strings.Split(tc.query, " | ")
			want := []PipelineStageInfo{
				{Index: 0, CommandType: "search", OriginalText: parts[0]},
				{Index: 1, CommandType: tc.last, IsAggregation: tc.agg, OriginalText: parts[1]},
			}
			got, err := ClassifyPipelineStagesContext(context.Background(), tc.query)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("classifier = %+v, %v; want %+v", got, err, want)
			}
			if got := ClassifyPipelineStages(tc.query); !reflect.DeepEqual(got, want) {
				t.Fatalf("wrapper = %+v; want %+v", got, want)
			}
		})
	}
}

func TestClassifyStagesPreservesUnparsedTailRejection(t *testing.T) {
	for _, query := range []string{
		`index=web | timechart span=1h count | trendline sma5(count) as trend`,
		`index=web | head 5 | timechart span=1h count | trendline sma5(count) as trend`,
	} {
		assertClassifierSyntaxFailure(t, query)
	}
}

func TestClassifyStagesSyntaxFailurePreservesContextAndLimits(t *testing.T) {
	query := `index=web | where x >`
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := ClassifyPipelineStagesContext(ctx, query); got != nil || err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("canceled classification = %+v, %v", got, err)
	}
	limits := defaultParseLimits()
	limits.maxTokens = 2
	if got, err := classifyPipelineStagesContext(context.Background(), query, limits); got != nil || err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("limited classification = %+v, %v", got, err)
	}
}
