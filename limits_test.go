package spl

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExtractContextRejectsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := ExtractConditionsContext(ctx, `x=1 y=2`)
	if result == nil || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], "context canceled") || len(result.Conditions) != 0 {
		t.Fatalf("canceled parse returned conditions or no cancellation: %#v", result)
	}
}

func TestExtractionRejectsTinyBudgetsWithoutPartialConditions(t *testing.T) {
	tests := []struct {
		name, query, want string
		configure         func(*parseLimits)
	}{
		{"input", `x=1 y=2`, "input", func(l *parseLimits) { l.maxInputBytes = 4 }},
		{"nesting", `((x=1))`, "nesting", func(l *parseLimits) { l.maxNestingDepth = 1 }},
		{"tokens", `x=1 y=2`, "token", func(l *parseLimits) { l.maxTokens = 2 }},
		{"work", `x=1 y=2`, "work", func(l *parseLimits) { l.maxWork = 1 }},
		{"output", `x=1 y=2`, "output", func(l *parseLimits) { l.maxOutputItems = 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := defaultParseLimits()
			tt.configure(&limits)
			got := extractConditionsContext(context.Background(), tt.query, limits)
			if got == nil || len(got.Errors) == 0 || !strings.Contains(got.Errors[0], tt.want) || len(got.Conditions) != 0 {
				t.Fatalf("expected %s rejection without partial conditions: %#v", tt.want, got)
			}
		})
	}
}

func TestNestingBudgetIgnoresLiteralAndCommentDelimiters(t *testing.T) {
	limits := defaultParseLimits()
	limits.maxNestingDepth = 1
	got := extractConditionsContext(context.Background(), "x=\"(((\" ``` (((\n", limits)
	if got == nil || len(got.Errors) != 0 || len(got.Conditions) != 1 {
		t.Fatalf("literal/comment delimiters counted as syntax nesting: %#v", got)
	}
	if got.Conditions[0].Field != "x" || got.Conditions[0].Value != "(((" {
		t.Fatalf("literal semantics changed: %#v", got.Conditions)
	}
}

type panicDeadlineContext struct{ context.Context }

func (panicDeadlineContext) Deadline() (time.Time, bool) { panic("fixture panic") }

func TestPublicContextBoundaryReturnsFailureOnUnexpectedPanic(t *testing.T) {
	ctx := panicDeadlineContext{context.Background()}
	result := ExtractConditionsContext(ctx, "x=1")
	if len(result.Conditions) != 0 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "parser panic") {
		t.Fatalf("unexpected panic did not return failure only: %+v", result)
	}
	if stages, err := ClassifyPipelineStagesContext(ctx, "x=1"); stages != nil || err == nil || !strings.Contains(err.Error(), "parser panic") {
		t.Fatalf("classification panic did not return failure: stages=%v err=%v", stages, err)
	}
	semantic := AnalyzeQueryContext(ctx, "x=1")
	if len(semantic.Conditions) != 0 || semantic.Expression != nil || len(semantic.Errors) != 1 || !strings.Contains(semantic.Errors[0], "parser panic") {
		t.Fatalf("unexpected semantic panic did not return failure only: %+v", semantic)
	}
}

func TestClassifyContextUsesLimitsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := ClassifyPipelineStagesContext(ctx, `x=1 | stats count`); got != nil || err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("missing cancellation: %v, %v", got, err)
	}
	limits := defaultParseLimits()
	limits.maxTokens = 2
	if got, err := classifyPipelineStagesContext(context.Background(), `x=1 | stats count`, limits); got != nil || err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("missing token limit: %v, %v", got, err)
	}
}

func TestRecursiveExtractionSharesTokenBudget(t *testing.T) {
	limits := defaultParseLimits()
	limits.maxTokens = 12
	got := extractConditionsContext(context.Background(), `x=1 | join a [ y=2 ]`, limits)
	if len(got.Conditions) != 0 || len(got.Errors) == 0 || !strings.Contains(got.Errors[0], "token") {
		t.Fatalf("recursive parse reset budget: %#v", got)
	}
}

func TestAnalyzeContextRejectsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := AnalyzeQueryContext(ctx, "x=1")
	if got == nil || len(got.Errors) == 0 || !strings.Contains(got.Errors[0], "context canceled") || len(got.Conditions) != 0 {
		t.Fatalf("semantic entrypoint ignored cancellation: %#v", got)
	}
}

func TestAnalyzeSharesBudgetWithSemanticConversion(t *testing.T) {
	limits := defaultParseLimits()
	limits.maxOutputItems = 1
	query := "x=1"
	if result := extractConditionsContext(context.Background(), query, limits); len(result.Errors) != 0 || len(result.Conditions) != 1 {
		t.Fatalf("extraction must fit this budget: %+v", result)
	}
	result := analyzeQueryContext(context.Background(), query, limits)
	if len(result.Conditions) != 0 || result.Expression != nil || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "output items") {
		t.Fatalf("semantic conversion must share the exhausted budget without partial output: %+v", result)
	}
}

func TestQuotedEmbeddedSearchUsesSyntaxBudget(t *testing.T) {
	for _, query := range []string{
		`| dataset method=powerquery search="columns x (metadata (user='alice'))"`,
		`| ldapsearch search="(&(cn=Alice)(sn=Smith))"`,
	} {
		limits := defaultParseLimits()
		limits.maxNestingDepth = 1
		result := extractConditionsContext(context.Background(), query, limits)
		if len(result.Conditions) != 0 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "nesting") {
			t.Fatalf("interpreted quoted search escaped nesting budget: %+v", result)
		}
	}
}

func TestPortableDepthGuardRejectsInsteadOfTruncating(t *testing.T) {
	for _, invoke := range []func(*parseBudget){
		func(b *parseBudget) { _ = extractPortableSPLConditionsDepth(`search user="alice"`, 2, b) },
		func(b *parseBudget) { _ = extractSubsearchConditions(`search [ search user="alice" ]`, 2, b) },
	} {
		func() {
			limits := defaultParseLimits()
			limits.maxNestingDepth = 1
			budget, cancel := newParseBudget(context.Background(), limits)
			defer cancel()
			defer func() {
				abort, ok := recover().(budgetAbort)
				if !ok || !strings.Contains(abort.err.Error(), "nesting") {
					t.Fatalf("expected explicit depth rejection, got %+v", abort)
				}
			}()
			invoke(budget)
		}()
	}
}
