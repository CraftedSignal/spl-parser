package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSortDirectionPublic(t *testing.T) {
	for _, tc := range []struct {
		command string
		fields  []string
	}{
		{`sort 10 +size, -source desc`, []string{"size", "source"}},
		{`sort limit=10 +num(Alias), -str(source) d`, []string{"Alias", "source"}},
		{`sort desc`, []string{"desc"}},
		{`sort d`, []string{"d"}},
		{`sort -desc`, []string{"desc"}},
		{`sort host, desc`, []string{"host", "desc"}},
		{`sort "desc"`, []string{"desc"}},
		{`sort num(desc)`, []string{"desc"}},
		{`sort desc d`, []string{"desc"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			query := `search index=main | eval Alias=src | ` + tc.command + ` | where Alias="x"`
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			if len(p.Errors)+len(s.Errors) != 0 || p.Dataflow == nil || s.Dataflow == nil || !p.Dataflow.Complete || !s.Dataflow.Complete || !reflect.DeepEqual(p.GroupByFields, tc.fields) || !reflect.DeepEqual(s.GroupByFields, tc.fields) || !reflect.DeepEqual(p.Dataflow, s.Dataflow) || !reflect.DeepEqual(p.Conditions, s.Conditions) {
				t.Fatalf("sort models: errors=%v/%v fields=%q/%q complete=%v", p.Errors, s.Errors, p.GroupByFields, s.GroupByFields, p.Dataflow)
			}
			stages := ClassifyPipelineStages(query)
			if len(stages) != 4 || stages[2].CommandType != "sort" || stages[2].OriginalText != tc.command || stages[2].IsAggregation {
				t.Fatalf("classification: %+v", stages)
			}
			stage := p.Dataflow.Stages[2]
			if stage.Source != " "+tc.command+" " || stage.Command != "sort" || stage.Effect != "retain" || len(stage.Inputs) != 1 || stage.Inputs[0].QueryPath != "" || len(stage.BindingIDs) != 0 || stage.OutputScopeID == stage.Inputs[0].ScopeID {
				t.Fatalf("sort ownership: %+v", stage)
			}
			found := false
			for _, scope := range p.Dataflow.Scopes {
				if scope.ID == stage.OutputScopeID {
					found = true
					if scope.Mode != "inherit" || scope.ParentID != stage.Inputs[0].ScopeID || len(scope.BindingIDs)+len(scope.RemovedFields) != 0 {
						t.Fatalf("scope: %+v", scope)
					}
				}
			}
			if !found || len(p.Dataflow.Bindings) != 1 || len(p.Aggregations) != 0 || len(p.ComputedFields) != 1 {
				t.Fatal("sort changed stored fields")
			}
			seen := false
			for _, c := range p.Conditions {
				if c.Field == "Alias" {
					seen = true
					if c.InputScopeID != stage.OutputScopeID || c.FieldBinding == nil || c.FieldBinding.BindingID != p.Dataflow.Bindings[0].ID {
						t.Fatalf("alias lineage: %+v", c)
					}
				} else if c.Field != "index" {
					t.Fatalf("sort predicate: %+v", c)
				}
			}
			if !seen {
				t.Fatal("downstream condition missing")
			}
		})
	}
}

func TestSortDirectionMalformed(t *testing.T) {
	for _, command := range []string{`sort host desc desc`, `sort desc host`, `sort host descending`, `sort host "desc"`, `sort host Desc`, `sort host desc, source`} {
		t.Run(command, func(t *testing.T) {
			query := `search index=main | ` + command
			for _, stage := range ClassifyPipelineStages(query) {
				if stage.CommandType == "sort" {
					t.Fatalf("truncated sort: %+v", stage)
				}
			}
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			for _, flow := range []*QueryDataflow{p.Dataflow, s.Dataflow} {
				if flow == nil {
					continue
				}
				for _, stage := range flow.Stages {
					if stage.Command == "sort" && stage.Effect != "opaque" {
						t.Fatalf("recovered typed sort: %+v", stage)
					}
				}
			}
		})
	}
}

func TestSortDirectionBudgetAndClone(t *testing.T) {
	const query = `search index=main | eval Alias=src | sort -num(Alias) desc | where Alias="x"`
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
