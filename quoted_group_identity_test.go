package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

func TestQuotedGroupIdentityPublic(t *testing.T) {
	for _, tc := range []struct {
		literal string
		field   string
	}{
		{`"a\\b"`, "a\\b"},
		{`"a\\\\b"`, "a\\\\b"},
		{`"a\"b"`, `a"b`},
		{`'a\\b'`, "a\\b"},
		{`"\"edge\""`, `"edge"`},
		{`'"edge"'`, `"edge"`},
		{`"a\qb"`, `a\qb`},
		{`"user"`, "user"},
		{`user`, "user"},
		{`"a b"`, "a b"},
	} {
		t.Run(tc.literal, func(t *testing.T) {
			query := "index=main | stats count by " + tc.literal
			r := ExtractConditions(query)
			a := AnalyzeQuery(query)
			for _, result := range []struct {
				api        string
				groups     []string
				flow       *QueryDataflow
				incomplete bool
				errors     []string
				warnings   []string
			}{
				{"extract", r.GroupByFields, r.Dataflow, r.ExpressionIncomplete, r.Errors, r.Warnings},
				{"analyze", a.GroupByFields, a.Dataflow, a.ExpressionIncomplete, a.Errors, a.Warnings},
			} {
				if len(result.errors) != 0 || len(result.warnings) != 0 || result.incomplete || result.flow == nil || !result.flow.Complete {
					t.Fatalf("%s lost complete parse: %#v", result.api, result)
				}
				if len(result.groups) != 1 {
					t.Fatalf("%s groups: %#v", result.api, result.groups)
				}
				if result.groups[0] != tc.field {
					t.Errorf("%s group bytes: got %v want %v", result.api, []byte(result.groups[0]), []byte(tc.field))
				}
				groups := 0
				for _, binding := range result.flow.Bindings {
					if binding.Kind != "group" {
						continue
					}
					groups++
					if binding.Field != tc.field || binding.Expression != tc.literal || binding.StageID != "t1" || binding.InputScopeID != "s1" {
						t.Errorf("%s identity/source/owner changed: %#v", result.api, binding)
					}
				}
				if groups != 1 || result.flow.Stages[1].Source != " stats count by "+tc.literal {
					t.Errorf("%s group stage/source changed: %#v", result.api, result.flow)
				}
			}
		})
	}
}

func TestQuotedGroupIdentitySharedCallers(t *testing.T) {
	for _, tc := range []struct {
		command  string
		complete bool
		binding  string
	}{
		{`stats count by "a\\b"`, true, "group"},
		{`eventstats count by "a\\b"`, true, ""},
		{`streamstats count by "a\\b"`, true, ""},
		{`chart count by "a\\b"`, false, ""},
		{`dedup "a\\b"`, true, ""},
		{`fields "a\\b"`, false, ""},
		{`table "a\\b"`, true, "passthrough"},
		{`top "a\\b"`, false, ""},
		{`rare "a\\b"`, false, ""},
		{`transaction "a\\b"`, false, ""},
	} {
		t.Run(tc.command, func(t *testing.T) {
			query := "index=main | " + tc.command
			r, a := ExtractConditions(query), AnalyzeQuery(query)
			if len(r.Errors) != 0 || len(a.Errors) != 0 || !reflect.DeepEqual(r.GroupByFields, []string{"a\\b"}) || !reflect.DeepEqual(a.GroupByFields, []string{"a\\b"}) {
				t.Fatalf("shared caller identity: extract=%#v semantic=%#v", r, a)
			}
			for _, flow := range []*QueryDataflow{r.Dataflow, a.Dataflow} {
				if flow == nil || flow.Complete != tc.complete || flow.Stages[1].Source != " "+tc.command {
					t.Fatalf("actual stage boundary/source changed: %#v", flow)
				}
				found := 0
				for _, b := range flow.Bindings {
					if b.Kind == tc.binding {
						found++
						if b.Field != "a\\b" || b.Expression != `"a\\b"` {
							t.Fatalf("binding identity/authored source changed: %#v", b)
						}
					}
				}
				if tc.binding != "" && found != 1 {
					t.Fatalf("expected existing %s binding, got %#v", tc.binding, flow)
				}
			}
		})
	}
}

func TestQuotedGroupIdentityPreservesControls(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{`index=main | stats count by "a\\b", "a\\\\b"`, []string{"a\\b", "a\\\\b"}},
		{`index=main | stats count by "a*b"`, []string{"a*b"}},
		{`index=main | stats count by "earliest", "STATS", user`, []string{"user"}},
		{`index=main | fields - "a\\b"`, nil},
	} {
		r, a := ExtractConditions(tc.query), AnalyzeQuery(tc.query)
		if len(r.Errors) != 0 || len(a.Errors) != 0 || !reflect.DeepEqual(r.GroupByFields, tc.want) || !reflect.DeepEqual(a.GroupByFields, tc.want) {
			t.Fatalf("control %q: extract=%#v semantic=%#v", tc.query, r, a)
		}
		if strings.Contains(tc.query, "fields -") {
			if r.Dataflow == nil || !r.Dataflow.Complete || !reflect.DeepEqual(r.Dataflow.Scopes[len(r.Dataflow.Scopes)-1].RemovedFields, []string{"a\\b"}) {
				t.Fatalf("negative fields changed: %#v", r.Dataflow)
			}
		}
	}
}

func TestQuotedGroupIdentityChildAndCloneOwnership(t *testing.T) {
	query := `index=main | join key [ search index=other | stats count by "child\\name" ] | stats count by "root\\name"`
	r := ExtractConditions(query)
	if len(r.Errors) != 0 || len(r.Joins) != 1 || r.Joins[0].Subsearch == nil {
		t.Fatalf("nested fixture failed: %#v", r)
	}
	child := r.Joins[0].Subsearch
	if !reflect.DeepEqual(r.GroupByFields, []string{"root\\name"}) || !reflect.DeepEqual(child.GroupByFields, []string{"child\\name"}) {
		t.Fatalf("root/child inventories changed: root=%q child=%q", r.GroupByFields, child.GroupByFields)
	}
	if child.Dataflow == nil || !child.Dataflow.Complete || r.Dataflow == nil || r.Dataflow.Complete {
		t.Fatalf("join opaque boundary changed: root=%#v child=%#v", r.Dataflow, child.Dataflow)
	}
	if r.Dataflow.Stages[1].Inputs[1].QueryPath != "joins/0" || child.Dataflow.Bindings[0].Field != "child\\name" || child.Dataflow.Bindings[0].Expression != `"child\\name"` {
		t.Fatalf("child scope/source changed: %#v", r)
	}
	b, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	a := semanticFromExtraction(r, b)
	if !reflect.DeepEqual(a.GroupByFields, r.GroupByFields) || !reflect.DeepEqual(a.Joins[0].Subquery.GroupByFields, child.GroupByFields) {
		t.Fatalf("clone identity changed: %#v", a)
	}
	r.GroupByFields[0], child.GroupByFields[0], child.Dataflow.Bindings[0].Field = "changed", "changed", "changed"
	if a.GroupByFields[0] != "root\\name" || a.Joins[0].Subquery.GroupByFields[0] != "child\\name" || a.Joins[0].Subquery.Dataflow.Bindings[0].Field != "child\\name" {
		t.Fatalf("semantic clone aliases extraction: %#v", a)
	}
	projected := `index=main | join key [ search index=other | table "child\\name" ] | stats count by "root\\name"`
	r, a = ExtractConditions(projected), AnalyzeQuery(projected)
	if len(r.Errors) != 0 || len(a.Errors) != 0 || !reflect.DeepEqual(r.GroupByFields, []string{"root\\name"}) || !reflect.DeepEqual(r.Joins[0].ExposedFields, []string{"child\\name", "key"}) || !reflect.DeepEqual(a.Joins[0].ExposedFields, []string{"child\\name", "key"}) {
		t.Fatalf("projected child exposure leaked or lost identity: extract=%#v semantic=%#v", r, a)
	}
}

func quotedGroupList(t *testing.T, text string) IFieldListContext {
	t.Helper()
	lexer := NewSPLLexer(antlr.NewInputStream(text))
	parser := NewSPLParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	listener := &errorListener{}
	lexer.RemoveErrorListeners()
	parser.RemoveErrorListeners()
	lexer.AddErrorListener(listener)
	parser.AddErrorListener(listener)
	ctx := parser.FieldList()
	if len(listener.errors) != 0 {
		t.Fatalf("invalid field-list fixture: %v", listener.errors)
	}
	return ctx
}

func quotedGroupPanic(call func()) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = parseFailure(value)
		}
	}()
	call()
	return nil
}

func TestQuotedGroupIdentityPrechargeAndSliceSize(t *testing.T) {
	ctx := quotedGroupList(t, `"a\\b"`)
	for _, limit := range []int{4, 3} {
		limits := defaultParseLimits()
		limits.maxWork = limit
		b, cancel := newParseBudget(context.Background(), limits)
		e := &conditionExtractor{budget: b}
		err := quotedGroupPanic(func() { e.extractByFields(ctx) })
		cancel()
		if limit == 4 {
			if err != nil || b.work != 4 || b.output != 0 || !reflect.DeepEqual(e.groupByFields, []string{"a\\b"}) {
				t.Errorf("exact decode charge: work=%d output=%d groups=%q error=%v", b.work, b.output, e.groupByFields, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "work budget") || b.work != 3 || len(e.groupByFields) != 0 {
			t.Errorf("one-short preallocation guard: work=%d groups=%q error=%v", b.work, e.groupByFields, err)
		}
	}
	for _, text := range []string{`user, host`, `"a\\b", "c\\d"`} {
		for _, limit := range []int{2, 1} {
			limits := defaultParseLimits()
			limits.maxOutputItems = limit
			b, cancel := newParseBudget(context.Background(), limits)
			e := &conditionExtractor{budget: b}
			err := quotedGroupPanic(func() { e.extractByFields(quotedGroupList(t, text)) })
			cancel()
			if b.output != 0 || len(e.groupByFields) != limit || (limit == 2 && err != nil) || (limit == 1 && (err == nil || !strings.Contains(err.Error(), "output items"))) {
				t.Errorf("group slice boundary/no cumulative double charge: text=%q limit=%d groups=%q output=%d error=%v", text, limit, e.groupByFields, b.output, err)
			}
		}
	}
	limits := defaultParseLimits()
	limits.maxWork = len("earliest") - 1
	b, cancel := newParseBudget(context.Background(), limits)
	defer cancel()
	e := &conditionExtractor{budget: b}
	if err := quotedGroupPanic(func() { e.extractByFields(quotedGroupList(t, `"earliest"`)) }); err == nil || !strings.Contains(err.Error(), "work budget") || len(e.groupByFields) != 0 {
		t.Fatalf("excluded field skipped precharge: error=%v groups=%q", err, e.groupByFields)
	}
	// The existing nil-budget convention is retained for isolated helpers.
	e = &conditionExtractor{}
	e.extractByFields(ctx)
	if !reflect.DeepEqual(e.groupByFields, []string{"a\\b"}) {
		t.Fatalf("nil-budget helper changed: %q", e.groupByFields)
	}
}

func TestQuotedGroupIdentityPublicBudgetsFailAtomically(t *testing.T) {
	for _, query := range []string{
		`search index=main | stats count by "a\\b"`,
		`search index=main | join key [ search index=other | stats count by "child\\name" ] | stats count by "root\\name"`,
	} {
		for _, semantic := range []bool{false, true} {
			limits := defaultParseLimits()
			b, cancel := newParseBudget(context.Background(), limits)
			parsed := extractConditionsInternal(query, b)
			if semantic {
				_ = semanticFromExtraction(parsed, b)
			}
			work, output := b.work, b.output
			cancel()
			if len(parsed.Errors) != 0 || work < 2 || output < 2 {
				t.Fatalf("resource baseline failed: %#v work=%d output=%d", parsed, work, output)
			}
			for _, dimension := range []string{"input bytes", "work budget", "output items"} {
				for _, short := range []bool{false, true} {
					delta := 0
					if short {
						delta = 1
					}
					limits = defaultParseLimits()
					switch dimension {
					case "input bytes":
						limits.maxInputBytes = len(query) - delta
					case "work budget":
						limits.maxWork = work - delta
					case "output items":
						limits.maxOutputItems = output - delta
					}
					var errors []string
					var partial bool
					if semantic {
						r := analyzeQueryContext(context.Background(), query, limits)
						errors = r.Errors
						partial = r.Dataflow != nil || r.Expression != nil || len(r.Conditions)+len(r.GroupByFields)+len(r.Aggregations)+len(r.Joins)+len(r.Subsearches) != 0
					} else {
						r := extractConditionsContext(context.Background(), query, limits)
						errors = r.Errors
						partial = r.Dataflow != nil || r.Expression != nil || len(r.Conditions)+len(r.GroupByFields)+len(r.Aggregations)+len(r.Joins)+len(r.Subsearches) != 0
					}
					if !short && (len(errors) != 0 || !partial) {
						t.Fatalf("exact %s semantic=%v: errors=%v partial=%v", dimension, semantic, errors, partial)
					}
					if short && (len(errors) != 1 || !strings.Contains(errors[0], dimension) || partial) {
						t.Fatalf("one-short %s semantic=%v: errors=%v partial=%v", dimension, semantic, errors, partial)
					}
				}
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	query := `search index=main | stats count by "a\\b"`
	r, a := ExtractConditionsContext(ctx, query), AnalyzeQueryContext(ctx, query)
	if len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "context canceled") || r.Dataflow != nil || r.Expression != nil || len(r.Conditions)+len(r.GroupByFields)+len(r.Aggregations) != 0 {
		t.Fatalf("canceled extraction published partial: %#v", r)
	}
	if len(a.Errors) != 1 || !strings.Contains(a.Errors[0], "context canceled") || a.Dataflow != nil || a.Expression != nil || len(a.Conditions)+len(a.GroupByFields)+len(a.Aggregations) != 0 {
		t.Fatalf("canceled semantic view published partial: %#v", a)
	}
}
