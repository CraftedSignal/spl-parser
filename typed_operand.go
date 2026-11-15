package spl

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

func appendIdentityPart(builder *strings.Builder, value string) {
	builder.WriteString(strconv.Itoa(len(value)))
	builder.WriteByte(':')
	builder.WriteString(value)
}

func operandIdentity(operand *Operand, budget *parseBudget) string {
	if operand == nil {
		return "0:"
	}
	var builder strings.Builder
	appendIdentityPart(&builder, string(operand.Kind))
	appendIdentityPart(&builder, operand.Value)
	appendIdentityPart(&builder, operand.Source)
	for i := range operand.Items {
		budget.step()
		appendIdentityPart(&builder, operandIdentity(&operand.Items[i], budget))
	}
	return builder.String()
}

func cloneOperand(operand *Operand, budget *parseBudget) *Operand {
	if operand == nil {
		return nil
	}
	budget.reserveOutput(1)
	copy := *operand
	if operand.Items != nil {
		copy.Items = make([]Operand, len(operand.Items))
		for i := range operand.Items {
			budget.step()
			item := cloneOperand(&operand.Items[i], budget)
			copy.Items[i] = *item
		}
	}
	return &copy
}

func operandNodeCount(operand *Operand, budget *parseBudget) int {
	if operand == nil {
		return 0
	}
	count := 1
	for i := range operand.Items {
		budget.step()
		count += operandNodeCount(&operand.Items[i], budget)
	}
	return count
}

// predicateContextFor follows the owning grammar scope. A nested query has its
// own command nodes, so the nearest owner wins.
func predicateContextFor(node antlr.Tree, budget *parseBudget) PredicateContext {
	for parent := node; parent != nil; {
		budget.step()
		switch owner := parent.(type) {
		case IWhereCommandContext:
			return PredicateEval
		case IInputlookupCommandContext, ITstatsCommandContext, IMstatsCommandContext:
			return PredicateSearch
		case ISearchCommandContext:
			if owner.SEARCH() == nil && owner.GetStart().GetTokenIndex() > 0 {
				return ""
			}
			return PredicateSearch
		}
		context, ok := parent.(antlr.ParserRuleContext)
		if !ok {
			break
		}
		parent = context.GetParent()
	}
	return ""
}

func operandFromValue(ctx IValueContext, syntax PredicateContext) *Operand {
	if ctx == nil {
		return nil
	}
	source := ctx.GetText()
	value := source
	if ctx.QUOTED_STRING() != nil {
		value = decodeEscapedQuotes(trimMatchingQuotes(source))
		if syntax == PredicateEval && strings.HasPrefix(source, "'") {
			return &Operand{Kind: OperandField, Source: source, Value: value}
		}
		return &Operand{Kind: OperandString, Source: source, Value: value}
	}
	if ctx.NUMBER() != nil {
		return &Operand{Kind: OperandNumber, Source: source, Value: value}
	}
	if syntax == PredicateEval && ctx.IDENTIFIER() != nil {
		if unresolvedEvalName(value) {
			return nil
		}
		return &Operand{Kind: OperandField, Source: source, Value: value}
	}
	return &Operand{Kind: OperandBare, Source: source, Value: value}
}

func operandFromField(ctx IFieldNameContext, syntax PredicateContext) *Operand {
	if ctx == nil {
		return nil
	}
	source := ctx.GetText()
	if syntax == PredicateEval {
		if ctx.NUMBER() != nil {
			return &Operand{Kind: OperandNumber, Source: source, Value: source}
		}
		if unresolvedEvalName(source) {
			return nil
		}
		// The search grammar permits hyphenated names. In eval syntax that
		// spelling is arithmetic unless it is quoted.
		if strings.Contains(source, "-") {
			return nil
		}
	}
	return &Operand{Kind: OperandField, Source: source, Value: source}
}

func unresolvedEvalName(value string) bool {
	switch strings.ToLower(value) {
	case "true", "false", "null":
		return true
	}
	return false
}

func invertComparison(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op
}

func directConditionOperands(ctx IConditionContext, syntax PredicateContext) (lhs, rhs *Operand, scalar bool) {
	if ctx == nil || ctx.FieldName() == nil {
		return nil, nil, false
	}
	lhs = operandFromField(ctx.FieldName(), syntax)
	if ctx.Value() != nil {
		rhs = operandFromValue(ctx.Value(), syntax)
	}
	if syntax == PredicateEval {
		if lhs == nil || (ctx.Value() != nil && rhs == nil) {
			return lhs, rhs, true
		}
		if ctx.ComparisonOp() != nil && lhs.Kind != OperandField && rhs != nil && rhs.Kind != OperandField {
			return lhs, rhs, true
		}
		if ctx.IN() != nil && lhs.Kind != OperandField {
			return lhs, rhs, true
		}
	}
	return lhs, rhs, false
}

func directAdditiveOperand(ctx IAdditiveExpressionContext, budget *parseBudget) *Operand {
	mul, ok := directRuleChild[IMultiplicativeExpressionContext](ctx, budget)
	if !ok {
		return nil
	}
	unary, ok := directRuleChild[IUnaryExpressionContext](mul, budget)
	if !ok {
		return nil
	}
	negative := false
	if unary.MINUS() != nil && unary.GetChildCount() == 2 {
		negative = true
		unary = unary.UnaryExpression()
	}
	primary, ok := directRuleChild[IPrimaryExpressionContext](unary, budget)
	if !ok {
		return nil
	}
	var operand *Operand
	if primary.Value() != nil && primary.GetChildCount() == 1 {
		operand = operandFromValue(primary.Value(), PredicateEval)
	} else if primary.FieldName() != nil && primary.GetChildCount() == 1 {
		operand = operandFromField(primary.FieldName(), PredicateEval)
	}
	if operand == nil {
		return nil
	}
	if negative {
		if operand.Kind != OperandNumber {
			return nil
		}
		operand.Source = "-" + operand.Source
		operand.Value = "-" + operand.Value
	}
	return operand
}

func directExpressionOperand(ctx IExpressionContext, budget *parseBudget) *Operand {
	primary := directArgumentPrimary(ctx, budget)
	if primary == nil || primary.GetChildCount() != 1 {
		return nil
	}
	if primary.Value() != nil {
		return operandFromValue(primary.Value(), PredicateEval)
	}
	if primary.FieldName() != nil {
		return operandFromField(primary.FieldName(), PredicateEval)
	}
	return nil
}
