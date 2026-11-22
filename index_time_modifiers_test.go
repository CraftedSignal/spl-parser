package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func assertIndexTimeOpaque(t *testing.T, flow *QueryDataflow, conditions []Condition, expressionNil, incomplete bool, errs []string) {
	t.Helper()
	if len(errs) != 0 || flow == nil || flow.Complete || !incomplete || !expressionNil {
		t.Fatalf("index-time window claims complete predicates/graph: flow=%+v incomplete=%v nilExpr=%v errors=%v", flow, incomplete, expressionNil, errs)
	}
	for _, c := range conditions {
		if c.Context == PredicateSearch && (c.Field == "_index_earliest" || c.Field == "_index_latest") {
			t.Errorf("modifier became event predicate: %+v", c)
		}
	}
	stage := flow.Stages[0]
	if stage.Command != "search" || stage.Effect != "opaque" || len(stage.BindingIDs) != 0 {
		t.Errorf("search not opaque: %+v", stage)
	}
	found := false
	for _, scope := range flow.Scopes {
		if scope.ID == stage.OutputScopeID {
			found = scope.Mode == "opaque" && len(scope.BindingIDs) == 0
		}
	}
	if !found {
		t.Errorf("opaque output scope missing: %+v", flow.Scopes)
	}
	found = false
	for _, d := range flow.Diagnostics {
		if d.Code == "unsupported_stage_dataflow" && d.StageID == stage.ID && d.Message == "unsupported field scope for search" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing owning search diagnostic: %+v", flow.Diagnostics)
	}
}

func TestIndexTimeModifiersOpaque(t *testing.T) {
	for _, query := range []string{
		`search index=main _index_earliest=-1h status=ok`,
		`search index=main _index_latest=now status=ok`,
		`search index=main _index_earliest="-1h" status=ok`,
		`search index=main _index_latest="now" status=ok`,
		`search index=main _index_earliest=-1h _index_latest=now status=ok`,
		`index=main _index_earliest=-1h status=ok`,
		`_index_earliest=-1h`,
		`search _index_latest=now`,
		`search _index_earliest=-1h _index_latest=now`,
		`search status=ok OR _index_earliest=-1h`,
		`search _index_earliest=-1h OR status=ok`,
		`search NOT _index_earliest=-1h`,
		`search NOT (status=ok OR _index_latest=now)`,
	} {
		t.Run(query, func(t *testing.T) {
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			assertIndexTimeOpaque(t, p.Dataflow, p.Conditions, p.Expression == nil, p.ExpressionIncomplete, p.Errors)
			assertIndexTimeOpaque(t, s.Dataflow, s.Conditions, s.Expression == nil, s.ExpressionIncomplete, s.Errors)
			if p.Dataflow.Stages[0].Source != query || s.Dataflow.Stages[0].Source != query {
				t.Fatal("authored source changed")
			}
			if len(p.Conditions) != len(s.Conditions) {
				t.Fatal("semantic condition counts differ")
			}
			for i := range p.Conditions {
				if !reflect.DeepEqual(p.Conditions[i], s.Conditions[i]) {
					t.Fatal("semantic condition ownership/roles differ")
				}
			}
			want := "boolean expression unavailable: query contains an unsupported or unrepresented boolean operand"
			if !strings.Contains(strings.Join(p.Warnings, "\n"), want) || !strings.Contains(strings.Join(s.Warnings, "\n"), want) {
				t.Fatalf("missing incomplete warning: %v / %v", p.Warnings, s.Warnings)
			}
		})
	}
}

func TestIndexTimeModifierContextControls(t *testing.T) {
	for _, tc := range []struct{ query, field, kind, value string }{
		{`search index=main earliest=-1h latest=now status=ok`, "status", "bare", "ok"},
		{`search index=main _indextime=123 status=ok`, "_indextime", "number", "123"},
		{`search index=main "_index_earliest=-1h" status=ok`, "_raw", "string", "_index_earliest=-1h"},
		{`search status="_index_earliest"`, "status", "string", "_index_earliest"},
		{`search index=main | where '_index_earliest'="x"`, "_index_earliest", "string", "x"},
		{`search index=main | where _index_latest="x"`, "_index_latest", "string", "x"},
		{`search index=main | where 'earliest'="x"`, "earliest", "string", "x"},
		{`search _INDEX_EARLIEST=-1h`, "_INDEX_EARLIEST", "bare", "-1h"},
		{`search _index_earliest!=-1h`, "_index_earliest", "bare", "-1h"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			p, s := ExtractConditions(tc.query), AnalyzeQuery(tc.query)
			for _, a := range []struct {
				flow       *QueryDataflow
				conditions []Condition
				incomplete bool
				errors     []string
			}{{p.Dataflow, p.Conditions, p.ExpressionIncomplete, p.Errors}, {s.Dataflow, s.Conditions, s.ExpressionIncomplete, s.Errors}} {
				if len(a.errors) != 0 || a.flow == nil || !a.flow.Complete || a.incomplete {
					t.Fatalf("unchanged context lost: %+v", a)
				}
				found := false
				for _, c := range a.conditions {
					if c.Field == tc.field {
						found = c.LHS != nil && c.LHS.Kind == OperandField && c.RHS != nil && string(c.RHS.Kind) == tc.kind && c.RHS.Value == tc.value
					}
				}
				if !found {
					t.Fatalf("typed context control missing: %+v", a.conditions)
				}
			}
		})
	}
	p, s := ExtractConditions(`search index=main "_index_earliest"="x"`), AnalyzeQuery(`search index=main "_index_earliest"="x"`)
	if len(p.Errors)+len(s.Errors) != 0 || !p.ExpressionIncomplete || !s.ExpressionIncomplete || p.Dataflow != nil || s.Dataflow != nil || p.Expression != nil || s.Expression != nil {
		t.Fatalf("quoted search LHS boundary changed: %+v / %+v", p, s)
	}
	// The existing name-only compatibility classifier is intentionally unchanged.
	if IsSearchScopeMetadata("_index_earliest") || IsSearchScopeMetadata("_index_latest") || !IsSearchScopeMetadata("earliest") {
		t.Fatal("global scope-name map changed")
	}
}

func TestIndexTimeModifierChildOwnership(t *testing.T) {
	for _, childOpaque := range []bool{false, true} {
		parent := `search index=main parent=ok`
		child := `search index=child child=ok`
		if childOpaque {
			child = `search index=child _index_earliest=-1h child=ok`
		} else {
			parent = `search index=main _index_earliest=-1h parent=ok`
		}
		query := parent + ` | append [` + child + `]`
		p, s := ExtractConditions(query), AnalyzeQuery(query)
		if len(p.Errors)+len(s.Errors) != 0 || len(p.Joins) != 1 || len(s.Joins) != 1 || p.Joins[0].Subsearch == nil || s.Joins[0].Subquery == nil {
			t.Fatalf("child parse failed: %+v / %+v", p, s)
		}
		pc, sc := p.Joins[0].Subsearch, s.Joins[0].Subquery
		if childOpaque {
			assertIndexTimeOpaque(t, pc.Dataflow, pc.Conditions, pc.Expression == nil, pc.ExpressionIncomplete, pc.Errors)
			assertIndexTimeOpaque(t, sc.Dataflow, sc.Conditions, sc.Expression == nil, sc.ExpressionIncomplete, sc.Errors)
			if p.ExpressionIncomplete || s.ExpressionIncomplete || p.Dataflow.Stages[0].Effect != "filter" || s.Dataflow.Stages[0].Effect != "filter" {
				t.Fatal("child modifier leaked into parent search")
			}
		} else {
			assertIndexTimeOpaque(t, p.Dataflow, p.Conditions, p.Expression == nil, p.ExpressionIncomplete, p.Errors)
			assertIndexTimeOpaque(t, s.Dataflow, s.Conditions, s.Expression == nil, s.ExpressionIncomplete, s.Errors)
			if pc.ExpressionIncomplete || sc.ExpressionIncomplete || pc.Dataflow == nil || sc.Dataflow == nil || !pc.Dataflow.Complete || !sc.Dataflow.Complete {
				t.Fatal("parent modifier leaked into child")
			}
		}
		if pc.Dataflow.Stages[0].Source != child || sc.Dataflow.Stages[0].Source != child {
			t.Fatal("child source changed")
		}
		for _, c := range p.Conditions {
			if c.Field == "child" {
				t.Fatal("child condition leaked to parent")
			}
		}
		inputs := p.Dataflow.Stages[1].Inputs
		if len(inputs) != 2 || inputs[1].QueryPath != "joins/0" || inputs[1].ScopeID != pc.Dataflow.OutputScopeID {
			t.Fatalf("child graph ownership: %+v", inputs)
		}
	}
}

func TestIndexTimeModifierLaterReferenceCloneAndBudgets(t *testing.T) {
	const query = `search index=main _index_earliest=-1h status=ok | where status="x"`
	p, s := ExtractConditions(query), AnalyzeQuery(query)
	assertIndexTimeOpaque(t, p.Dataflow, p.Conditions, p.Expression == nil, p.ExpressionIncomplete, p.Errors)
	assertIndexTimeOpaque(t, s.Dataflow, s.Conditions, s.Expression == nil, s.ExpressionIncomplete, s.Errors)
	for _, conditions := range [][]Condition{p.Conditions, s.Conditions} {
		last := conditions[len(conditions)-1]
		if last.Context != PredicateEval || last.Field != "status" || last.FieldBinding == nil || last.FieldBinding.Kind != "unknown" || last.FieldBinding.BindingID != "" {
			t.Fatalf("later reference fabricated known output: %+v", last)
		}
	}
	b, cancel := newParseBudget(context.Background(), defaultParseLimits())
	clone := semanticFromExtraction(p, b)
	cancel()
	p.Conditions[0].RHS.Value = "mutated"
	p.Dataflow.Stages[0].Source = "mutated"
	p.Dataflow.Scopes[1].Mode = "mutated"
	if clone.Conditions[0].RHS.Value != "main" || clone.Dataflow.Stages[0].Source != `search index=main _index_earliest=-1h status=ok ` || clone.Dataflow.Scopes[1].Mode != "opaque" {
		t.Fatal("semantic clone aliases extraction")
	}
	for _, q := range []string{query, `_index_earliest=-1h`, `search index=main | append [search _index_latest=now]`} {
		for _, semantic := range []bool{false, true} {
			b, cancel := newParseBudget(context.Background(), defaultParseLimits())
			p := extractConditionsInternal(q, b)
			if semantic {
				_ = semanticFromExtraction(p, b)
			}
			work, output := b.work, b.output
			cancel()
			if len(p.Errors) != 0 {
				t.Fatalf("budget fixture parse: %+v", p)
			}
			for _, resource := range []struct {
				name string
				used int
				set  func(*parseLimits, int)
			}{{"work budget", work, func(l *parseLimits, n int) { l.maxWork = n }}, {"output items", output, func(l *parseLimits, n int) { l.maxOutputItems = n }}} {
				for _, short := range []bool{false, true} {
					l := defaultParseLimits()
					n := resource.used
					if short {
						n--
					}
					resource.set(&l, n)
					var errs []string
					var partial bool
					if semantic {
						r := analyzeQueryContext(context.Background(), q, l)
						errs = r.Errors
						partial = r.Dataflow != nil || len(r.Conditions)+len(r.Joins)+len(r.Subsearches) > 0 || r.Expression != nil
					} else {
						r := extractConditionsContext(context.Background(), q, l)
						errs = r.Errors
						partial = r.Dataflow != nil || len(r.Conditions)+len(r.Joins)+len(r.Subsearches) > 0 || r.Expression != nil
					}
					if !short && (len(errs) != 0 || !partial) || short && (len(errs) != 1 || !strings.Contains(errs[0], resource.name) || partial) {
						t.Fatalf("exact/short %s semantic=%v short=%v: %v partial=%v", resource.name, semantic, short, errs, partial)
					}
				}
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		p, s := ExtractConditionsContext(ctx, q), AnalyzeQueryContext(ctx, q)
		if p.Dataflow != nil || s.Dataflow != nil || len(p.Conditions)+len(s.Conditions)+len(p.Joins)+len(s.Joins) != 0 || len(p.Errors) != 1 || len(s.Errors) != 1 || !strings.Contains(p.Errors[0], "context canceled") || !strings.Contains(s.Errors[0], "context canceled") {
			t.Fatalf("canceled partial model: %+v / %+v", p, s)
		}
	}
}

func TestIndexTimeModifierOnlyChildDoesNotRecoverIntoParent(t *testing.T) {
	const query = `| append [search _index_earliest=-1h]`
	p, s := ExtractConditions(query), AnalyzeQuery(query)
	if len(p.Errors)+len(s.Errors) != 0 || len(p.Joins) != 1 || len(s.Joins) != 1 {
		t.Fatalf("child parse: %+v / %+v", p, s)
	}
	if len(p.Conditions) != 0 || len(s.Conditions) != 0 {
		t.Fatalf("portable recovery leaked child predicate into parent: %+v / %+v", p.Conditions, s.Conditions)
	}
	child, semanticChild := p.Joins[0].Subsearch, s.Joins[0].Subquery
	assertIndexTimeOpaque(t, child.Dataflow, child.Conditions, child.Expression == nil, child.ExpressionIncomplete, child.Errors)
	assertIndexTimeOpaque(t, semanticChild.Dataflow, semanticChild.Conditions, semanticChild.Expression == nil, semanticChild.ExpressionIncomplete, semanticChild.Errors)
}
