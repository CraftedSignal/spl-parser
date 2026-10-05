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

func buildConditionExpression(tree antlr.Tree, conditions []Condition) *ConditionExpression {
	if tree == nil || len(conditions) < 2 {
		return nil
	}
	if expression := buildPipelineConditionExpression(tree, conditions); expression != nil {
		return expression
	}
	for _, candidate := range collectSPLSearchExpressions(tree) {
		builder := &splExpressionBuilder{conditions: conditions}
		expression := builder.searchExpression(candidate)
		if expression != nil && builder.next == len(conditions) {
			return expression
		}
	}
	return nil
}

func buildPipelineConditionExpression(tree antlr.Tree, conditions []Condition) *ConditionExpression {
	builder := &splExpressionBuilder{conditions: conditions}
	expression := builder.tree(tree)
	if expression != nil && builder.next == len(conditions) {
		return expression
	}
	return nil
}

type splExpressionBuilder struct {
	conditions []Condition
	next       int
}

func (b *splExpressionBuilder) tree(node antlr.Tree) *ConditionExpression {
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

func collectSPLSearchExpressions(tree antlr.Tree) []ISearchExpressionContext {
	var expressions []ISearchExpressionContext
	var visit func(antlr.Tree)
	visit = func(node antlr.Tree) {
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
	var expression *ConditionExpression
	operator := BooleanAnd
	for _, child := range ctx.GetChildren() {
		switch node := child.(type) {
		case ILogicalOpContext:
			operator = BooleanAnd
			if strings.EqualFold(node.GetText(), "OR") {
				operator = BooleanOr
			}
		case ISearchTermContext:
			next := b.searchTerm(node)
			if next == nil {
				operator = BooleanAnd
				continue
			}
			expression = combineConditionExpressions(operator, expression, next)
			operator = BooleanAnd
		}
	}
	return expression
}

func (b *splExpressionBuilder) searchTerm(ctx ISearchTermContext) *ConditionExpression {
	if ctx == nil {
		return nil
	}
	if ctx.NOT() != nil {
		return demorganConditionExpression(b.searchTerm(ctx.SearchTerm()))
	}
	if nested := ctx.SearchExpression(); nested != nil {
		return b.searchExpression(nested)
	}
	if ctx.Condition() != nil {
		return b.condition()
	}
	return nil
}

func (b *splExpressionBuilder) whereExpression(ctx IExpressionContext) *ConditionExpression {
	if ctx == nil {
		return nil
	}
	return b.orExpression(ctx.OrExpression())
}

func (b *splExpressionBuilder) orExpression(ctx IOrExpressionContext) *ConditionExpression {
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
	if ctx == nil {
		return nil
	}
	if ctx.NOT() != nil {
		return demorganConditionExpression(b.notExpression(ctx.NotExpression()))
	}
	return b.comparisonExpression(ctx.ComparisonExpression())
}

func (b *splExpressionBuilder) comparisonExpression(ctx IComparisonExpressionContext) *ConditionExpression {
	if ctx == nil {
		return nil
	}
	if nested := nestedWhereExpression(ctx); nested != nil {
		return b.whereExpression(nested)
	}
	if !comparisonExpressionExtractsCondition(ctx) {
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

func comparisonExpressionExtractsCondition(ctx IComparisonExpressionContext) bool {
	if ctx == nil {
		return false
	}
	if ctx.Condition() != nil {
		return true
	}
	return containsExtractedFunctionCondition(ctx)
}

func containsExtractedFunctionCondition(node antlr.Tree) bool {
	if node == nil {
		return false
	}
	switch ctx := node.(type) {
	case ISubsearchContext:
		return false
	case IFunctionCallContext:
		return ctx.CIDRMATCH() != nil ||
			ctx.MATCH() != nil ||
			ctx.LIKE() != nil ||
			ctx.ISNOTNULL() != nil ||
			ctx.ISNULL() != nil
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if containsExtractedFunctionCondition(node.GetChild(i)) {
			return true
		}
	}
	return false
}

func (b *splExpressionBuilder) condition() *ConditionExpression {
	if b.next >= len(b.conditions) {
		return nil
	}
	index := b.next
	b.next++
	return &ConditionExpression{Operator: BooleanCondition, ConditionIndex: index}
}

func remapConditionExpression(expression *ConditionExpression, indexMap []int) *ConditionExpression {
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
			remapped := remapConditionExpression(child, indexMap)
			if remapped == nil {
				continue
			}
			if remapped.Operator == expression.Operator {
				for _, grandchild := range remapped.Children {
					converted.Children = appendUniqueConditionExpression(converted.Children, grandchild)
				}
				continue
			}
			converted.Children = appendUniqueConditionExpression(converted.Children, remapped)
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

func appendUniqueConditionExpression(children []*ConditionExpression, child *ConditionExpression) []*ConditionExpression {
	for _, existing := range children {
		if equalConditionExpression(existing, child) {
			return children
		}
	}
	return append(children, child)
}

func equalConditionExpression(a, b *ConditionExpression) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Operator != b.Operator || a.ConditionIndex != b.ConditionIndex || len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		if !equalConditionExpression(a.Children[i], b.Children[i]) {
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
