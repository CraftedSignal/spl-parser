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
	expression, _ := buildConditionExpressionWithCompleteness(tree, conditions, budget)
	return expression
}

// A nil expression can be intentional for a single filter or a command with
// no event predicate. incomplete distinguishes it from a dropped Boolean atom.
func buildConditionExpressionWithCompleteness(tree antlr.Tree, conditions []Condition, budget *parseBudget) (*ConditionExpression, bool) {
	if tree == nil {
		return nil, false
	}
	if expression, incomplete := buildPipelineConditionExpressionWithCompleteness(tree, conditions, budget); incomplete {
		return nil, true
	} else if expression != nil && len(conditions) >= 2 {
		return expression, false
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

func buildPipelineConditionExpressionWithCompleteness(tree antlr.Tree, conditions []Condition, budget *parseBudget) (*ConditionExpression, bool) {
	builder := &splExpressionBuilder{conditions: conditions, budget: budget}
	expression := builder.tree(tree)
	if builder.incomplete {
		return nil, true
	}
	if expression != nil && builder.next == len(conditions) {
		return expression, false
	}
	return nil, false
}

type splExpressionBuilder struct {
	budget     *parseBudget
	conditions []Condition
	next       int
	incomplete bool
}

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
		return []*ConditionExpression{b.condition()}
	}
	if bare := ctx.BareWord(); bare != nil && bareWordIsSearchPredicate(bare, b.budget) {
		var parts []*ConditionExpression
		for range searchBareWordSpan(bare, b.budget) {
			parts = append(parts, b.condition())
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
	if nested := nestedWhereExpression(ctx); nested != nil {
		return b.whereExpression(nested)
	}
	if condition := ctx.Condition(); condition != nil && condition.FieldName() != nil {
		fieldLower := strings.ToLower(condition.FieldName().GetText())
		if isExcludedField(fieldLower) && (condition.ComparisonOp() == nil || !statsColumnNames[fieldLower]) {
			// Scope metadata is intentionally excluded from event predicates.
			return nil
		}
	}
	if !comparisonExpressionExtractsCondition(ctx, b.budget) {
		b.incomplete = true
		return nil
	}
	return b.condition()
}

func nestedWhereExpression(node antlr.Tree) IExpressionContext {
	if node == nil {
		return nil
	}
	switch ctx := node.(type) {
	case IFunctionCallContext:
		return nil
	case IExpressionContext:
		return ctx
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if nested := nestedWhereExpression(node.GetChild(i)); nested != nil {
			return nested
		}
	}
	return nil
}

func comparisonExpressionExtractsCondition(ctx IComparisonExpressionContext, budget *parseBudget) bool {
	if ctx == nil {
		return false
	}
	condition := ctx.Condition()
	if condition == nil {
		// A function inside an outer comparison (for example isnull(y)=false)
		// does not represent the complete Boolean predicate.
		return false
	}
	if condition.FieldName() != nil {
		return condition.ComparisonOp() != nil && condition.Value() != nil ||
			condition.IN() != nil && condition.ValueList() != nil
	}
	return extractedFunctionCondition(condition.FunctionCall(), budget)
}

// These are the bare function predicates that EnterFunctionCall emits as a
// Condition. Argument counts mirror its emission branches.
func extractedFunctionCondition(ctx IFunctionCallContext, budget *parseBudget) bool {
	if ctx == nil || ctx.ArgumentList() == nil {
		return false
	}
	required := 0
	if ctx.CIDRMATCH() != nil || ctx.MATCH() != nil || ctx.LIKE() != nil {
		required = 2
	} else if ctx.ISNULL() != nil || ctx.ISNOTNULL() != nil {
		required = 1
	}
	if required == 0 {
		return false
	}
	count := 0
	args := ctx.ArgumentList()
	for i := 0; i < args.GetChildCount(); i++ {
		budget.step()
		if _, ok := args.GetChild(i).(IExpressionContext); ok {
			count++
			if count == required {
				return true
			}
		}
	}
	return false
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
