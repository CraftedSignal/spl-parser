package spl

import (
	"context"
	"strings"
	"testing"
)

// Search grouping was added independently of resource limits. Its additional
// tree scans must consume the same work budget as extraction and conversion.
func TestSearchGroupingScansShareWorkBudget(t *testing.T) {
	expression := splSearchExpressionForTest(t, `alpha* beta gamma`)
	term := expression.AllSearchTerm()[0]
	for _, name := range []string{"unsupported operands", "bare word span", "predicate ancestry"} {
		t.Run(name, func(t *testing.T) {
			limits := defaultParseLimits()
			limits.maxWork = 0
			budget, cancel := newParseBudget(context.Background(), limits)
			defer cancel()
			defer func() {
				abort, ok := recover().(budgetAbort)
				if !ok || !strings.Contains(abort.err.Error(), "work budget") {
					t.Errorf("search scan escaped shared work budget: %#v (work=%d)", abort, budget.work)
				}
			}()
			switch name {
			case "unsupported operands":
				hasUnsupportedSearchOperand(expression, budget)
			case "bare word span":
				searchBareWordSpan(term.BareWord(), budget)
			case "predicate ancestry":
				bareWordIsSearchPredicate(term.BareWord(), budget)
			}
		})
	}
}

func TestConcurrentContextRequestsKeepBudgetsIndependent(t *testing.T) {
	for i := 0; i < 8; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if failed := ExtractConditionsContext(ctx, `alpha* OR beta gamma`); len(failed.Errors) != 1 || len(failed.Conditions) != 0 {
				t.Fatalf("canceled extraction returned partial output: %#v", failed)
			}
			semantic := AnalyzeQueryContext(context.Background(), "alpha* OR beta gamma `unknown`")
			if len(semantic.Errors) != 0 || len(semantic.Conditions) != 2 || semantic.Expression != nil || len(semantic.Warnings) != 1 {
				t.Fatalf("independent semantic request lost search semantics: %#v", semantic)
			}
			if alternatives := semantic.Conditions[0].Alternatives; len(alternatives) != 2 || alternatives[0] != "alpha*" || alternatives[1] != "beta" || semantic.Conditions[1].Value != "gamma" {
				t.Fatalf("independent request lost keyword/glob grouping: %#v", semantic.Conditions)
			}
			if stages, err := ClassifyPipelineStagesContext(context.Background(), `alpha* OR beta gamma | stats count`); err != nil || len(stages) != 2 {
				t.Fatalf("independent classification failed: stages=%v, error=%v", stages, err)
			}
		})
	}
}
