package spl

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"strings"
	"testing"
)

// Public JSON projection lets the regression characterize missing metadata
// before the optional API is implemented.
type joinAliasTestInfo struct {
	LeftAlias  string `json:"left_alias"`
	RightAlias string `json:"right_alias"`
	Source     string `json:"source"`
	Pairs      []struct {
		LeftField  string `json:"left_field"`
		RightField string `json:"right_field"`
		Source     string `json:"source"`
	} `json:"pairs"`
	LeftInput  ScopeReference `json:"left_input"`
	RightInput ScopeReference `json:"right_input"`
}

func readJoinAliasTestInfo(t *testing.T, value any) *joinAliasTestInfo {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Alias *joinAliasTestInfo `json:"alias"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result.Alias
}

func TestJoinAliasPublic(t *testing.T) {
	for _, tc := range []struct {
		name, options, clause, left, right string
		pairs                              [][3]string
	}{
		{"witness", "left=L right=R", "where L.host=R.user", "L", "R", [][3]string{{"host", "user", "L.host=R.user"}}},
		{"reversed ordered pairs", "type=left left=L right=R max=1", "where R.user = L.host L.id=R.uid L.id=R.uid", "L", "R", [][3]string{{"host", "user", "R.user = L.host"}, {"id", "uid", "L.id=R.uid"}, {"id", "uid", "L.id=R.uid"}}},
		{"quoted dotted", `left="L.part" right="R side"`, `where "L.part.host.name"="R side.user.name"`, "L.part", "R side", [][3]string{{"host.name", "user.name", `"L.part.host.name"="R side.user.name"`}}},
		{"numeric and escaped", `left=12 right="R\\side"`, `where "12.host"="R\\side.user"`, "12", `R\side`, [][3]string{{"host", "user", `"12.host"="R\\side.user"`}}},
		{"overlapping unique", "left=A right=A.B", "where A.x=A.B.y", "A", "A.B", [][3]string{{"x", "y", "A.x=A.B.y"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stage := " join " + tc.options + " " + tc.clause + " [search index=other | table user] "
			query := "index=main |" + stage
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			if len(p.Errors)+len(s.Errors) != 0 || len(p.Joins) != 1 || len(s.Joins) != 1 {
				t.Fatalf("join parse: %+v / %+v", p, s)
			}
			if len(p.Joins[0].JoinFields)+len(p.Joins[0].ExposedFields)+len(s.Joins[0].JoinFields)+len(s.Joins[0].ExposedFields) != 0 {
				t.Fatal("alias equality flattened into event/legacy fields")
			}
			for _, conditions := range [][]Condition{p.Conditions, s.Conditions} {
				for _, c := range conditions {
					if c.Field != "index" {
						t.Fatalf("alias equality leaked into predicates: %+v", c)
					}
				}
			}
			for i, value := range []any{p.Joins[0], s.Joins[0]} {
				a := readJoinAliasTestInfo(t, value)
				if a == nil || a.LeftAlias != tc.left || a.RightAlias != tc.right || a.Source != tc.clause || len(a.Pairs) != len(tc.pairs) {
					t.Fatalf("alias metadata: %+v", a)
				}
				for n, pair := range a.Pairs {
					if pair.LeftField != tc.pairs[n][0] || pair.RightField != tc.pairs[n][1] || pair.Source != tc.pairs[n][2] {
						t.Fatalf("pair %d: %+v", n, pair)
					}
				}
				flow := p.Dataflow
				if i == 1 {
					flow = s.Dataflow
				}
				if flow == nil || flow.Complete || len(flow.Stages) != 2 {
					t.Fatalf("join must remain opaque: %+v", flow)
				}
				st := flow.Stages[1]
				if st.Source != stage || st.Effect != "opaque" || len(st.Inputs) != 2 || a.LeftInput != st.Inputs[0] || a.RightInput != st.Inputs[1] || a.LeftInput.QueryPath != "" || a.RightInput.QueryPath != "joins/0" || a.LeftInput.ScopeID == "" || a.RightInput.ScopeID == "" {
					t.Fatalf("stage/source owners: alias=%+v stage=%+v", a, st)
				}
			}
			if ClassifyFieldProvenance(p, "host") != ProvenanceAmbiguous {
				t.Fatal("alias key acquired unqualified provenance")
			}
		})
	}
}

func TestJoinAliasNestedAndLegacy(t *testing.T) {
	query := `index=main | join left=L right=R where L.host=R.user [search index=child | join left=L right=R where R.uid=L.id [search index=leaf]] | join left=L right=R where L.x=R.y [search index=second]`
	p, s := ExtractConditions(query), AnalyzeQuery(query)
	if len(p.Errors)+len(s.Errors) != 0 || len(p.Joins) != 2 || len(s.Joins) != 2 || p.Joins[0].Subsearch == nil || len(p.Joins[0].Subsearch.Joins) != 1 || s.Joins[0].Subquery == nil || len(s.Joins[0].Subquery.Joins) != 1 {
		t.Fatalf("nested joins: %+v / %+v", p, s)
	}
	for _, tc := range []struct {
		value any
		path  string
	}{{p.Joins[0], "joins/0"}, {p.Joins[1], "joins/1"}, {p.Joins[0].Subsearch.Joins[0], "joins/0"}, {s.Joins[0].Subquery.Joins[0], "joins/0"}} {
		a := readJoinAliasTestInfo(t, tc.value)
		if a == nil || a.LeftInput.QueryPath != "" || a.RightInput.QueryPath != tc.path || a.LeftInput.ScopeID == "" || a.RightInput.ScopeID == "" {
			t.Fatalf("wrong local canonical owner: %+v", a)
		}
	}
	for _, query := range []string{`index=main | join host [search index=other]`, `index=main | join [search index=other]`, `index=main | join left=L left=X right=R host [search index=other]`} {
		p, s := ExtractConditions(query), AnalyzeQuery(query)
		if len(p.Errors)+len(s.Errors) != 0 || len(p.Joins) != 1 || readJoinAliasTestInfo(t, p.Joins[0]) != nil || readJoinAliasTestInfo(t, s.Joins[0]) != nil {
			t.Fatalf("legacy join changed: %+v / %+v", p, s)
		}
		if strings.Contains(query, " host ") && !reflect.DeepEqual(p.Joins[0].JoinFields, []string{"host"}) {
			t.Fatal("legacy key lost")
		}
	}
}

func TestJoinAliasInvalid(t *testing.T) {
	for _, command := range []string{
		`join left=L where L.x=R.y`, `join left=L right=L where L.x=L.y`,
		`join left=L left=X right=R where L.x=R.y`, `join left="" right=R where L.x=R.y`,
		`join left=L right=R where X.x=R.y`, `join left=L right=R where L.x=L.y`,
		`join left=A right=A.B where A.B.x=A.B.y`, `join left=L right=R where l.x=R.y`,
		`join left=L right=R host where L.x=R.y`, `join left=L right=R where L.x>R.y`,
		`join left=L right=R where L.x=R.y AND L.z=R.z`, `join left=L right=R where L.x=R.y,L.z=R.z`,
		`join left=L right=R where L.x=R.y where L.z=R.z`,
	} {
		t.Run(command, func(t *testing.T) {
			query := `x=1 | ` + command + ` [search index=other]`
			p, s := ExtractConditions(query), AnalyzeQuery(query)
			if len(p.Errors) == 0 || len(s.Errors) == 0 {
				t.Fatalf("invalid alias join silently recovered: %+v / %+v", p, s)
			}
			for _, j := range p.Joins {
				if readJoinAliasTestInfo(t, j) != nil {
					t.Fatal("partially resolved carrier published")
				}
			}
		})
	}
}

func TestJoinAliasBudgetsAndCancellation(t *testing.T) {
	query := `index=main | join left=L right=R where L.host=R.user [search index=other]`
	for _, semantic := range []bool{false, true} {
		b, cancel := newParseBudget(context.Background(), defaultParseLimits())
		p := extractConditionsInternal(query, b)
		if len(p.Errors) != 0 || len(p.Joins) != 1 || readJoinAliasTestInfo(t, p.Joins[0]) == nil {
			cancel()
			t.Fatalf("baseline: %+v", p)
		}
		if semantic {
			_ = semanticFromExtraction(p, b)
		}
		work, output := b.work, b.output
		cancel()
		for _, resource := range []string{"work", "output"} {
			limits := defaultParseLimits()
			if resource == "work" {
				limits.maxWork = work - 1
			} else {
				limits.maxOutputItems = output - 1
			}
			var errors []string
			var partial bool
			if semantic {
				r := analyzeQueryContext(context.Background(), query, limits)
				errors = r.Errors
				partial = r.Dataflow != nil || len(r.Conditions)+len(r.Joins)+len(r.Subsearches) != 0
			} else {
				r := extractConditionsContext(context.Background(), query, limits)
				errors = r.Errors
				partial = r.Dataflow != nil || len(r.Conditions)+len(r.Joins)+len(r.Subsearches) != 0
			}
			if len(errors) != 1 || partial || !strings.Contains(errors[0], "budget") && !strings.Contains(errors[0], "output items") {
				t.Fatalf("non-atomic %s semantic=%v: errors=%v partial=%v", resource, semantic, errors, partial)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, s := ExtractConditionsContext(ctx, query), AnalyzeQueryContext(ctx, query)
	if len(p.Errors) != 1 || len(s.Errors) != 1 || len(p.Joins)+len(s.Joins) != 0 || p.Dataflow != nil || s.Dataflow != nil {
		t.Fatal("cancellation published partial model")
	}
}

func TestJoinAliasCloneOwnership(t *testing.T) {
	query := `index=main | join left=L right=R where L.host=R.user [search index=child | join left=L right=R where L.id=R.uid [search index=leaf]]`
	p := ExtractConditions(query)
	if len(p.Errors) != 0 || len(p.Joins) != 1 || p.Joins[0].Alias == nil {
		t.Fatalf("baseline: %+v", p)
	}
	b, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	s := semanticFromExtraction(p, b)
	if len(s.Joins) != 1 || s.Joins[0].Alias == nil || p.Joins[0].Alias == s.Joins[0].Alias || &p.Joins[0].Alias.Pairs[0] == &s.Joins[0].Alias.Pairs[0] || !reflect.DeepEqual(p.Joins[0].Alias, s.Joins[0].Alias) {
		t.Fatal("alias carrier/pairs were not independently cloned")
	}
	if p.Joins[0].Subsearch.Joins[0].Alias == s.Joins[0].Subquery.Joins[0].Alias {
		t.Fatal("nested alias carrier shared")
	}
	p.Joins[0].Alias.Pairs[0].LeftField = "changed"
	p.Joins[0].Alias.LeftInput.ScopeID = "changed"
	p.Joins[0].Subsearch.Joins[0].Alias.Pairs[0].RightField = "changed"
	if s.Joins[0].Alias.Pairs[0].LeftField != "host" || s.Joins[0].Alias.LeftInput.ScopeID == "changed" || s.Joins[0].Subquery.Joins[0].Alias.Pairs[0].RightField != "uid" {
		t.Fatal("extraction mutation reached semantic alias owner")
	}
}

func TestJoinAliasLegacyWhereOwnership(t *testing.T) {
	for _, tc := range []struct {
		query   string
		keys    []string
		options map[string]string
	}{
		{`index=main | join where host [search index=other]`, []string{"where", "host"}, map[string]string{}},
		{`index=main | join left=where right=R host [search index=other]`, []string{"host"}, map[string]string{"left": "where", "right": "R"}},
		{`index=main | join left=L right=R where [search index=other]`, []string{"where"}, map[string]string{"left": "L", "right": "R"}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			p, s := ExtractConditions(tc.query), AnalyzeQuery(tc.query)
			if len(p.Errors)+len(s.Errors) != 0 || len(p.Joins) != 1 || len(s.Joins) != 1 {
				t.Fatalf("legacy WHERE ownership errors: %v / %v", p.Errors, s.Errors)
			}
			if p.Joins[0].Alias != nil || s.Joins[0].Alias != nil || !reflect.DeepEqual(p.Joins[0].JoinFields, tc.keys) || !reflect.DeepEqual(s.Joins[0].JoinFields, tc.keys) || !maps.Equal(p.Joins[0].Options, tc.options) || !maps.Equal(s.Joins[0].Options, tc.options) {
				t.Fatalf("legacy fields/options changed: %+v / %+v", p.Joins[0], s.Joins[0])
			}
		})
	}
}
