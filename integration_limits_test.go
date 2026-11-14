package spl

import (
	"context"
	"fmt"
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
			keyword := fmt.Sprintf("alpha%d*", i)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if failed := ExtractConditionsContext(ctx, `alpha* OR beta gamma`); len(failed.Errors) != 1 || len(failed.Conditions) != 0 {
				t.Fatalf("canceled extraction returned partial output: %#v", failed)
			}
			semantic := AnalyzeQueryContext(context.Background(), keyword+" OR beta gamma `unknown`")
			if len(semantic.Errors) != 0 || len(semantic.Conditions) != 3 || semantic.Expression != nil || len(semantic.Warnings) != 1 {
				t.Fatalf("independent semantic request lost search semantics: %#v", semantic)
			}
			// The macro makes expression ancestry incomplete, so all portable
			// values remain separate instead of guessing which ORs are siblings.
			if semantic.Conditions[0].Value != keyword || semantic.Conditions[1].Value != "beta" || semantic.Conditions[2].Value != "gamma" ||
				len(semantic.Conditions[0].Alternatives) != 0 || len(semantic.Conditions[1].Alternatives) != 0 {
				t.Fatalf("independent request lost keyword/glob values: %#v", semantic.Conditions)
			}
			if stages, err := ClassifyPipelineStagesContext(context.Background(), keyword+` OR beta gamma | stats count`); err != nil || len(stages) != 2 {
				t.Fatalf("independent classification failed: stages=%v, error=%v", stages, err)
			}
		})
	}
}
