package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func wave4bImplicitAggregateScope(t *testing.T, command string) {
	for _, query := range []string{
		`index=main | stats avg`,
		`index=main | stats count avg`,
		`index=main | stats count avg by host`,
	} {
		query = strings.Replace(query, "stats", command, 1)
		t.Run(query, func(t *testing.T) {
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			for _, view := range []struct {
				flow       *QueryDataflow
				aggs       []Aggregation
				errors     []string
				incomplete bool
			}{
				{p.Dataflow, p.Aggregations, p.Errors, p.ExpressionIncomplete},
				{s.Dataflow, s.Aggregations, s.Errors, s.ExpressionIncomplete},
			} {
				if len(view.errors) != 0 || view.incomplete || view.flow == nil {
					t.Fatalf("grammar/filter coverage changed: %+v", view)
				}
				if view.flow.Complete {
					t.Errorf("implicit field expansion claims a complete finite graph: %+v", view.flow)
				}
				stage := view.flow.Stages[1]
				if stage.Effect != "opaque" || len(stage.BindingIDs) != 0 || len(view.flow.Bindings) != 0 || stage.Source != strings.TrimPrefix(query, "index=main |") {
					t.Errorf("partial/fabricated stats output or changed source: %+v", view.flow)
				}
				found := false
				for _, d := range view.flow.Diagnostics {
					if d.Code == "unsupported_stage_dataflow" && d.StageID == stage.ID {
						found = true
					}
				}
				if !found {
					t.Errorf("missing exact stats unsupported-stage boundary: %+v", view.flow)
				}
				for _, a := range view.aggs {
					if a.StageID != "" || a.BindingID != "" {
						t.Errorf("legacy summary claims graph binding: %+v", a)
					}
				}
				last := view.aggs[len(view.aggs)-1]
				if last.Function != "avg" || last.Field != "" || last.Column != "avg" {
					t.Errorf("legacy summary changed: %+v", view.aggs)
				}
			}
		})
	}
}

func wave4bFixedAggregateScopeControls(t *testing.T, command string) {
	for _, tc := range []struct{ query, column, field string }{
		{`index=main | stats count`, "count", ""},
		{`index=main | stats COUNT`, "COUNT", ""},
		{`index=main | stats count()`, "count()", ""},
		{`index=main | stats count as Total`, "Total", ""},
		{`index=main | stats avg(bytes)`, "avg(bytes)", "bytes"},
		{`index=main | stats avg(bytes) as Mean`, "Mean", "bytes"},
	} {
		tc.query = strings.Replace(tc.query, "stats", command, 1)
		p, s := ExtractConditions(tc.query), AnalyzeQuery(tc.query)
		if len(p.Errors)+len(s.Errors) != 0 || p.Dataflow == nil || s.Dataflow == nil || !p.Dataflow.Complete || !s.Dataflow.Complete || !reflect.DeepEqual(p.Aggregations, s.Aggregations) || len(p.Aggregations) != 1 {
			t.Fatalf("fixed output control %q: %+v / %+v", tc.query, p, s)
		}
		a := p.Aggregations[0]
		if a.Column != tc.column || a.Field != tc.field || a.BindingID == "" || a.StageID == "" || len(p.Dataflow.Bindings) != 1 || p.Dataflow.Bindings[0].Field != tc.column || p.Dataflow.Bindings[0].ID != a.BindingID {
			t.Fatalf("fixed field/owner %q: %+v", tc.query, p)
		}
	}
}

func wave4bImplicitAggregateBudgetAndClone(t *testing.T, command string) {
	query := "index=main | " + command + " count avg by host"
	for _, semantic := range []bool{false, true} {
		b, cancel := newParseBudget(context.Background(), defaultParseLimits())
		p := extractConditionsInternal(query, b)
		if semantic {
			_ = semanticFromExtraction(p, b)
		}
		work := b.work
		cancel()
		for _, short := range []bool{false, true} {
			l := defaultParseLimits()
			l.maxWork = work
			if short {
				l.maxWork--
			}
			var errs []string
			var partial bool
			if semantic {
				r := analyzeQueryContext(context.Background(), query, l)
				errs = r.Errors
				partial = r.Dataflow != nil || len(r.Aggregations)+len(r.Conditions) > 0
			} else {
				r := extractConditionsContext(context.Background(), query, l)
				errs = r.Errors
				partial = r.Dataflow != nil || len(r.Aggregations)+len(r.Conditions) > 0
			}
			if !short && (len(errs) != 0 || !partial) || short && (len(errs) != 1 || !strings.Contains(errs[0], "work budget") || partial) {
				t.Fatalf("work exact/one-short semantic=%v short=%v errors=%v partial=%v", semantic, short, errs, partial)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, s := ExtractConditionsContext(ctx, query), AnalyzeQueryContext(ctx, query)
	if len(p.Errors) != 1 || len(s.Errors) != 1 || p.Dataflow != nil || s.Dataflow != nil || len(p.Aggregations)+len(s.Aggregations) != 0 || !strings.Contains(p.Errors[0], "context canceled") || !strings.Contains(s.Errors[0], "context canceled") {
		t.Fatalf("canceled partial models: %+v / %+v", p, s)
	}
	p = ExtractConditions(query)
	b, cancel := newParseBudget(context.Background(), defaultParseLimits())
	s = semanticFromExtraction(p, b)
	cancel()
	p.Aggregations[0].Column = "changed"
	p.Dataflow.Stages[1].Source = "changed"
	if s.Aggregations[0].Column != "count" || s.Dataflow.Stages[1].Source != " "+command+" count avg by host" {
		t.Fatal("semantic clone aliases extracted summary/source")
	}
}

func wave4bImplicitAggregateLaterReference(t *testing.T, command string) {
	query := "index=main | " + command + " avg | where avg>1"
	p, s := ExtractConditions(query), AnalyzeQuery(query)
	for _, c := range [][]Condition{p.Conditions, s.Conditions} {
		last := c[len(c)-1]
		if last.Field != "avg" || last.Operator != ">" || last.Value != "1" || last.FieldBinding == nil || last.FieldBinding.Kind != "unknown" || last.FieldBinding.BindingID != "" {
			t.Fatalf("fabricated finite output reference: %+v", last)
		}
	}
	if len(p.Errors)+len(s.Errors) != 0 || p.Dataflow == nil || s.Dataflow == nil || p.Dataflow.Complete || s.Dataflow.Complete || p.Dataflow.Stages[1].Source != " "+command+" avg " {
		t.Fatalf("opaque source/diagnostics lost: %+v / %+v", p, s)
	}
}

func TestWave4bEventStreamImplicitAggregates(t *testing.T) {
	for _, command := range []string{"eventstats", "streamstats"} {
		t.Run(command, func(t *testing.T) {
			t.Run("implicit", func(t *testing.T) { wave4bImplicitAggregateScope(t, command) })
			t.Run("fixed", func(t *testing.T) { wave4bFixedAggregateScopeControls(t, command) })
			t.Run("budget_clone_cancel", func(t *testing.T) { wave4bImplicitAggregateBudgetAndClone(t, command) })
			t.Run("later_reference", func(t *testing.T) { wave4bImplicitAggregateLaterReference(t, command) })
			for _, call := range []string{"avg(bytes)", "count"} {
				query := "index=main | " + command + " " + call + " by user"
				p, s := ExtractConditions(query), AnalyzeQuery(query)
				if len(p.Errors)+len(s.Errors) != 0 || p.Dataflow == nil || s.Dataflow == nil || !p.Dataflow.Complete || !s.Dataflow.Complete || !reflect.DeepEqual(p.Aggregations, s.Aggregations) || len(p.Aggregations) != 1 {
					t.Fatalf("grouped fixed control %q: %+v / %+v", query, p, s)
				}
				a := p.Aggregations[0]
				if a.Command != command || len(p.GroupByFields) != 1 || p.GroupByFields[0] != "user" || a.BindingID == "" || a.StageID == "" || a.Column != call {
					t.Fatalf("grouped identity %q: %+v", query, a)
				}
			}
		})
	}
}
