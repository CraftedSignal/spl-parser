package spl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

func publicObject(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func TestEvalBindingsAreOrderedAndOccurrenceOwned(t *testing.T) {
	for _, first := range []string{"lower(x)", "upper(x)"} {
		query := `search index=main | eval y = ` + first + ` | where y = "a" | eval y = lower(x) | where y = "a"`
		result := ExtractConditions(query)
		if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete {
			t.Fatalf("dataflow failed: %#v", result)
		}
		flow := result.Dataflow
		if len(flow.Bindings) != 2 || flow.Bindings[0].Expression != first || flow.Bindings[1].Expression != "lower(x)" || flow.Bindings[0].ID == flow.Bindings[1].ID {
			t.Fatalf("lost ordered eval expressions: %#v", flow.Bindings)
		}
		var filtered []Condition
		for _, c := range result.Conditions {
			if c.Field == "y" {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) != 2 || filtered[0].FieldBinding == nil || filtered[1].FieldBinding == nil ||
			filtered[0].FieldBinding.BindingID != flow.Bindings[0].ID || filtered[1].FieldBinding.BindingID != flow.Bindings[1].ID ||
			filtered[0].InputScopeID == filtered[1].InputScopeID {
			t.Fatalf("filters lost occurrence scope: %#v", filtered)
		}
		semantic := AnalyzeQuery(query)
		if semantic.Dataflow == nil || len(semantic.Dataflow.Bindings) != 2 || semantic.Dataflow.Bindings[0].Expression != first {
			t.Fatalf("semantic flow lost history: %#v", semantic.Dataflow)
		}
	}
}

func TestEvalSiblingsReadSuccessiveScopesAndCaseIsExact(t *testing.T) {
	result := ExtractConditions(`search index=main | eval Alias = source, z = Alias | where Alias = "a" AND alias = "b"`)
	if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete {
		t.Fatalf("unexpected result: %#v", result)
	}
	bindings := result.Dataflow.Bindings
	if len(bindings) != 2 || bindings[0].Field != "Alias" || bindings[1].InputScopeID == bindings[0].InputScopeID {
		t.Fatalf("sibling eval scopes collapsed: %#v", bindings)
	}
	var upper, lower *Condition
	for i := range result.Conditions {
		if result.Conditions[i].Field == "Alias" {
			upper = &result.Conditions[i]
		}
		if result.Conditions[i].Field == "alias" {
			lower = &result.Conditions[i]
		}
	}
	if upper == nil || lower == nil || upper.FieldBinding == nil || lower.FieldBinding == nil ||
		upper.FieldBinding.Kind != "binding" || upper.FieldBinding.BindingID != bindings[0].ID || lower.FieldBinding.Kind != "input" || lower.IsComputed {
		t.Fatalf("exact-case provenance failed: upper=%#v lower=%#v", upper, lower)
	}
}

func TestQuotedEvalNamesKeepExactIdentity(t *testing.T) {
	query := `search index=main | eval "Alias"=src | where 'Alias'="a" AND 'alias'="b"`
	result := ExtractConditions(query)
	if len(result.Errors) != 0 || result.Dataflow == nil || len(result.Dataflow.Bindings) != 1 {
		t.Fatalf("quoted eval parse failed: %#v", result)
	}
	if result.Dataflow.Bindings[0].Field != "Alias" {
		t.Fatalf("quoted field identity lost: %#v", result.Dataflow.Bindings)
	}
	if _, ok := result.ComputedFields["Alias"]; !ok {
		t.Fatalf("quoted assignment missing final compatibility key: %#v", result.ComputedFields)
	}
	var upper, lower *Condition
	for i := range result.Conditions {
		if result.Conditions[i].Field == "Alias" {
			upper = &result.Conditions[i]
		}
		if result.Conditions[i].Field == "alias" {
			lower = &result.Conditions[i]
		}
	}
	if upper == nil || lower == nil || upper.FieldBinding.Kind != "binding" || lower.FieldBinding.Kind != "input" {
		t.Fatalf("quoted case binding failed: upper=%#v lower=%#v", upper, lower)
	}
}

func TestAuthoredStageAndRHSWithComments(t *testing.T) {
	query := "search index=main | eval Alias = lower( x ) ```note\n | where Alias = \"a\""
	result := ExtractConditions(query)
	if result.Dataflow == nil || !result.Dataflow.Complete || len(result.Dataflow.Bindings) != 1 {
		t.Fatalf("unexpected dataflow: %#v", result)
	}
	if result.Dataflow.Bindings[0].Expression != "lower( x )" || !strings.Contains(result.Dataflow.Stages[1].Source, "Alias = lower( x ) ```note\n ") {
		t.Fatalf("authored source lost: %#v", result.Dataflow)
	}
}

func TestMappedBacktickCommentMatchesExistingNormalizer(t *testing.T) {
	query := "search index=main | eval Alias = lower( x ) ```note\n | where Alias = \"a\""
	spans := make([]sourceRuneSpan, 0, len(query))
	for i := range query {
		spans = append(spans, sourceRuneSpan{start: i, end: i + 1})
	}
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	mapped, _ := mapSPLBacktickComments(query, spans, budget)
	if mapped != stripSPLBacktickComments(query) {
		t.Fatalf("comment mapping changed normalizer: mapped=%q canonical=%q", mapped, stripSPLBacktickComments(query))
	}
}

func TestAuthoredUnicodeWhitespaceSurvivesStageSource(t *testing.T) {
	query := "search index=main | eval\u00a0Alias=lower( x ) | where Alias=\"a\""
	result := ExtractConditions(query)
	if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete ||
		len(result.Dataflow.Bindings) != 1 || result.Dataflow.Bindings[0].Expression != "lower( x )" ||
		!strings.Contains(result.Dataflow.Stages[1].Source, "eval\u00a0Alias") {
		t.Fatalf("Unicode source mapping failed: %#v", result)
	}
}

func TestJoinOwnsChildScopeWithoutParentLeakage(t *testing.T) {
	query := `search index=main | eval Parent=src | join key [ search index=other | eval Child=src | table key Child ] | where Parent="a"`
	result := ExtractConditions(query)
	if len(result.Errors) != 0 || result.Dataflow == nil || len(result.Joins) != 1 || result.Joins[0].Subsearch == nil {
		t.Fatalf("join parse failed: %#v", result)
	}
	if len(result.Dataflow.Stages) != 4 || len(result.Dataflow.Stages[2].Inputs) != 2 ||
		result.Dataflow.Stages[2].Inputs[1].QueryPath != "joins/0" ||
		result.Dataflow.Stages[2].Inputs[1].ScopeID != result.Joins[0].Subsearch.Dataflow.OutputScopeID {
		t.Fatalf("join child reference missing: %#v", result.Dataflow)
	}
	if len(result.Dataflow.Bindings) != 1 || result.Dataflow.Bindings[0].Field != "Parent" ||
		len(result.Joins[0].Subsearch.Dataflow.Bindings) != 3 {
		t.Fatalf("parent/child fields leaked: parent=%#v child=%#v", result.Dataflow.Bindings, result.Joins[0].Subsearch.Dataflow.Bindings)
	}
	if len(result.Subsearches) != 1 || result.Subsearches[0].Dataflow == result.Joins[0].Subsearch.Dataflow {
		t.Fatalf("compatibility child graph aliased: %#v", result.Subsearches)
	}
}

func TestNestedChildPathsAndSelectedRootOutput(t *testing.T) {
	query := `search index=main | eval Shared=src | join key [ search index=other | eval Shared=other | join key [ search index=third | eval Shared=third ] | table key Shared ] | where Shared="a"`
	result := ExtractConditions(query)
	if len(result.Errors) != 0 || result.Dataflow == nil || len(result.Joins) != 1 ||
		result.Joins[0].Subsearch == nil || result.Joins[0].Subsearch.Dataflow == nil || len(result.Joins[0].Subsearch.Joins) != 1 {
		t.Fatalf("nested child extraction failed: %#v", result)
	}
	outerChild := result.Joins[0].Subsearch
	innerChild := outerChild.Joins[0].Subsearch
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	resolved, err := resolveDataflowPath(result, "joins/0/joins/0", budget)
	if err != nil || resolved != innerChild || innerChild == nil || innerChild.Dataflow == nil {
		t.Fatalf("nested path did not resolve relative to owner: result=%#v err=%v", resolved, err)
	}
	rootStages := result.Dataflow.Stages
	childStages := outerChild.Dataflow.Stages
	if len(rootStages) != 4 || len(rootStages[2].Inputs) != 2 || rootStages[2].Inputs[1].QueryPath != "joins/0" ||
		result.Dataflow.OutputScopeID != rootStages[3].OutputScopeID || rootStages[3].PipeStage <= rootStages[2].PipeStage+1 ||
		len(childStages) < 3 || !strings.Contains(childStages[1].Source, "eval Shared=other") ||
		len(childStages[2].Inputs) != 2 || childStages[2].Inputs[1].QueryPath != "joins/0" ||
		!strings.Contains(innerChild.Dataflow.Stages[1].Source, "eval Shared=third") ||
		len(result.Dataflow.Bindings) != 1 || result.Dataflow.Bindings[0].Field != "Shared" ||
		result.Conditions[len(result.Conditions)-1].FieldBinding.Kind != "unknown" {
		t.Fatalf("nested child ownership, source or stage gaps lost: root=%#v child=%#v", result, outerChild)
	}
	if len(result.Subsearches) != 1 || result.Subsearches[0] == nil || result.Subsearches[0].Dataflow == outerChild.Dataflow {
		t.Fatalf("legacy duplicate child graph aliased: %#v", result.Subsearches)
	}
	result.Subsearches[0].Dataflow.Stages[1].Source = "mutated"
	if outerChild.Dataflow.Stages[1].Source == "mutated" {
		t.Fatal("legacy duplicate child shared mutable stage graph")
	}
}

func TestStatsClosesOutputAndEventstatsInherits(t *testing.T) {
	stats := ExtractConditions(`search index=main | stats count by user | where count > 5 AND bytes > 0`)
	if len(stats.Errors) != 0 || stats.Dataflow == nil || len(stats.Aggregations) != 1 {
		t.Fatalf("stats failed: %#v", stats)
	}
	a := stats.Aggregations[0]
	if a.StageID != stats.Dataflow.Stages[1].ID || a.BindingID == "" || stats.Dataflow.Scopes[2].Mode != "closed" {
		t.Fatalf("stats binding/output missing: %#v", stats.Dataflow)
	}
	var count, bytes *Condition
	for i := range stats.Conditions {
		if stats.Conditions[i].Field == "count" {
			count = &stats.Conditions[i]
		}
		if stats.Conditions[i].Field == "bytes" {
			bytes = &stats.Conditions[i]
		}
	}
	if count == nil || bytes == nil || count.FieldBinding.BindingID != a.BindingID || !count.IsComputed ||
		bytes.FieldBinding.Kind != "absent" || stats.Dataflow.Complete {
		t.Fatalf("stats post-filter ownership: count=%#v bytes=%#v flow=%#v", count, bytes, stats.Dataflow)
	}
	eventstats := ExtractConditions(`search index=main | eventstats count by user | where bytes > 0`)
	if len(eventstats.Errors) != 0 || eventstats.Dataflow == nil || !eventstats.Dataflow.Complete ||
		eventstats.Dataflow.Scopes[2].Mode != "inherit" {
		t.Fatalf("eventstats should inherit input fields: %#v", eventstats)
	}
}

func TestTableAndRenameFieldScopes(t *testing.T) {
	result := ExtractConditions(`search index=main | rename Old AS New | table New | where Old="a" AND New="b"`)
	if len(result.Errors) != 0 || result.Dataflow == nil || len(result.Dataflow.Bindings) != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	flow := result.Dataflow
	if flow.Stages[1].Effect != "rename" || flow.Stages[2].Effect != "project" || flow.Scopes[3].Mode != "closed" ||
		flow.Bindings[0].Kind != "alias" || flow.Bindings[1].Kind != "passthrough" {
		t.Fatalf("rename/table scope missing: %#v", flow)
	}
	var old, new *Condition
	for i := range result.Conditions {
		if result.Conditions[i].Field == "Old" {
			old = &result.Conditions[i]
		}
		if result.Conditions[i].Field == "New" {
			new = &result.Conditions[i]
		}
	}
	if old == nil || new == nil || old.FieldBinding.Kind != "absent" || new.FieldBinding.Kind != "binding" || flow.Complete {
		t.Fatalf("closed projection lookup incorrect: old=%#v new=%#v", old, new)
	}
}

func TestQuotedProjectionAndGroupKeepAuthoredExpressions(t *testing.T) {
	for _, fixture := range []struct {
		query string
		kind  string
	}{
		{`search index=main | table "User Name"`, "passthrough"},
		{`search index=main | stats count by "User Name"`, "group"},
	} {
		result := ExtractConditions(fixture.query)
		if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete {
			t.Fatalf("query did not produce complete graph: %q: %#v", fixture.query, result)
		}
		var selected *FieldBinding
		for i := range result.Dataflow.Bindings {
			if result.Dataflow.Bindings[i].Kind == fixture.kind {
				selected = &result.Dataflow.Bindings[i]
				break
			}
		}
		if selected == nil || selected.Field != "User Name" || selected.Expression != `"User Name"` {
			t.Fatalf("authored selector was lost: %q: %#v", fixture.query, result.Dataflow.Bindings)
		}
	}
}

func TestSemanticDataflowAndTypedReferencesAreIndependent(t *testing.T) {
	query := `search index=main | eval y=x | where y = z`
	parsed := ExtractConditions(query)
	semantic := AnalyzeQuery(query)
	if parsed.Dataflow == nil || semantic.Dataflow == nil || len(parsed.Conditions) < 1 || len(semantic.Conditions) < 1 ||
		semantic.Conditions[len(semantic.Conditions)-1].RHS == nil || semantic.Conditions[len(semantic.Conditions)-1].RHS.FieldBinding == nil {
		t.Fatalf("typed reference missing: parsed=%#v semantic=%#v", parsed, semantic)
	}
	semantic.Dataflow.Scopes[2].BindingIDs[0] = "changed"
	semantic.Conditions[len(semantic.Conditions)-1].RHS.FieldBinding.Kind = "changed"
	if semantic.Expression != nil && semantic.Expression.Condition != nil && semantic.Expression.Condition.RHS != nil &&
		semantic.Expression.Condition.RHS.FieldBinding != nil && semantic.Expression.Condition.RHS.FieldBinding.Kind == "changed" {
		t.Fatal("semantic condition and expression leaf share mutable binding reference")
	}
	if parsed.Dataflow.Scopes[2].BindingIDs[0] == "changed" || parsed.Conditions[len(parsed.Conditions)-1].RHS.FieldBinding.Kind == "changed" {
		t.Fatalf("semantic mutation reached extraction: parsed=%#v", parsed)
	}
}

func TestSemanticConversionClonesExactExtractionAndAllLeafReferences(t *testing.T) {
	parsed := ExtractConditions(`search index=main | eval y=x,z=y | where y=z AND z IN (y,"a")`)
	if len(parsed.Errors) != 0 || parsed.Dataflow == nil || !parsed.Dataflow.Complete {
		t.Fatalf("fixture extraction failed: %#v", parsed)
	}
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	semantic := semanticFromExtraction(parsed, budget)
	if semantic == nil || semantic.Dataflow == nil || semantic.Expression == nil {
		t.Fatalf("conversion failed: %#v", semantic)
	}
	var leaves []*Condition
	var walk func(*SemanticExpression)
	walk = func(node *SemanticExpression) {
		if node == nil {
			return
		}
		if node.Condition != nil {
			leaves = append(leaves, node.Condition)
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(semantic.Expression)
	if len(leaves) != len(semantic.Conditions) || len(leaves) != len(parsed.Conditions) {
		t.Fatalf("semantic expression leaves missing: leaves=%d conditions=%d parsed=%d", len(leaves), len(semantic.Conditions), len(parsed.Conditions))
	}
	fieldIndex, listIndex := -1, -1
	for i := range semantic.Conditions {
		if semantic.Conditions[i].Field == "y" && semantic.Conditions[i].RHS != nil && semantic.Conditions[i].RHS.Kind == OperandField {
			fieldIndex = i
		}
		if semantic.Conditions[i].Field == "z" && semantic.Conditions[i].RHS != nil && len(semantic.Conditions[i].RHS.Items) > 0 {
			listIndex = i
		}
	}
	if fieldIndex < 0 || listIndex < 0 || semantic.Conditions[fieldIndex].FieldBinding == nil ||
		semantic.Conditions[fieldIndex].LHS.FieldBinding == nil || semantic.Conditions[fieldIndex].RHS.FieldBinding == nil ||
		semantic.Conditions[listIndex].RHS.Items[0].FieldBinding == nil {
		t.Fatalf("typed refs missing: %#v", semantic.Conditions)
	}
	semantic.Dataflow.Scopes[2].BindingIDs[0] = "mutated"
	semantic.Conditions[fieldIndex].FieldBinding.Kind = "mutated"
	semantic.Conditions[fieldIndex].LHS.FieldBinding.Kind = "mutated"
	semantic.Conditions[fieldIndex].RHS.FieldBinding.Kind = "mutated"
	semantic.Conditions[listIndex].RHS.Items[0].FieldBinding.Kind = "mutated"
	if parsed.Dataflow.Scopes[2].BindingIDs[0] == "mutated" ||
		parsed.Conditions[fieldIndex].FieldBinding.Kind == "mutated" || parsed.Conditions[fieldIndex].LHS.FieldBinding.Kind == "mutated" ||
		parsed.Conditions[fieldIndex].RHS.FieldBinding.Kind == "mutated" || parsed.Conditions[listIndex].RHS.Items[0].FieldBinding.Kind == "mutated" ||
		leaves[fieldIndex].FieldBinding.Kind == "mutated" || leaves[fieldIndex].LHS.FieldBinding.Kind == "mutated" ||
		leaves[fieldIndex].RHS.FieldBinding.Kind == "mutated" || leaves[listIndex].RHS.Items[0].FieldBinding.Kind == "mutated" {
		t.Fatalf("clone ownership leaked: parsed=%#v semantic=%#v leaves=%#v", parsed.Conditions, semantic.Conditions, leaves)
	}
}

func TestNilAndPresentFieldReferencesRemainDistinctDuringDedup(t *testing.T) {
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	base := Condition{Field: "x", Operator: "=", Value: "a", PipeStage: 1, StageID: "t1", InputScopeID: "s1",
		LHS: &Operand{Kind: OperandField, Value: "x"}}
	with := base
	with.pendingFieldBinding = pendingFieldBinding{valid: true, value: FieldBindingRef{ScopeID: "s1", Kind: "input"}}
	with.LHS = cloneOperand(base.LHS, budget)
	with.LHS.pendingFieldBinding = with.pendingFieldBinding
	if got := deduplicateConditions([]Condition{base, with}, budget); len(got) != 2 ||
		operandIdentity(base.LHS, budget) == operandIdentity(with.LHS, budget) {
		t.Fatalf("nil/present identity merged: %#v", got)
	}
}

func TestRepeatedSameNameEvalSiblingsReadPreviousRevision(t *testing.T) {
	result := ExtractConditions(`search index=main | eval y=x,y=y | where y="a"`)
	if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete || len(result.Dataflow.Bindings) != 2 ||
		result.Dataflow.Bindings[0].Field != "y" || result.Dataflow.Bindings[1].Field != "y" ||
		result.Dataflow.Bindings[0].Expression != "x" || result.Dataflow.Bindings[1].Expression != "y" ||
		result.Dataflow.Bindings[1].InputScopeID == result.Dataflow.Bindings[0].InputScopeID ||
		result.Conditions[len(result.Conditions)-1].FieldBinding.BindingID != result.Dataflow.Bindings[1].ID {
		t.Fatalf("same-name eval sibling history lost: %#v", result)
	}
}

func TestScalarReadScopeAndExpressionCompletenessStaySeparate(t *testing.T) {
	query := `search index=main | eval y=x | where len(y)>2`
	parsed := ExtractConditions(query)
	semantic := AnalyzeQuery(query)
	var parseScalar *ScalarPredicate
	var semanticScalar *ScalarPredicate
	var parseWalk func(*ConditionExpression)
	parseWalk = func(node *ConditionExpression) {
		if node == nil {
			return
		}
		if node.Scalar != nil {
			parseScalar = node.Scalar
		}
		for _, child := range node.Children {
			parseWalk(child)
		}
	}
	var semanticWalk func(*SemanticExpression)
	semanticWalk = func(node *SemanticExpression) {
		if node == nil {
			return
		}
		if node.Scalar != nil {
			semanticScalar = node.Scalar
		}
		for _, child := range node.Children {
			semanticWalk(child)
		}
	}
	parseWalk(parsed.Expression)
	semanticWalk(semantic.Expression)
	if parsed.Dataflow == nil || !parsed.Dataflow.Complete || semantic.Dataflow == nil || !semantic.Dataflow.Complete ||
		parsed.ExpressionIncomplete || semantic.ExpressionIncomplete || parseScalar == nil || semanticScalar == nil {
		t.Fatalf("scalar model missing: parsed=%#v semantic=%#v", parsed, semantic)
	}
	if parseScalar.StageID != parsed.Dataflow.Stages[2].ID ||
		parseScalar.InputScopeID != parsed.Dataflow.Stages[2].Inputs[0].ScopeID ||
		semanticScalar.InputScopeID != parseScalar.InputScopeID {
		t.Fatalf("scalar read environment lost: parsed=%#v semantic=%#v", parseScalar, semanticScalar)
	}
}

func TestTruncatedPredicateCompletenessSurvivesSemanticConversion(t *testing.T) {
	query := "search x=u OR `unknown` OR x=v"
	parsed := ExtractConditions(query)
	semantic := AnalyzeQuery(query)
	if !parsed.ExpressionIncomplete || !semantic.ExpressionIncomplete || parsed.Expression != nil || semantic.Expression != nil ||
		parsed.Dataflow == nil || semantic.Dataflow == nil || parsed.Dataflow.Complete != semantic.Dataflow.Complete {
		t.Fatalf("expression and graph completeness diverged: parsed=%#v semantic=%#v", parsed, semantic)
	}
}

func TestStandaloneSubsearchUsesCompatibilitySlot(t *testing.T) {
	result := ExtractConditions(`search index=main [ search index=other | eval Child=src ] | where x="a"`)
	if len(result.Errors) != 0 || result.Dataflow == nil || len(result.Subsearches) != 1 || result.Subsearches[0].Dataflow == nil {
		t.Fatalf("standalone subsearch failed: %#v", result)
	}
	inputs := result.Dataflow.Stages[0].Inputs
	if len(inputs) != 2 || inputs[1].QueryPath != "subsearches/0" || inputs[1].ScopeID != result.Subsearches[0].Dataflow.OutputScopeID {
		t.Fatalf("standalone child path missing: %#v", inputs)
	}
}

func TestSemanticSubsearchNilSlotAndPrivateGraphIntegrity(t *testing.T) {
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	semantic := semanticFromExtraction(&ParseResult{Subsearches: []*ParseResult{nil, {}}}, budget)
	if len(semantic.Subsearches) != 2 || semantic.Subsearches[0] != nil || semantic.Subsearches[1] == nil {
		t.Fatalf("nil child slot compacted: %#v", semantic.Subsearches)
	}
	bad := &ParseResult{Dataflow: &QueryDataflow{OutputScopeID: "s0", Scopes: []FieldScope{{ID: "s0", Mode: "inherit", ParentID: "s0"}}}}
	if err := finalizeDataflow(bad, budget); err == nil {
		t.Fatal("scope cycle was accepted")
	}
	stageCycle := &ParseResult{Dataflow: &QueryDataflow{OutputScopeID: "s0", Scopes: []FieldScope{{ID: "s0", Mode: "input"}},
		Stages: []DataflowStage{{ID: "t0", Source: "search x=a", OutputScopeID: "s0", Inputs: []ScopeReference{{ScopeID: "s0"}}}}}}
	if err := finalizeDataflow(stageCycle, budget); err == nil {
		t.Fatal("stage input cycle was accepted")
	}
}

func TestParentFirstChildReferenceAndDanglingPayloads(t *testing.T) {
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	child := &ParseResult{Dataflow: &QueryDataflow{Complete: true, OutputScopeID: "s0", Scopes: []FieldScope{{ID: "s0", Mode: "input"}}}}
	root := &ParseResult{Dataflow: &QueryDataflow{Complete: false, OutputScopeID: "s1",
		Scopes: []FieldScope{{ID: "s0", Mode: "input"}, {ID: "s1", Mode: "opaque", ParentID: "s0"}},
		Stages: []DataflowStage{{ID: "t0", Command: "join", Source: "join key [search x=a]", Effect: "opaque",
			Inputs: []ScopeReference{{ScopeID: "s0"}, {QueryPath: "joins/0", ScopeID: "s0"}}, OutputScopeID: "s1"}}},
		Joins: []JoinInfo{{Subsearch: child}}}
	if err := finalizeDataflow(root, budget); err != nil {
		t.Fatalf("parent-first stage to later child rejected: %v", err)
	}
	root.Dataflow.Stages[0].Inputs[1].QueryPath = "joins/1"
	if err := finalizeDataflow(root, budget); err == nil {
		t.Fatal("dangling child path accepted")
	}
	root.Conditions = []Condition{{Field: "x", Value: "a"}}
	failedChild := finalizeSPLDataflowResult(root, budget)
	if failedChild.Dataflow != nil || len(failedChild.Conditions) != 0 || failedChild.Expression != nil || len(failedChild.Errors) != 1 {
		t.Fatalf("dangling child published partial model: %#v", failedChild)
	}
	root.Dataflow.Stages[0].Inputs[1].QueryPath = "joins/0"
	root.Expression = &ConditionExpression{Operator: BooleanScalar,
		Scalar: &ScalarPredicate{StageID: "missing", InputScopeID: "s0", Expression: "len(x)>1", Context: PredicateEval}}
	if err := finalizeDataflow(root, budget); err == nil {
		t.Fatal("dangling scalar stage accepted")
	}
	failedScalar := finalizeSPLDataflowResult(root, budget)
	if failedScalar.Dataflow != nil || len(failedScalar.Conditions) != 0 || failedScalar.Expression != nil || len(failedScalar.Errors) != 1 {
		t.Fatalf("dangling scalar published partial model: %#v", failedScalar)
	}
	root.Expression.Scalar.StageID = "t0"
	root.Expression.Scalar.InputScopeID = "missing"
	if err := finalizeDataflow(root, budget); err == nil {
		t.Fatal("dangling scalar scope accepted")
	}
}

func TestFailedCanonicalChildDoesNotPublishParentGraph(t *testing.T) {
	result := ExtractConditions(`search index=main | join key [ search x= ] | where x="a"`)
	if result.Dataflow != nil || len(result.Joins) != 1 || result.Joins[0].Subsearch == nil ||
		result.Joins[0].Subsearch.Dataflow != nil {
		t.Fatalf("failed child leaked parent or child graph: %#v", result)
	}
}

func TestCanonicalChildInputIsReservedBeforePublication(t *testing.T) {
	for _, fixture := range []struct {
		query string
		kind  string
	}{
		{`join key [ search x=a ]`, "join"},
		{`append [ search x=a ]`, "append"},
	} {
		query := splParserForTest(t, fixture.query).Query()
		if len(query.AllPipelineStage()) != 1 {
			t.Fatalf("child stage did not parse: %q", fixture.query)
		}
		ctx := query.AllPipelineStage()[0].(*PipelineStageContext)
		var childCtx *SubsearchContext
		if fixture.kind == "join" {
			childCtx = ctx.JoinCommand().Subsearch().(*SubsearchContext)
		} else {
			childCtx = ctx.AppendCommand().Subsearch().(*SubsearchContext)
		}
		limits := defaultParseLimits()
		budget, cancel := newParseBudget(context.Background(), limits)
		e := &conditionExtractor{budget: budget, dataflowOwners: []dataflowOwnerFrame{{Selected: true}},
			canonicalChildren: map[antlr.ParserRuleContext]string{childCtx: "joins/0"},
			joins:             []JoinInfo{{Subsearch: &ParseResult{Dataflow: &QueryDataflow{OutputScopeID: "s0"}}}}}
		builder := newSPLDataflowBuilder(e)
		stage := DataflowStage{ID: "t0", Command: fixture.kind, Inputs: []ScopeReference{{ScopeID: builder.current}}}
		before := budget.output
		builder.stageEffect(ctx, &stage, builder.current)
		if budget.output-before != 4 || len(stage.Inputs) != 2 || stage.Inputs[1].QueryPath != "joins/0" {
			t.Fatalf("child input not fully charged: %q delta=%d stage=%#v", fixture.query, budget.output-before, stage)
		}
		cancel()

		limits.maxOutputItems = before + 3 // Opaque scope/diagnostic fit; child input does not.
		tiny, tinyCancel := newParseBudget(context.Background(), limits)
		e.budget = tiny
		limited := newSPLDataflowBuilder(e)
		limitedStage := DataflowStage{ID: "t0", Command: fixture.kind, Inputs: []ScopeReference{{ScopeID: limited.current}}}
		var failure error
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					failure = parseFailure(recovered)
				}
			}()
			limited.stageEffect(ctx, &limitedStage, limited.current)
		}()
		if failure == nil || !strings.Contains(failure.Error(), "output items") || len(limitedStage.Inputs) != 1 {
			t.Fatalf("child input escaped tiny budget: %q failure=%v stage=%#v", fixture.query, failure, limitedStage)
		}
		tinyCancel()
	}
}

func TestPendingBindingIdentityPreventsCrossScopeORGrouping(t *testing.T) {
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	base := Condition{Field: "x", Operator: "=", Value: "a", LogicalOp: "OR", PipeStage: 1, StageID: "t1", InputScopeID: "s1",
		pendingFieldBinding: pendingFieldBinding{valid: true, value: FieldBindingRef{ScopeID: "s1", Kind: "input"}}}
	other := base
	other.Value = "b"
	other.InputScopeID = "s2"
	other.pendingFieldBinding.value.ScopeID = "s2"
	expression := &ConditionExpression{Operator: BooleanOr, Children: []*ConditionExpression{{Operator: BooleanCondition, ConditionIndex: 0}, {Operator: BooleanCondition, ConditionIndex: 1}}}
	grouped, _ := groupORConditionsByExpression([]Condition{base, other}, expression, budget)
	if len(grouped) != 2 {
		t.Fatalf("different pending scopes merged: %#v", grouped)
	}
	other.InputScopeID = base.InputScopeID
	other.pendingFieldBinding = base.pendingFieldBinding
	grouped, _ = groupORConditionsByExpression([]Condition{base, other}, expression, budget)
	if len(grouped) != 1 || len(grouped[0].Alternatives) != 2 {
		t.Fatalf("same-scope OR stopped grouping: %#v", grouped)
	}
}

func TestPublicResultsClearPendingReferences(t *testing.T) {
	result := ExtractConditions(`search index=main | eval y=x | where y IN ("a","b")`)
	if len(result.Errors) != 0 || len(result.Conditions) == 0 {
		t.Fatalf("parse failed: %#v", result)
	}
	var checkOperand func(*Operand)
	checkOperand = func(operand *Operand) {
		if operand == nil {
			return
		}
		if operand.pendingFieldBinding.valid {
			t.Fatalf("pending operand reference published: %#v", operand)
		}
		for i := range operand.Items {
			checkOperand(&operand.Items[i])
		}
	}
	for _, condition := range result.Conditions {
		if condition.pendingFieldBinding.valid {
			t.Fatalf("pending condition reference published: %#v", condition)
		}
		checkOperand(condition.LHS)
		checkOperand(condition.RHS)
	}
}

func TestFieldsSelectorsRespectKnownAndOpaqueOutputs(t *testing.T) {
	negative := ExtractConditions(`search index=main | fields - secret | where secret="x"`)
	if len(negative.Errors) != 0 || negative.Dataflow == nil || negative.Dataflow.Stages[1].Effect != "project" ||
		negative.Dataflow.Scopes[2].Mode != "inherit" || negative.Conditions[len(negative.Conditions)-1].FieldBinding.Kind != "absent" {
		t.Fatalf("negative fields removal failed: %#v", negative)
	}
	positive := ExtractConditions(`search index=main | fields user | where _raw="x"`)
	if len(positive.Errors) != 0 || positive.Dataflow == nil || positive.Dataflow.Complete ||
		positive.Dataflow.Stages[1].Effect != "opaque" || positive.Conditions[len(positive.Conditions)-1].FieldBinding.Kind != "unknown" {
		t.Fatalf("positive fields claimed omitted internal field absent: %#v", positive)
	}
	for _, query := range []string{
		`search index=main | fields - "foo*" | where fooA="a"`,
		`search index=main | table "foo*" | where fooA="a"`,
	} {
		result := ExtractConditions(query)
		if len(result.Errors) != 0 || result.Dataflow == nil || result.Dataflow.Complete ||
			result.Dataflow.Stages[1].Effect != "opaque" ||
			result.Conditions[len(result.Conditions)-1].FieldBinding.Kind != "unknown" ||
			!strings.Contains(result.Dataflow.Stages[1].Source, `"foo*"`) {
			t.Fatalf("pattern selector claimed exact schema: %q: %#v", query, result)
		}
	}
}

func TestDynamicEvalTargetDoesNotPublishExactBinding(t *testing.T) {
	result := ExtractConditions(`search index=main | eval <<FIELD>>=src | where x="a"`)
	if len(result.Errors) != 0 || result.Dataflow == nil || result.Dataflow.Complete ||
		result.Dataflow.Stages[1].Effect != "opaque" || len(result.Dataflow.Stages[1].BindingIDs) != 0 ||
		result.Conditions[len(result.Conditions)-1].FieldBinding.Kind != "unknown" ||
		!strings.Contains(result.Dataflow.Stages[1].Source, "<<FIELD>>") {
		t.Fatalf("dynamic eval target claimed exact binding: %#v", result)
	}
}

func TestQuotedDollarEvalTargetRetainsExactBinding(t *testing.T) {
	query := `search index=main | eval "price$"=x | where 'price$'>1`
	parsed, semantic := ExtractConditions(query), AnalyzeQuery(query)
	for _, result := range []struct {
		flow       *QueryDataflow
		conditions []Condition
		computed   map[string]string
		errors     []string
	}{
		{parsed.Dataflow, parsed.Conditions, parsed.ComputedFields, parsed.Errors},
		{semantic.Dataflow, semantic.Conditions, semantic.ComputedFields, semantic.Errors},
	} {
		if len(result.errors) != 0 || result.flow == nil || !result.flow.Complete ||
			len(result.flow.Bindings) != 1 || result.flow.Bindings[0].Field != "price$" ||
			result.flow.Stages[1].Effect != "assign" || result.computed["price$"] != "x" {
			t.Fatalf("literal quoted dollar field lost exact assignment: %#v", result)
		}
		condition := result.conditions[len(result.conditions)-1]
		if condition.Field != "price$" || condition.FieldBinding == nil || condition.FieldBinding.Kind != "binding" ||
			condition.FieldBinding.BindingID != result.flow.Bindings[0].ID || !condition.IsComputed || condition.SourceField != "x" {
			t.Fatalf("literal quoted dollar field did not resolve: %#v", condition)
		}
	}
}

func TestPatternAggregateAliasDoesNotPublishPartialBindings(t *testing.T) {
	result := ExtractConditions(`search index=main | stats count AS n, sum(bytes) AS "foo*" by user | where n>1`)
	if len(result.Errors) != 0 || result.Dataflow == nil || result.Dataflow.Complete ||
		result.Dataflow.Stages[1].Effect != "opaque" || len(result.Dataflow.Stages[1].BindingIDs) != 0 ||
		len(result.Dataflow.Bindings) != 0 || result.Conditions[len(result.Conditions)-1].FieldBinding.Kind != "unknown" {
		t.Fatalf("pattern aggregate alias published partial schema: %#v", result)
	}
}

func TestEscapedAggregateAliasUsesDecodedFieldIdentity(t *testing.T) {
	query := `search index=main | stats count AS "A\"B" | where 'A\"B'>1`
	result := ExtractConditions(query)
	semantic := AnalyzeQuery(query)
	if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete ||
		len(result.Aggregations) != 1 || result.Aggregations[0].Column != `A"B` ||
		len(result.Dataflow.Bindings) != 1 || result.Dataflow.Bindings[0].Field != `A"B` ||
		result.ComputedFields[`A"B`] != "count" || semantic.ComputedFields[`A"B`] != "count" ||
		result.Conditions[len(result.Conditions)-1].FieldBinding.BindingID != result.Aggregations[0].BindingID ||
		!result.Conditions[len(result.Conditions)-1].IsComputed ||
		semantic.Dataflow == nil || !semantic.Dataflow.Complete || semantic.Aggregations[0].Column != `A"B` {
		t.Fatalf("escaped aggregate alias did not resolve: parsed=%#v semantic=%#v", result, semantic)
	}
}

func TestComputedProvenanceSurvivesProjectionAndFunctionPredicate(t *testing.T) {
	for _, fixture := range []struct {
		query       string
		field       string
		computed    bool
		sourceField string
	}{
		{`search index=main | stats count | table count | where count>5`, "count", true, ""},
		{`search index=main | eval y=x | table y | where isnotnull(y)`, "y", true, "x"},
		{`search index=main | eval y=coalesce("path\\",alpha) | where isnotnull(y)`, "y", true, "alpha"},
		{`search index=main | table y | where isnotnull(y)`, "y", false, ""},
	} {
		parsed := ExtractConditions(fixture.query)
		semantic := AnalyzeQuery(fixture.query)
		if parsed.Dataflow == nil || !parsed.Dataflow.Complete || semantic.Dataflow == nil || !semantic.Dataflow.Complete {
			t.Fatalf("fixture graph incomplete: %q: %#v", fixture.query, parsed)
		}
		for _, conditions := range [][]Condition{parsed.Conditions, semantic.Conditions} {
			found := false
			for _, condition := range conditions {
				if condition.Field == fixture.field {
					found = true
					if condition.IsComputed != fixture.computed || condition.SourceField != fixture.sourceField {
						t.Fatalf("legacy provenance lost: %q: %#v", fixture.query, condition)
					}
				}
			}
			if !found {
				t.Fatalf("missing field condition for %q", fixture.query)
			}
		}
	}
}

func TestAuthoredEvalCommentDoesNotBecomeSourceField(t *testing.T) {
	query := "search index=main | eval y=coalesce(``` commentword ``` x,z) | where y=\"a\""
	parsed, semantic := ExtractConditions(query), AnalyzeQuery(query)
	for _, result := range []struct {
		flow       *QueryDataflow
		conditions []Condition
		computed   map[string]string
		errors     []string
	}{
		{parsed.Dataflow, parsed.Conditions, parsed.ComputedFields, parsed.Errors},
		{semantic.Dataflow, semantic.Conditions, semantic.ComputedFields, semantic.Errors},
	} {
		if len(result.errors) != 0 || result.flow == nil || !result.flow.Complete ||
			len(result.flow.Bindings) != 1 || result.flow.Bindings[0].Expression != "coalesce(``` commentword ``` x,z)" ||
			result.computed["y"] != "x" {
			t.Fatalf("authored expression or normalized compatibility source changed: %#v", result)
		}
		condition := result.conditions[len(result.conditions)-1]
		if condition.Field != "y" || !condition.IsComputed || condition.SourceField != "x" ||
			condition.FieldBinding == nil || condition.FieldBinding.BindingID != result.flow.Bindings[0].ID {
			t.Fatalf("comment word displaced source field: %#v", condition)
		}
	}
}

func TestEvalSourceHintChargesCharacterAndCandidateWork(t *testing.T) {
	const expression = "coalesce(alpha,z)"
	budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
	defer cancel()
	if hint := firstSPLFieldHint(expression, budget); hint != "alpha" || budget.work <= len(expression) {
		t.Fatalf("unbounded or incorrect syntax hint: hint=%q work=%d", hint, budget.work)
	}
	limits := defaultParseLimits()
	limits.maxWork = len(expression)
	limited, stop := newParseBudget(context.Background(), limits)
	defer stop()
	func() {
		defer func() {
			if got := recover(); got == nil {
				t.Fatal("source-hint scan exceeded shared work without rejecting")
			} else if failure := parseFailure(got); !strings.Contains(failure.Error(), "work budget") {
				t.Fatalf("wrong source-hint limit failure: %v", failure)
			}
		}()
		firstSPLFieldHint(expression, limited)
	}()
}

func TestStatsRebindingKeepsBothAggregateOwners(t *testing.T) {
	query := `search index=main | stats sum(bytes) AS n | where n>5 | stats max(n) AS n | where n>5`
	result := ExtractConditions(query)
	semantic := AnalyzeQuery(query)
	if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete || len(result.Aggregations) != 2 ||
		len(semantic.Aggregations) != 2 || result.Aggregations[0].BindingID == result.Aggregations[1].BindingID {
		t.Fatalf("aggregate history collapsed: %#v", result)
	}
	var filters []Condition
	for _, condition := range result.Conditions {
		if condition.Field == "n" {
			filters = append(filters, condition)
		}
	}
	if len(filters) != 2 || filters[0].FieldBinding.BindingID != result.Aggregations[0].BindingID ||
		filters[1].FieldBinding.BindingID != result.Aggregations[1].BindingID {
		t.Fatalf("stats filters read wrong output: %#v", filters)
	}
}

func TestStatsAggregateAndByBindingOrdinalsFollowAuthoredOrder(t *testing.T) {
	query := `search index=main | stats sum(bytes) AS total, count AS n by user, host | where total>5`
	parsed, semantic := ExtractConditions(query), AnalyzeQuery(query)
	for _, view := range []struct {
		flow   *QueryDataflow
		aggs   []Aggregation
		errors []string
	}{
		{parsed.Dataflow, parsed.Aggregations, parsed.Errors},
		{semantic.Dataflow, semantic.Aggregations, semantic.Errors},
	} {
		if len(view.errors) != 0 || view.flow == nil || !view.flow.Complete || len(view.flow.Bindings) != 4 || len(view.aggs) != 2 {
			t.Fatalf("mixed stats outputs missing: %#v", view)
		}
		want := map[string]struct {
			kind    string
			ordinal int
		}{"total": {"aggregate", 0}, "n": {"aggregate", 1}, "user": {"group", 2}, "host": {"group", 3}}
		seen := make(map[string]FieldBinding)
		for _, binding := range view.flow.Bindings {
			expected, ok := want[binding.Field]
			if !ok || binding.Kind != expected.kind || binding.Ordinal != expected.ordinal ||
				binding.InputScopeID != view.flow.Stages[1].Inputs[0].ScopeID {
				t.Fatalf("stats binding has wrong lexical ordinal or input: %#v", binding)
			}
			seen[binding.Field] = binding
		}
		if len(seen) != len(want) || view.aggs[0].BindingID != seen["total"].ID || view.aggs[1].BindingID != seen["n"].ID {
			t.Fatalf("stats output/aggregation pairing changed: %#v", view)
		}
	}
}

func TestArgumentlessAggregateReplacesEarlierAssignmentHint(t *testing.T) {
	query := `search index=main | eval count=x | stats count | table count | where count>1`
	parsed, semantic := ExtractConditions(query), AnalyzeQuery(query)
	for _, view := range []struct {
		flow       *QueryDataflow
		conditions []Condition
		errors     []string
	}{
		{parsed.Dataflow, parsed.Conditions, parsed.Errors},
		{semantic.Dataflow, semantic.Conditions, semantic.Errors},
	} {
		if len(view.errors) != 0 || view.flow == nil || !view.flow.Complete || len(view.flow.Bindings) != 3 {
			t.Fatalf("aggregate boundary missing: %#v", view)
		}
		condition := view.conditions[len(view.conditions)-1]
		if condition.Field != "count" || !condition.IsComputed || condition.SourceField != "" ||
			condition.FieldBinding == nil || condition.FieldBinding.BindingID != view.flow.Bindings[2].ID ||
			view.flow.Bindings[1].Kind != "aggregate" || view.flow.Bindings[1].Expression != "count" {
			t.Fatalf("obsolete assignment hint survived argumentless aggregate: %#v", view)
		}
	}
}

func TestSpacedArgumentlessAggregateReplacesEarlierAssignmentHint(t *testing.T) {
	for _, function := range []string{"count ( )", "count()"} {
		t.Run(function, func(t *testing.T) {
			query := `search index=main | eval 'count()'=x | stats ` + function + ` | table "count()" | where 'count()'>1`
			parsed, semantic := ExtractConditions(query), AnalyzeQuery(query)
			for _, view := range []struct {
				flow       *QueryDataflow
				aggs       []Aggregation
				conditions []Condition
				errors     []string
			}{
				{parsed.Dataflow, parsed.Aggregations, parsed.Conditions, parsed.Errors},
				{semantic.Dataflow, semantic.Aggregations, semantic.Conditions, semantic.Errors},
			} {
				if len(view.errors) != 0 || view.flow == nil || !view.flow.Complete ||
					len(view.flow.Bindings) != 3 || len(view.aggs) != 1 ||
					view.flow.Bindings[1].Expression != function || view.flow.Bindings[1].Field != "count()" ||
					view.aggs[0].BindingID != view.flow.Bindings[1].ID {
					t.Fatalf("argumentless aggregate graph/source changed: %#v", view)
				}
				condition := view.conditions[len(view.conditions)-1]
				if condition.Field != "count()" || !condition.IsComputed || condition.SourceField != "" ||
					condition.FieldBinding == nil || condition.FieldBinding.BindingID != view.flow.Bindings[2].ID {
					t.Fatalf("argumentless aggregate retained obsolete source: %#v", view)
				}
			}
		})
	}
}

func TestEscapedRenameCompatibilityUsesDecodedFieldIdentity(t *testing.T) {
	query := `search index=main | rename Old AS "A\"B" | where 'A\"B'="a"`
	parsed, semantic := ExtractConditions(query), AnalyzeQuery(query)
	for _, view := range []struct {
		flow       *QueryDataflow
		conditions []Condition
		aliases    map[string]string
		computed   map[string]string
		errors     []string
	}{
		{parsed.Dataflow, parsed.Conditions, parsed.FieldAliases, parsed.ComputedFields, parsed.Errors},
		{semantic.Dataflow, semantic.Conditions, semantic.FieldAliases, semantic.ComputedFields, semantic.Errors},
	} {
		if len(view.errors) != 0 || view.flow == nil || !view.flow.Complete || len(view.flow.Bindings) != 1 ||
			view.flow.Bindings[0].Field != `A"B` || view.flow.Bindings[0].Kind != "alias" ||
			view.aliases[`A"B`] != "Old" || view.computed[`A"B`] != "Old" ||
			len(view.aliases) != 1 || len(view.computed) != 1 || !strings.Contains(view.flow.Stages[1].Source, `"A\"B"`) {
			t.Fatalf("escaped rename identity or source mismatched: %#v", view)
		}
		condition := view.conditions[len(view.conditions)-1]
		if condition.Field != `A"B` || condition.FieldBinding == nil ||
			condition.FieldBinding.BindingID != view.flow.Bindings[0].ID || !condition.IsComputed || condition.SourceField != "Old" {
			t.Fatalf("escaped rename condition did not resolve: %#v", condition)
		}
	}
}

func TestTypedFieldListItemsUseConditionReadScope(t *testing.T) {
	result := ExtractConditions(`search index=main | eval x=src, y=x | where x IN (y, "a")`)
	if len(result.Errors) != 0 || result.Dataflow == nil || !result.Dataflow.Complete {
		t.Fatalf("typed list parse failed: %#v", result)
	}
	var condition *Condition
	for i := range result.Conditions {
		if result.Conditions[i].Field == "x" && result.Conditions[i].Operator == "in" {
			condition = &result.Conditions[i]
		}
	}
	if condition == nil || condition.RHS == nil || len(condition.RHS.Items) != 2 || condition.RHS.Items[0].Kind != OperandField ||
		condition.RHS.Items[0].FieldBinding == nil || condition.RHS.Items[0].FieldBinding.ScopeID != condition.InputScopeID ||
		condition.RHS.Items[0].FieldBinding.BindingID != result.Dataflow.Bindings[1].ID {
		t.Fatalf("typed IN field item lost scope: %#v", condition)
	}
}

func TestDataflowJSONRoundTripPreservesIncompleteGraph(t *testing.T) {
	result := ExtractConditions(`search index=main | fields user | where _raw="x"`)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var restored ParseResult
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Dataflow == nil || restored.Dataflow.Complete || len(restored.Dataflow.Diagnostics) == 0 ||
		len(restored.Dataflow.Stages) != 3 || restored.Dataflow.Stages[1].PipeStage != 1 ||
		restored.Dataflow.OutputScopeID != result.Dataflow.OutputScopeID {
		t.Fatalf("incomplete graph did not round-trip: %#v", restored.Dataflow)
	}
}

func TestPublicOrderedEvalAndAggregationMetadata(t *testing.T) {
	lower := `search index=main | eval y = lower(x) | where y = "a" | eval y = lower(x) | where y = "a"`
	upper := `search index=main | eval y = upper(x) | where y = "a" | eval y = lower(x) | where y = "a"`
	for _, query := range []string{lower, upper} {
		for _, value := range []any{ExtractConditions(query), AnalyzeQuery(query)} {
			object := publicObject(t, value)
			flow, ok := object["dataflow"].(map[string]any)
			if !ok || flow["complete"] != true {
				t.Fatalf("missing complete public dataflow for %q: %#v", query, flow)
			}
			bindings, ok := flow["bindings"].([]any)
			if !ok || len(bindings) != 2 {
				t.Fatalf("expected both eval writes for %q: %#v", query, flow["bindings"])
			}
		}
	}
	semantic := publicObject(t, AnalyzeQuery(`search index=main | stats sum(bytes) AS n | where n > 5`))
	if aggregates, ok := semantic["aggregations"].([]any); !ok || len(aggregates) != 1 {
		t.Fatalf("semantic aggregation omitted: %#v", semantic["aggregations"])
	}
}
