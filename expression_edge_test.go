package spl

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

func splSearchExpressionForTest(t *testing.T, expression string) ISearchExpressionContext {
	t.Helper()

	return splParserForTest(t, expression).SearchExpression()
}

func splParserForTest(t *testing.T, query string) *SPLParser {
	t.Helper()

	input := antlr.NewInputStream(query)
	lexer := NewSPLLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	return NewSPLParser(stream)
}

func splWhereExpressionForTest(t *testing.T, expression string) IExpressionContext {
	t.Helper()

	return splParserForTest(t, expression).Expression()
}

func TestSPLExpressionBuilderHelperEdges(t *testing.T) {
	conditions := []Condition{
		{Field: "a", Operator: "=", Value: "1"},
		{Field: "b", Operator: "=", Value: "2"},
	}
	builder := &splExpressionBuilder{conditions: conditions}

	if got := buildConditionExpression(nil, conditions); got != nil {
		t.Fatalf("nil tree returned expression %#v", got)
	}
	if got := buildConditionExpression(splSearchExpressionForTest(t, `a=1 b=2`), conditions[:1]); got != nil {
		t.Fatalf("single condition returned expression %#v", got)
	}
	if got := buildConditionExpression(splSearchExpressionForTest(t, `a=1 OR b=2`), conditions); got == nil || got.Operator != BooleanOr {
		t.Fatalf("search-expression fallback did not preserve OR grouping, got %#v", got)
	}
	if got := builder.tree(nil); got != nil {
		t.Fatalf("nil tree node returned expression %#v", got)
	}
	if got := builder.tree(splParserForTest(t, `mstats count WHERE a=1`).MstatsCommand()); got == nil {
		t.Fatalf("mstats command with WHERE expression returned nil")
	}
	if got := builder.tree(splParserForTest(t, `inputlookup users WHERE a=1`).InputlookupCommand()); got == nil {
		t.Fatalf("inputlookup command with WHERE expression returned nil")
	}
	if got := collectSPLSearchExpressions(nil); got != nil {
		t.Fatalf("nil tree search-expression collection returned %#v", got)
	}
	if got := builder.searchTerm(nil); got != nil {
		t.Fatalf("nil search term returned expression %#v", got)
	}
	if got := builder.whereExpression(nil); got != nil {
		t.Fatalf("nil where expression returned %#v", got)
	}
	if got := builder.orExpression(nil); got != nil {
		t.Fatalf("nil OR expression returned %#v", got)
	}
	emptyOr := NewEmptyOrExpressionContext()
	if got := builder.orExpression(emptyOr); got != nil {
		t.Fatalf("empty OR expression returned %#v", got)
	}
	invalidOr := NewEmptyOrExpressionContext()
	invalidOr.AddChild(NewEmptyAndExpressionContext())
	invalidOr.AddChild(NewEmptyAndExpressionContext())
	if got := builder.orExpression(invalidOr); got != nil {
		t.Fatalf("invalid multi-child OR expression returned %#v", got)
	}
	if got := builder.andExpression(nil); got != nil {
		t.Fatalf("nil AND expression returned %#v", got)
	}
	if got := builder.notExpression(nil); got != nil {
		t.Fatalf("nil NOT expression returned %#v", got)
	}
	if got := builder.comparisonExpression(nil); got != nil {
		t.Fatalf("nil comparison expression returned %#v", got)
	}
	if got := nestedWhereExpression(nil); got != nil {
		t.Fatalf("nil nested expression returned %#v", got)
	}
	if comparisonExpressionExtractsCondition(nil) {
		t.Fatalf("nil comparison extracted a condition")
	}
	if containsExtractedFunctionCondition(nil) {
		t.Fatalf("nil tree contained extracted function condition")
	}
	if containsExtractedFunctionCondition(NewEmptySubsearchContext()) {
		t.Fatalf("subsearch should not be treated as an extracted function condition")
	}
	if !containsExtractedFunctionCondition(splWhereExpressionForTest(t, `cidrmatch("10.0.0.0/8", src_ip)`)) {
		t.Fatalf("nested cidrmatch function was not detected")
	}

	exhausted := &splExpressionBuilder{conditions: conditions, next: len(conditions)}
	if got := exhausted.condition(); got != nil {
		t.Fatalf("exhausted condition builder returned %#v", got)
	}
}

func TestSPLExpressionBuilderDirectParsingEdges(t *testing.T) {
	searchBuilder := &splExpressionBuilder{
		conditions: []Condition{
			{Field: "a", Operator: "=", Value: "1"},
			{Field: "b", Operator: "=", Value: "2"},
		},
	}
	searchExpr := searchBuilder.searchExpression(splSearchExpressionForTest(t, `a=1 OR b=2`))
	if searchExpr == nil || searchExpr.Operator != BooleanOr || len(searchExpr.Children) != 2 {
		t.Fatalf("search expression did not preserve OR grouping: %#v", searchExpr)
	}

	whereBuilder := &splExpressionBuilder{
		conditions: []Condition{
			{Field: "a", Operator: "=", Value: "1"},
			{Field: "b", Operator: "=", Value: "2"},
			{Field: "c", Operator: "=", Value: "3"},
		},
	}
	whereExpr := whereBuilder.whereExpression(splWhereExpressionForTest(t, `(a=1 OR b=2) AND NOT c=3`))
	if whereExpr == nil || whereExpr.Operator != BooleanAnd || len(whereExpr.Children) != 2 {
		t.Fatalf("where expression did not preserve AND/OR/NOT grouping: %#v", whereExpr)
	}
	if whereExpr.Children[1].Operator != BooleanCondition || whereExpr.Children[1].ConditionIndex != 2 {
		t.Fatalf("negated leaf should remain the third extracted condition, got %#v", whereExpr.Children[1])
	}
}

func TestSPLExpressionRemapAndEqualityEdges(t *testing.T) {
	leaf0 := &ConditionExpression{Operator: BooleanCondition, ConditionIndex: 0}
	leaf1 := &ConditionExpression{Operator: BooleanCondition, ConditionIndex: 1}
	duplicate := &ConditionExpression{Operator: BooleanCondition, ConditionIndex: 1}
	nested := &ConditionExpression{
		Operator: BooleanAnd,
		Children: []*ConditionExpression{
			leaf0,
			{
				Operator: BooleanAnd,
				Children: []*ConditionExpression{leaf1, duplicate},
			},
			{Operator: BooleanCondition, ConditionIndex: 2},
			{Operator: BooleanCondition, ConditionIndex: 3},
		},
	}

	if got := remapConditionExpression(nil, []int{0}); got != nil {
		t.Fatalf("nil expression remapped to %#v", got)
	}
	if got := remapConditionExpression(leaf0, nil); got != leaf0 {
		t.Fatalf("empty index map should return original expression, got %#v", got)
	}
	if got := remapConditionExpression(&ConditionExpression{Operator: BooleanCondition, ConditionIndex: -1}, []int{0}); got != nil {
		t.Fatalf("negative condition index remapped to %#v", got)
	}
	if got := remapConditionExpression(&ConditionExpression{Operator: BooleanCondition, ConditionIndex: 2}, []int{0}); got != nil {
		t.Fatalf("out-of-range condition index remapped to %#v", got)
	}
	if got := remapConditionExpression(&ConditionExpression{Operator: "xor"}, []int{0}); got != nil {
		t.Fatalf("unknown expression operator remapped to %#v", got)
	}

	remapped := remapConditionExpression(nested, []int{1, 0, -1, -1})
	if remapped == nil || remapped.Operator != BooleanAnd || len(remapped.Children) != 2 {
		t.Fatalf("remap should flatten duplicate children and prune dropped indexes, got %#v", remapped)
	}
	if remapped.Children[0].ConditionIndex != 1 || remapped.Children[1].ConditionIndex != 0 {
		t.Fatalf("remap did not apply expected indexes, got %#v", remapped)
	}
	flattened := remapConditionExpression(&ConditionExpression{
		Operator: BooleanOr,
		Children: []*ConditionExpression{
			{Operator: BooleanCondition, ConditionIndex: 0},
			{
				Operator: BooleanOr,
				Children: []*ConditionExpression{
					{Operator: BooleanCondition, ConditionIndex: 1},
					{Operator: BooleanCondition, ConditionIndex: 2},
				},
			},
		},
	}, []int{0, 1, 2})
	if flattened == nil || flattened.Operator != BooleanOr || len(flattened.Children) != 3 {
		t.Fatalf("same-operator remap should flatten grandchildren, got %#v", flattened)
	}
	if got := remapConditionExpression(&ConditionExpression{
		Operator: BooleanAnd,
		Children: []*ConditionExpression{
			{Operator: BooleanCondition, ConditionIndex: 0},
			{Operator: BooleanCondition, ConditionIndex: 1},
		},
	}, []int{-1, -1}); got != nil {
		t.Fatalf("fully pruned expression remapped to %#v", got)
	}

	collapsed := remapConditionExpression(&ConditionExpression{
		Operator: BooleanOr,
		Children: []*ConditionExpression{
			{Operator: BooleanCondition, ConditionIndex: 0},
			{Operator: BooleanCondition, ConditionIndex: 1},
		},
	}, []int{-1, 0})
	if collapsed == nil || collapsed.Operator != BooleanCondition || collapsed.ConditionIndex != 0 {
		t.Fatalf("single remaining child did not collapse, got %#v", collapsed)
	}

	if !equalConditionExpression(nil, nil) {
		t.Fatalf("nil expressions should compare equal")
	}
	if equalConditionExpression(leaf0, nil) {
		t.Fatalf("non-nil expression compared equal to nil")
	}
	if equalConditionExpression(leaf0, leaf1) {
		t.Fatalf("different condition indexes compared equal")
	}
	if equalConditionExpression(
		&ConditionExpression{Operator: BooleanAnd, Children: []*ConditionExpression{leaf0}},
		&ConditionExpression{Operator: BooleanAnd, Children: []*ConditionExpression{leaf1}},
	) {
		t.Fatalf("different children compared equal")
	}
}

func TestSPLExpressionBooleanCombinationEdges(t *testing.T) {
	leaf0 := &ConditionExpression{Operator: BooleanCondition, ConditionIndex: 0}
	leaf1 := &ConditionExpression{Operator: BooleanCondition, ConditionIndex: 1}
	leaf2 := &ConditionExpression{Operator: BooleanCondition, ConditionIndex: 2}

	if got := combineConditionExpressions(BooleanAnd, nil, leaf0); got != leaf0 {
		t.Fatalf("nil left should return right, got %#v", got)
	}
	if got := combineConditionExpressions(BooleanAnd, leaf0, nil); got != leaf0 {
		t.Fatalf("nil right should return left, got %#v", got)
	}

	combined := combineConditionExpressions(BooleanAnd,
		&ConditionExpression{Operator: BooleanAnd, Children: []*ConditionExpression{leaf0, leaf1}},
		leaf2,
	)
	if combined == nil || combined.Operator != BooleanAnd || len(combined.Children) != 3 {
		t.Fatalf("left AND children were not flattened, got %#v", combined)
	}

	combined = combineConditionExpressions(BooleanOr,
		leaf0,
		&ConditionExpression{Operator: BooleanOr, Children: []*ConditionExpression{leaf1, leaf2}},
	)
	if combined == nil || combined.Operator != BooleanOr || len(combined.Children) != 3 {
		t.Fatalf("right OR children were not flattened, got %#v", combined)
	}

	if got := demorganConditionExpression(nil); got != nil {
		t.Fatalf("nil De Morgan expression returned %#v", got)
	}
	notLeaf := demorganConditionExpression(leaf0)
	if notLeaf != leaf0 {
		t.Fatalf("leaf De Morgan expression should be returned directly, got %#v", notLeaf)
	}
	orExpr := demorganConditionExpression(&ConditionExpression{
		Operator: BooleanAnd,
		Children: []*ConditionExpression{leaf0, leaf1},
	})
	if orExpr == nil || orExpr.Operator != BooleanOr || len(orExpr.Children) != 2 {
		t.Fatalf("AND should invert to OR, got %#v", orExpr)
	}
	andExpr := demorganConditionExpression(&ConditionExpression{
		Operator: BooleanOr,
		Children: []*ConditionExpression{leaf0, leaf1},
	})
	if andExpr == nil || andExpr.Operator != BooleanAnd || len(andExpr.Children) != 2 {
		t.Fatalf("OR should invert to AND, got %#v", andExpr)
	}
}
