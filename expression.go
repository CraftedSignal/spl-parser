package spl

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

// BooleanOperator represents parsed boolean expression operators.
type BooleanOperator string

const (
	BooleanCondition BooleanOperator = "condition"
	BooleanAnd       BooleanOperator = "and"
	BooleanOr        BooleanOperator = "or"
)

// ConditionExpression preserves boolean grouping over extracted Conditions.
// ConditionIndex references ParseResult.Conditions.
type ConditionExpression struct {
	Operator       BooleanOperator        `json:"operator"`
	ConditionIndex int                    `json:"condition_index"`
	Children       []*ConditionExpression `json:"children,omitempty"`
}

func buildConditionExpression(tree antlr.Tree, conditions []Condition, budget *parseBudget) *ConditionExpression {
	expression, _ := buildConditionExpressionWithCompleteness(tree, conditions, budget, nil)
	return expression
}

// A nil expression can be intentional for a single filter or a command with
// no event predicate. incomplete distinguishes it from a dropped Boolean atom.
func buildConditionExpressionWithCompleteness(tree antlr.Tree, conditions []Condition, budget *parseBudget, sources conditionSourceBindings) (*ConditionExpression, bool) {
	if tree == nil {
		return nil, false
	}
	if expression, incomplete := buildPipelineConditionExpressionWithCompleteness(tree, conditions, budget, sources); incomplete {
		return nil, true
	} else if expression != nil && len(conditions) >= 2 {
		return expression, false
	}
	if sources != nil {
		return nil, false
	}
	if len(conditions) < 2 {
		return nil, false
	}
	for _, candidate := range collectSPLSearchExpressions(tree, budget) {
		builder := &splExpressionBuilder{conditions: conditions, budget: budget}
		expression := builder.searchExpression(candidate)
		if builder.incomplete {
			return nil, true
		}
		if expression != nil && builder.next == len(conditions) {
			return expression, false
		}
	}
	return nil, false
}

func buildPipelineConditionExpressionWithCompleteness(tree antlr.Tree, conditions []Condition, budget *parseBudget, sources conditionSourceBindings) (*ConditionExpression, bool) {
	builder := &splExpressionBuilder{conditions: conditions, budget: budget, sources: sources}
	if sources != nil {
		budget.step()
		builder.used = make([]bool, len(conditions))
	}
	expression := builder.tree(tree)
	if builder.incomplete {
		return nil, true
	}
	if sources != nil {
		for _, used := range builder.used {
			budget.step()
			if !used {
				return nil, true
			}
		}
	}
	if expression != nil && builder.next == len(conditions) {
		return expression, false
	}
	return nil, false
}

type splExpressionBuilder struct {
	budget     *parseBudget
	conditions []Condition
	sources    conditionSourceBindings
	used       []bool
	next       int
	incomplete bool
}

type conditionSourceBinding struct {
	first   int
	count   int
	invalid bool
}

type conditionSourceBindings map[antlr.Tree]conditionSourceBinding

func (b *splExpressionBuilder) tree(node antlr.Tree) *ConditionExpression {
	b.budget.step()
	if node == nil {
		return nil
	}

	switch ctx := node.(type) {
	case ISubsearchContext:
		return nil
	case ISearchCommandContext:
		return b.searchExpression(ctx.SearchExpression())
	case IWhereCommandContext:
		return b.whereExpression(ctx.Expression())
	case ITstatsCommandContext:
		return b.searchExpression(ctx.SearchExpression())
	case IMstatsCommandContext:
		return b.searchExpression(ctx.SearchExpression())
	case IInputlookupCommandContext:
		return b.whereExpression(ctx.Expression())
	}

	var expression *ConditionExpression
	for i := 0; i < node.GetChildCount(); i++ {
		expression = combineConditionExpressions(BooleanAnd, expression, b.tree(node.GetChild(i)))
	}
	return expression
}

func collectSPLSearchExpressions(tree antlr.Tree, budget *parseBudget) []ISearchExpressionContext {
	var expressions []ISearchExpressionContext
	var visit func(antlr.Tree)
	visit = func(node antlr.Tree) {
		budget.step()
		if node == nil {
			return
		}
		if expression, ok := node.(ISearchExpressionContext); ok {
			expressions = append(expressions, expression)
		}
		for i := 0; i < node.GetChildCount(); i++ {
			visit(node.GetChild(i))
		}
	}
	visit(tree)
	return expressions
}

func (b *splExpressionBuilder) searchExpression(ctx ISearchExpressionContext) *ConditionExpression {
	b.budget.step()
	if ctx == nil {
		// tstats and mstats without a where clause
		return nil
	}
	// Search has higher OR precedence than explicit and implicit AND.
	// Finish each contiguous OR group before joining it to the conjunction.
	// whereExpression uses the separate AND-before-OR expression grammar.
	var expression, orGroup *ConditionExpression
	operator := BooleanAnd
	for _, child := range ctx.GetChildren() {
		switch node := child.(type) {
		case ILogicalOpContext:
			operator = BooleanAnd
			if strings.EqualFold(node.GetText(), "OR") {
				operator = BooleanOr
			}
		case ISearchTermContext:
			for _, next := range b.searchTermExpressions(node) {
				if operator == BooleanOr {
					orGroup = combineConditionExpressions(BooleanOr, orGroup, next)
				} else {
					expression = combineConditionExpressions(BooleanAnd, expression, orGroup)
					orGroup = next
				}
				operator = BooleanAnd
			}
		}
	}
	return combineConditionExpressions(BooleanAnd, expression, orGroup)
}

func (b *splExpressionBuilder) searchTerm(ctx ISearchTermContext) *ConditionExpression {
	var expression *ConditionExpression
	for _, part := range b.searchTermExpressions(ctx) {
		expression = combineConditionExpressions(BooleanAnd, expression, part)
	}
	return expression
}

func (b *splExpressionBuilder) searchTermExpressions(ctx ISearchTermContext) []*ConditionExpression {
	b.budget.step()
	if ctx == nil {
		return nil
	}
	if ctx.NOT() != nil {
		parts := b.searchTermExpressions(ctx.SearchTerm())
		if len(parts) > 0 {
			parts[0] = demorganConditionExpression(parts[0])
		}
		return parts
	}
	if nested := ctx.SearchExpression(); nested != nil {
		return []*ConditionExpression{b.searchExpression(nested)}
	}
	if ctx.Condition() != nil {
		shape := classifyCondition(ctx.Condition(), b.budget)
		switch shape.kind {
		case predicateAtom:
			return []*ConditionExpression{b.boundCondition(shape.source, 0, 1)}
		case predicateNeutral:
			return nil
		default:
			b.incomplete = true
			return nil
		}
	}
	if bare := ctx.BareWord(); bare != nil && bareWordIsSearchPredicate(bare, b.budget) {
		var parts []*ConditionExpression
		span := searchBareWordSpan(bare, b.budget)
		for i := range span {
			b.budget.step()
			parts = append(parts, b.boundCondition(bare, i, len(span)))
		}
		return parts
	}
	return nil
}

// searchBareWordSpan joins only physically adjacent unquoted bare-word pieces.
// The grammar can parse key* as two terms, whereas key * is an implicit AND.
// Extraction and expression construction must consume the same single leaf.
func searchBareWordSpan(ctx IBareWordContext, budget *parseBudget) []string {
	budget.step()
	value := ctx.GetStart().GetInputStream().GetText(ctx.GetStart().GetStart(), ctx.GetStop().GetStop())
	if ctx.QUOTED_STRING() != nil {
		return []string{trimMatchingQuotes(value)}
	}
	term, ok := ctx.GetParent().(ISearchTermContext)
	if !ok {
		return strings.Fields(value)
	}
	for {
		budget.step()
		parent, nested := term.GetParent().(ISearchTermContext)
		if !nested {
			break
		}
		term = parent
	}
	expression, ok := term.GetParent().(ISearchExpressionContext)
	if !ok {
		return strings.Fields(value)
	}
	terms := expression.AllSearchTerm()
	// A whole positive, unquoted search * stage adds no event predicate.
	// Keep wildcard operands inside Boolean expressions and negated terms.
	if len(terms) == 1 && value == "*" && term.NOT() == nil {
		if _, standalone := expression.GetParent().(ISearchCommandContext); standalone {
			return nil
		}
	}
	for i, candidate := range terms {
		budget.step()
		if candidate != term {
			continue
		}
		if i > 0 && adjacentBareSearchTerms(terms[i-1], candidate, budget) {
			return nil
		}
		for j := i + 1; j < len(terms) && adjacentBareSearchTerms(terms[j-1], terms[j], budget); j++ {
			value = ctx.GetStart().GetInputStream().GetText(ctx.GetStart().GetStart(), terms[j].GetStop().GetStop())
		}
		break
	}
	return strings.Fields(value)
}

func adjacentBareSearchTerms(left, right ISearchTermContext, budget *parseBudget) bool {
	budget.step()
	for left.NOT() != nil {
		budget.step()
		left = left.SearchTerm()
	}
	return left.BareWord() != nil && right.BareWord() != nil &&
		left.BareWord().QUOTED_STRING() == nil && right.BareWord().QUOTED_STRING() == nil &&
		left.GetStop().GetStop()+1 == right.GetStart().GetStart()
}

func hasUnsupportedSearchOperand(node antlr.Tree, budget *parseBudget) bool {
	budget.step()
	if search, ok := node.(ISearchCommandContext); ok && search.SEARCH() == nil && search.GetStart().GetTokenIndex() > 0 {
		return false
	}
	if term, ok := node.(ISearchTermContext); ok && (term.MACRO() != nil || term.Subsearch() != nil) {
		return true
	}
	if _, subsearch := node.(ISubsearchContext); subsearch {
		return false
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if hasUnsupportedSearchOperand(node.GetChild(i), budget) {
			return true
		}
	}
	return false
}

// The grammar also accepts unknown pipeline commands as implicit searches.
// Only the initial stage can be an implicit search; subsequent searches use
// SEARCH. Do not turn an unknown command name or its arguments into keywords.
func bareWordIsSearchPredicate(ctx IBareWordContext, budget *parseBudget) bool {
	budget.step()
	for parent := ctx.GetParent(); parent != nil; parent = parent.GetParent() {
		budget.step()
		if search, ok := parent.(ISearchCommandContext); ok {
			return search.SEARCH() != nil || search.GetStart().GetTokenIndex() == 0
		}
	}
	return true
}

func (b *splExpressionBuilder) whereExpression(ctx IExpressionContext) *ConditionExpression {
	b.budget.step()
	if ctx == nil {
		return nil
	}
	return b.orExpression(ctx.OrExpression())
}

func (b *splExpressionBuilder) orExpression(ctx IOrExpressionContext) *ConditionExpression {
	b.budget.step()
	if ctx == nil {
		return nil
	}
	children := ctx.AllAndExpression()
	if len(children) == 0 {
		return nil
	}
	var expression *ConditionExpression
	for _, child := range children {
		childExpression := b.andExpression(child)
		if childExpression == nil {
			if len(children) > 1 {
				return nil
			}
			continue
		}
		expression = combineConditionExpressions(BooleanOr, expression, childExpression)
	}
	return expression
}

func (b *splExpressionBuilder) andExpression(ctx IAndExpressionContext) *ConditionExpression {
	b.budget.step()
	if ctx == nil {
		return nil
	}
	var expression *ConditionExpression
	for _, child := range ctx.AllNotExpression() {
		expression = combineConditionExpressions(BooleanAnd, expression, b.notExpression(child))
	}
	return expression
}

func (b *splExpressionBuilder) notExpression(ctx INotExpressionContext) *ConditionExpression {
	b.budget.step()
	if ctx == nil {
		return nil
	}
	if ctx.NOT() != nil {
		return demorganConditionExpression(b.notExpression(ctx.NotExpression()))
	}
	return b.comparisonExpression(ctx.ComparisonExpression())
}

func (b *splExpressionBuilder) comparisonExpression(ctx IComparisonExpressionContext) *ConditionExpression {
	b.budget.step()
	if ctx == nil {
		return nil
	}
	shape := classifyComparison(ctx, b.budget)
	switch shape.kind {
	case predicateAtom:
		return b.boundCondition(shape.source, 0, 1)
	case predicateParenthesis:
		return b.whereExpression(shape.nested)
	case predicateNeutral:
		return nil
	default:
		b.incomplete = true
		return nil
	}
}

type predicateShapeKind uint8

const (
	predicateUnsupported predicateShapeKind = iota
	predicateAtom
	predicateParenthesis
	predicateNeutral
)

type predicateShape struct {
	kind   predicateShapeKind
	source antlr.Tree
	nested IExpressionContext
}

func directRuleChild[T antlr.Tree](node antlr.Tree, budget *parseBudget) (T, bool) {
	var zero T
	budget.step()
	if node == nil || node.GetChildCount() != 1 {
		return zero, false
	}
	child, ok := node.GetChild(0).(T)
	return child, ok
}

// classifyComparison descends only through a complete operator-free grammar
// chain. A recognizable descendant of arithmetic or an outer comparison is
// never enough to claim the whole predicate.
func classifyComparison(ctx IComparisonExpressionContext, budget *parseBudget) predicateShape {
	if ctx == nil {
		return predicateShape{}
	}
	if condition, ok := directRuleChild[IConditionContext](ctx, budget); ok {
		return classifyCondition(condition, budget)
	}
	additive, ok := directRuleChild[IAdditiveExpressionContext](ctx, budget)
	if !ok {
		return predicateShape{}
	}
	multiplicative, ok := directRuleChild[IMultiplicativeExpressionContext](additive, budget)
	if !ok {
		return predicateShape{}
	}
	unary, ok := directRuleChild[IUnaryExpressionContext](multiplicative, budget)
	if !ok {
		return predicateShape{}
	}
	primary, ok := directRuleChild[IPrimaryExpressionContext](unary, budget)
	if !ok || primary.GetChildCount() != 3 || primary.LPAREN() == nil || primary.RPAREN() == nil || primary.Expression() == nil {
		return predicateShape{}
	}
	budget.step()
	if primary.GetChild(0) != primary.LPAREN() || primary.GetChild(1) != primary.Expression() || primary.GetChild(2) != primary.RPAREN() {
		return predicateShape{}
	}
	return predicateShape{kind: predicateParenthesis, nested: primary.Expression()}
}

func classifyCondition(ctx IConditionContext, budget *parseBudget) predicateShape {
	budget.step()
	if ctx == nil {
		return predicateShape{}
	}
	if ctx.FieldName() != nil {
		comparison := ctx.ComparisonOp() != nil && ctx.Value() != nil && ctx.GetChildCount() == 3 &&
			ctx.GetChild(0) == ctx.FieldName() && ctx.GetChild(1) == ctx.ComparisonOp() && ctx.GetChild(2) == ctx.Value()
		literalIN := ctx.IN() != nil && ctx.ValueList() != nil && ctx.Subsearch() == nil && ctx.GetChildCount() == 5 &&
			ctx.GetChild(0) == ctx.FieldName() && ctx.GetChild(1) == ctx.IN() && ctx.GetChild(2) == ctx.LPAREN() &&
			ctx.GetChild(3) == ctx.ValueList() && ctx.GetChild(4) == ctx.RPAREN()
		if !comparison && !literalIN {
			return predicateShape{}
		}
		fieldLower := strings.ToLower(ctx.FieldName().GetText())
		if isExcludedField(fieldLower) && (!comparison || !statsColumnNames[fieldLower]) {
			return predicateShape{kind: predicateNeutral}
		}
		return predicateShape{kind: predicateAtom, source: ctx}
	}
	call, ok := directRuleChild[IFunctionCallContext](ctx, budget)
	if !ok || !representedBareFunction(call, budget) {
		return predicateShape{}
	}
	return predicateShape{kind: predicateAtom, source: call}
}

func representedBareFunction(call IFunctionCallContext, budget *parseBudget) bool {
	budget.step()
	if call == nil || call.ArgumentList() == nil || call.GetChildCount() != 4 ||
		call.GetChild(1) != call.LPAREN() || call.GetChild(2) != call.ArgumentList() || call.GetChild(3) != call.RPAREN() {
		return false
	}
	args := call.ArgumentList()
	if call.ISNULL() != nil || call.ISNOTNULL() != nil {
		if args.GetChildCount() != 1 {
			return false
		}
		field, ok := args.GetChild(0).(IExpressionContext)
		return ok && directFieldArgument(field, budget)
	}
	if args.GetChildCount() != 3 || args.COMMA(0) == nil {
		return false
	}
	first, firstOK := args.GetChild(0).(IExpressionContext)
	second, secondOK := args.GetChild(2).(IExpressionContext)
	if !firstOK || !secondOK || args.GetChild(1) != args.COMMA(0) {
		return false
	}
	if call.CIDRMATCH() != nil {
		return directQuotedArgument(first, budget) && directFieldArgument(second, budget)
	}
	return (call.MATCH() != nil || call.LIKE() != nil) && directFieldArgument(first, budget) && directQuotedArgument(second, budget)
}

func directArgumentPrimary(ctx IExpressionContext, budget *parseBudget) IPrimaryExpressionContext {
	or, ok := directRuleChild[IOrExpressionContext](ctx, budget)
	if !ok {
		return nil
	}
	and, ok := directRuleChild[IAndExpressionContext](or, budget)
	if !ok {
		return nil
	}
	not, ok := directRuleChild[INotExpressionContext](and, budget)
	if !ok {
		return nil
	}
	comparison, ok := directRuleChild[IComparisonExpressionContext](not, budget)
	if !ok {
		return nil
	}
	additive, ok := directRuleChild[IAdditiveExpressionContext](comparison, budget)
	if !ok {
		return nil
	}
	multiplicative, ok := directRuleChild[IMultiplicativeExpressionContext](additive, budget)
	if !ok {
		return nil
	}
	unary, ok := directRuleChild[IUnaryExpressionContext](multiplicative, budget)
	if !ok {
		return nil
	}
	primary, ok := directRuleChild[IPrimaryExpressionContext](unary, budget)
	if !ok {
		return nil
	}
	return primary
}

func directFieldArgument(ctx IExpressionContext, budget *parseBudget) bool {
	primary := directArgumentPrimary(ctx, budget)
	if primary == nil || primary.GetChildCount() != 1 {
		return false
	}
	if primary.FieldName() != nil {
		return primary.GetChild(0) == primary.FieldName()
	}
	if value := primary.Value(); value != nil && value.IDENTIFIER() != nil && value.GetChildCount() == 1 {
		lower := strings.ToLower(value.GetText())
		return primary.GetChild(0) == value && lower != "true" && lower != "false" && lower != "null"
	}
	return false
}

func directQuotedArgument(ctx IExpressionContext, budget *parseBudget) bool {
	primary := directArgumentPrimary(ctx, budget)
	return primary != nil && primary.GetChildCount() == 1 && primary.Value() != nil && primary.GetChild(0) == primary.Value() &&
		primary.Value().GetChildCount() == 1 && primary.Value().QUOTED_STRING() != nil
}

func (b *splExpressionBuilder) condition() *ConditionExpression {
	b.budget.step()
	if b.next >= len(b.conditions) {
		b.incomplete = true
		return nil
	}
	index := b.next
	b.next++
	return &ConditionExpression{Operator: BooleanCondition, ConditionIndex: index}
}

func (b *splExpressionBuilder) boundCondition(source antlr.Tree, offset, cardinality int) *ConditionExpression {
	if b.sources == nil {
		return b.condition()
	}
	b.budget.step()
	binding, found := b.sources[source]
	if !found || binding.invalid || binding.count != cardinality || offset < 0 || offset >= cardinality {
		b.incomplete = true
		return nil
	}
	index := binding.first + offset
	if index < 0 || index >= len(b.used) || index != b.next || b.used[index] {
		b.incomplete = true
		return nil
	}
	b.used[index] = true
	b.next++
	return &ConditionExpression{Operator: BooleanCondition, ConditionIndex: index}
}

func remapConditionExpression(expression *ConditionExpression, indexMap []int, budgets ...*parseBudget) *ConditionExpression {
	var budget *parseBudget
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	budget.step()
	if expression == nil || len(indexMap) == 0 {
		return expression
	}
	switch expression.Operator {
	case BooleanCondition:
		if expression.ConditionIndex < 0 || expression.ConditionIndex >= len(indexMap) {
			return nil
		}
		mapped := indexMap[expression.ConditionIndex]
		if mapped < 0 {
			return nil
		}
		return &ConditionExpression{Operator: BooleanCondition, ConditionIndex: mapped}
	case BooleanAnd, BooleanOr:
		converted := &ConditionExpression{Operator: expression.Operator}
		for _, child := range expression.Children {
			budget.step()
			remapped := remapConditionExpression(child, indexMap, budget)
			if remapped == nil {
				continue
			}
			if remapped.Operator == expression.Operator {
				for _, grandchild := range remapped.Children {
					budget.step()
					converted.Children = appendUniqueConditionExpression(converted.Children, grandchild, budget)
				}
				continue
			}
			converted.Children = appendUniqueConditionExpression(converted.Children, remapped, budget)
		}
		if len(converted.Children) == 0 {
			return nil
		}
		if len(converted.Children) == 1 {
			return converted.Children[0]
		}
		return converted
	default:
		return nil
	}
}

func appendUniqueConditionExpression(children []*ConditionExpression, child *ConditionExpression, budgets ...*parseBudget) []*ConditionExpression {
	var budget *parseBudget
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	for _, existing := range children {
		budget.step()
		if equalConditionExpression(existing, child, budget) {
			return children
		}
	}
	return append(children, child)
}

func equalConditionExpression(a, b *ConditionExpression, budgets ...*parseBudget) bool {
	var budget *parseBudget
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	budget.step()
	if a == nil || b == nil {
		return a == b
	}
	if a.Operator != b.Operator || a.ConditionIndex != b.ConditionIndex || len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		budget.step()
		if !equalConditionExpression(a.Children[i], b.Children[i], budget) {
			return false
		}
	}
	return true
}

func combineConditionExpressions(operator BooleanOperator, left, right *ConditionExpression) *ConditionExpression {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	combined := &ConditionExpression{Operator: operator}
	if left.Operator == operator {
		combined.Children = append(combined.Children, left.Children...)
	} else {
		combined.Children = append(combined.Children, left)
	}
	if right.Operator == operator {
		combined.Children = append(combined.Children, right.Children...)
	} else {
		combined.Children = append(combined.Children, right)
	}
	return combined
}

func demorganConditionExpression(expression *ConditionExpression) *ConditionExpression {
	if expression == nil {
		return nil
	}
	switch expression.Operator {
	case BooleanAnd:
		converted := &ConditionExpression{Operator: BooleanOr}
		for _, child := range expression.Children {
			converted.Children = append(converted.Children, demorganConditionExpression(child))
		}
		return converted
	case BooleanOr:
		converted := &ConditionExpression{Operator: BooleanAnd}
		for _, child := range expression.Children {
			converted.Children = append(converted.Children, demorganConditionExpression(child))
		}
		return converted
	default:
		return expression
	}
}
