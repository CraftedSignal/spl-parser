package spl

import "github.com/antlr4-go/antlr/v4"

// indexTimeModifierSearch recognizes only the proved direct search occurrence,
// not a same-spelled event field, quoted phrase or argument of another command.
func indexTimeModifierSearch(ctx IConditionContext, budget *parseBudget) ISearchCommandContext {
	budget.step()
	if ctx == nil || ctx.GetChildCount() != 3 || ctx.FieldName() == nil || ctx.ComparisonOp() == nil || ctx.ComparisonOp().EQ() == nil || ctx.Value() == nil {
		return nil
	}
	field := ctx.FieldName()
	if field.GetChildCount() != 1 {
		return nil
	}
	base, ok := field.GetChild(0).(IFieldNameBaseContext)
	if !ok || base.GetChildCount() != 1 || base.IDENTIFIER(0) == nil {
		return nil
	}
	token := base.IDENTIFIER(0).GetSymbol()
	width := token.GetStop() - token.GetStart() + 1
	if width != len("_index_earliest") && width != len("_index_latest") {
		return nil
	}
	// The token width bounds this exact comparison before GetText materializes it.
	budget.step()
	name := token.GetText()
	if name != "_index_earliest" && name != "_index_latest" {
		return nil
	}
	for parent := ctx.GetParent(); parent != nil; parent = parent.GetParent() {
		budget.step()
		switch owner := parent.(type) {
		case ISearchCommandContext:
			if implicitPipelineCommand(owner, budget) {
				return nil
			}
			return owner
		case IQueryContext, ISubsearchContext, IPipelineStageContext, IFunctionCallContext:
			return nil
		}
	}
	return nil
}

// searchHasIndexTimeModifier scans the existing tree without allocating a child
// list. Subqueries have separate extraction owners and are never crossed.
func searchHasIndexTimeModifier(node antlr.Tree, owner ISearchCommandContext, budget *parseBudget) bool {
	budget.step()
	if node == nil {
		return false
	}
	if _, child := node.(ISubsearchContext); child {
		return false
	}
	if condition, ok := node.(IConditionContext); ok {
		if indexTimeModifierSearch(condition, budget) == owner {
			return true
		}
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if searchHasIndexTimeModifier(node.GetChild(i), owner, budget) {
			return true
		}
	}
	return false
}
