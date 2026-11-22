package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSortWrapperPublic(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, sort, suffix string
		fields                     []string
		alias                      bool
	}{
		{"official signed count", `search index=main`, `sort 100 -num(size), +str(source)`, "", []string{"size", "source"}, false},
		{"four wrappers and alias", `search index=main | eval Alias=src`, `sort limit=12 auto(Alias), str("Fresh Name"), ip(src_ip), -num(Bytes)`, ` | where Alias="x"`, []string{"Alias", "Fresh Name", "src_ip", "Bytes"}, true},
		{"quoted escapes and paths", `search index=main`, `sort str("a\\b"), ip('"edge"'), auto(event.host)`, "", []string{`a\b`, `"edge"`, "event.host"}, false},
		{"plain quoted parity", `search index=main`, `sort -"Fresh Name", "a\\b"`, "", []string{"Fresh Name", `a\b`}, false},
		{"ordinary wrapper names", `search index=main`, `sort num, auto, Num, "str(field)"`, "", []string{"num", "auto", "Num", "str(field)"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := tc.prefix + " | " + tc.sort + tc.suffix
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			if len(p.Errors)+len(s.Errors) != 0 || p.Dataflow == nil || s.Dataflow == nil || !p.Dataflow.Complete || !s.Dataflow.Complete || !reflect.DeepEqual(p.GroupByFields, tc.fields) || !reflect.DeepEqual(s.GroupByFields, tc.fields) || !reflect.DeepEqual(p.Conditions, s.Conditions) || !reflect.DeepEqual(p.Dataflow, s.Dataflow) {
				t.Fatalf("sort models: errors=%v/%v fields=%q/%q", p.Errors, s.Errors, p.GroupByFields, s.GroupByFields)
			}
			index := 1
			if tc.alias {
				index = 2
			}
			stages := ClassifyPipelineStages(query)
			if len(stages) != len(p.Dataflow.Stages) || stages[index].CommandType != "sort" || stages[index].OriginalText != tc.sort || stages[index].IsAggregation {
				t.Fatalf("typed sort classification/source: %+v", stages)
			}
			stage := p.Dataflow.Stages[index]
			wantSource := " " + tc.sort
			if tc.suffix != "" {
				wantSource += " "
			}
			if stage.Command != "sort" || stage.Source != wantSource || stage.Effect != "retain" || len(stage.Inputs) != 1 || stage.Inputs[0].QueryPath != "" || len(stage.BindingIDs) != 0 || stage.OutputScopeID == stage.Inputs[0].ScopeID {
				t.Fatalf("sort source/retain: %+v", stage)
			}
			found := false
			for _, scope := range p.Dataflow.Scopes {
				if scope.ID == stage.OutputScopeID {
					found = true
					if scope.Mode != "inherit" || scope.ParentID != stage.Inputs[0].ScopeID || len(scope.BindingIDs)+len(scope.RemovedFields) != 0 {
						t.Fatalf("sort wrote fields: %+v", scope)
					}
				}
			}
			if !found || len(p.Aggregations)+len(s.Aggregations) != 0 {
				t.Fatal("sort acquired aggregate or missing output")
			}
			for _, c := range p.Conditions {
				if c.Field != "index" && (!tc.alias || c.Field != "Alias") {
					t.Fatalf("wrapper emitted predicate: %+v", c)
				}
			}
			if tc.alias {
				if len(p.Dataflow.Bindings) != 1 {
					t.Fatalf("wrapper computed fields: %+v", p.Dataflow.Bindings)
				}
				seen := false
				for _, c := range p.Conditions {
					if c.Field == "Alias" {
						seen = true
						if c.InputScopeID != stage.OutputScopeID || c.FieldBinding == nil || c.FieldBinding.BindingID != p.Dataflow.Bindings[0].ID {
							t.Fatalf("alias lineage lost: %+v", c)
						}
					}
				}
				if !seen {
					t.Fatal("downstream alias predicate lost")
				}
			} else if len(p.Dataflow.Bindings) != 0 || len(p.ComputedFields) != 0 {
				t.Fatal("wrapper replaced stored values")
			}
		})
	}
}

func TestSortWrapperMalformed(t *testing.T) {
	for _, command := range []string{`sort lower(host)`, `sort Num(host)`, `sort num()`, `sort num(a,b)`, `sort num(str(a))`, `sort num(a`, `sort num(a+1)`, `sort 100 -num(a), +lower(b)`} {
		t.Run(command, func(t *testing.T) {
			query := `search index=main | ` + command
			for _, stage := range ClassifyPipelineStages(query) {
				if stage.CommandType == "sort" {
					t.Fatalf("recovered/truncated typed sort classification: %+v", stage)
				}
			}
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			for _, flow := range []*QueryDataflow{p.Dataflow, s.Dataflow} {
				if flow == nil {
					continue
				}
				for _, stage := range flow.Stages {
					if stage.Command == "sort" && stage.Effect != "opaque" {
						t.Fatalf("unknown/malformed call became typed sort: %+v", stage)
					}
				}
			}
		})
	}
}

func TestSortWrapperBudgetAndClone(t *testing.T) {
	const query = `search index=main | eval Alias=src | sort -num(Alias) | where Alias="x"`
	for _, semantic := range []bool{false, true} {
		b, cancel := newParseBudget(context.Background(), defaultParseLimits())
		p := extractConditionsInternal(query, b)
		if len(p.Errors) != 0 || p.Dataflow == nil || !p.Dataflow.Complete || !reflect.DeepEqual(p.GroupByFields, []string{"Alias"}) {
			cancel()
			t.Fatalf("budget baseline: %+v", p)
		}
		if semantic {
			_ = semanticFromExtraction(p, b)
		}
		work, output := b.work, b.output
		cancel()
		for _, resource := range []string{"work", "output"} {
			for _, short := range []int{0, 1} {
				limits := defaultParseLimits()
				if resource == "work" {
					limits.maxWork = work - short
				} else {
					limits.maxOutputItems = output - short
				}
				var errs []string
				var partial bool
				if semantic {
					r := analyzeQueryContext(context.Background(), query, limits)
					errs = r.Errors
					partial = r.Dataflow != nil || len(r.Conditions)+len(r.GroupByFields) != 0 || r.Expression != nil
				} else {
					r := extractConditionsContext(context.Background(), query, limits)
					errs = r.Errors
					partial = r.Dataflow != nil || len(r.Conditions)+len(r.GroupByFields) != 0 || r.Expression != nil
				}
				if short == 0 && (len(errs) != 0 || !partial) {
					t.Fatalf("exact %s semantic=%v: %v", resource, semantic, errs)
				}
				if short == 1 && (len(errs) != 1 || partial || !strings.Contains(errs[0], "budget") && !strings.Contains(errs[0], "output items")) {
					t.Fatalf("one-short %s semantic=%v errors=%v partial=%v", resource, semantic, errs, partial)
				}
			}
		}
	}
	p := ExtractConditions(query)
	b, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	s := semanticFromExtraction(p, b)
	s.GroupByFields[0] = "changed"
	s.Dataflow.Stages[2].Source = "changed"
	s.Dataflow.Stages[2].Inputs[0].ScopeID = "changed"
	if p.GroupByFields[0] != "Alias" || p.Dataflow.Stages[2].Source == "changed" || p.Dataflow.Stages[2].Inputs[0].ScopeID == "changed" {
		t.Fatal("semantic sort clone shares ownership")
	}
}
