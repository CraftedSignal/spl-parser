package spl

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// This checks native carrier consistency, not runtime join support.
func TestWave4QuotedJoinIdentity(t *testing.T) {
	for _, tc := range []struct{ name, authored, identity string }{
		{"two backslashes", `"a\\b"`, `a\b`},
		{"four backslashes", `"a\\\\b"`, `a\\b`},
		{"escaped quote", `"a\"b"`, `a"b`},
		{"embedded boundary quotes", `'"edge"'`, `"edge"`},
		{"unknown escape", `"a\qb"`, `a\qb`},
		{"quoted plain", `"user"`, `user`},
		{"unquoted plain", `user`, `user`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := `index=main | join ` + tc.authored + ` [ search index=child | table ` + tc.authored + ` ]`
			parsed, semantic := ExtractConditions(query), AnalyzeQuery(query)
			data, err := json.Marshal(struct {
				Query    string
				Parsed   *ParseResult
				Semantic *SemanticQuery
			}{query, parsed, semantic})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("public carriers and diagnostics: %s", data)
			if parsed == nil || semantic == nil || len(parsed.Errors) != 0 || len(semantic.Errors) != 0 || len(parsed.Joins) != 1 || len(semantic.Joins) != 1 || parsed.Joins[0].Subsearch == nil || semantic.Joins[0].Subquery == nil {
				t.Fatalf("missing join carriers: parsed=%+v semantic=%+v", parsed, semantic)
			}
			join, sem := parsed.Joins[0], semantic.Joins[0]
			if !reflect.DeepEqual(join.Subsearch.GroupByFields, []string{tc.identity}) || !reflect.DeepEqual(sem.Subquery.GroupByFields, []string{tc.identity}) {
				t.Errorf("child table field: parsed=%q semantic=%q want=%q", join.Subsearch.GroupByFields, sem.Subquery.GroupByFields, tc.identity)
			}
			if !reflect.DeepEqual(join.JoinFields, []string{tc.identity}) || !reflect.DeepEqual(sem.JoinFields, []string{tc.identity}) {
				t.Errorf("join key: parsed=%q semantic=%q want=%q", join.JoinFields, sem.JoinFields, tc.identity)
			}
			if !slices.Contains(join.ExposedFields, tc.identity) || !slices.Contains(sem.ExposedFields, tc.identity) {
				t.Errorf("exposed fields missing decoded key: parsed=%q semantic=%q", join.ExposedFields, sem.ExposedFields)
			}
			if !reflect.DeepEqual(join.ExposedFields, []string{tc.identity}) || !reflect.DeepEqual(sem.ExposedFields, []string{tc.identity}) {
				t.Errorf("duplicate or changed exposed identities: parsed=%q semantic=%q", join.ExposedFields, sem.ExposedFields)
			}
			if parsed.Dataflow == nil || parsed.Dataflow.Complete || semantic.Dataflow == nil || semantic.Dataflow.Complete || len(parsed.Dataflow.Diagnostics) == 0 || parsed.Dataflow.Diagnostics[0].Code != "unsupported_stage_dataflow" {
				t.Errorf("unsupported join graph boundary changed: parsed=%+v semantic=%+v", parsed.Dataflow, semantic.Dataflow)
			}
			if got := ClassifyFieldProvenance(parsed, tc.identity); got != ProvenanceJoinKey {
				t.Errorf("decoded identity provenance=%q want=%q", got, ProvenanceJoinKey)
			}
		})
	}
}

func TestWave4JoinIdentityResourceAndClone(t *testing.T) {
	const query = `index=main | join "a\\b" [ search index=child | table "a\\b" ]`
	for _, semantic := range []bool{false, true} {
		limits := defaultParseLimits()
		b, cancel := newParseBudget(context.Background(), limits)
		parsed := extractConditionsInternal(query, b)
		if semantic {
			_ = semanticFromExtraction(parsed, b)
		}
		work := b.work
		cancel()
		if len(parsed.Errors) != 0 || len(parsed.Joins) != 1 {
			t.Fatalf("baseline: %+v", parsed)
		}
		for _, delta := range []int{0, 1} {
			limits = defaultParseLimits()
			limits.maxWork = work - delta
			var errors []string
			var partial bool
			if semantic {
				r := analyzeQueryContext(context.Background(), query, limits)
				errors = r.Errors
				partial = r.Dataflow != nil || r.Expression != nil || len(r.Conditions)+len(r.Joins)+len(r.Subsearches) != 0
			} else {
				r := extractConditionsContext(context.Background(), query, limits)
				errors = r.Errors
				partial = r.Dataflow != nil || r.Expression != nil || len(r.Conditions)+len(r.Joins)+len(r.Subsearches) != 0
			}
			if delta == 0 && (len(errors) != 0 || !partial) {
				t.Fatalf("exact work=%d semantic=%v errors=%v", work, semantic, errors)
			}
			if delta == 1 && (len(errors) != 1 || !strings.Contains(errors[0], "work budget") || partial) {
				t.Fatalf("one-short work=%d semantic=%v errors=%v partial=%v", work-1, semantic, errors, partial)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, s := ExtractConditionsContext(ctx, query), AnalyzeQueryContext(ctx, query)
	if len(p.Errors) != 1 || !strings.Contains(p.Errors[0], "context canceled") || p.Dataflow != nil || len(p.Joins) != 0 || len(s.Errors) != 1 || !strings.Contains(s.Errors[0], "context canceled") || s.Dataflow != nil || len(s.Joins) != 0 {
		t.Fatalf("cancellation returned partial model: %+v %+v", p, s)
	}
	p = ExtractConditions(query)
	b, cancel := newParseBudget(context.Background(), defaultParseLimits())
	s = semanticFromExtraction(p, b)
	cancel()
	p.Joins[0].JoinFields[0] = "changed"
	p.Joins[0].ExposedFields[0] = "changed"
	p.Joins[0].Subsearch.GroupByFields[0] = "changed"
	if !reflect.DeepEqual(s.Joins[0].JoinFields, []string{`a\b`}) || !reflect.DeepEqual(s.Joins[0].ExposedFields, []string{`a\b`}) || !reflect.DeepEqual(s.Joins[0].Subquery.GroupByFields, []string{`a\b`}) {
		t.Fatalf("semantic clone aliases mutable extraction: %+v", s.Joins[0])
	}
}
