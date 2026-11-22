package spl

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

// JoinKeyPair is an explicit side-qualified equality, normalized to declared
// left/right orientation. Source preserves the authored equality.
type JoinKeyPair struct {
	LeftField  string `json:"left_field"`
	RightField string `json:"right_field"`
	Source     string `json:"source"`
}

// JoinAliasInfo distinguishes explicit side-specific keys from implicit common
// fields. Input references identify owner-relative scopes, not output bindings.
type JoinAliasInfo struct {
	LeftAlias  string         `json:"left_alias"`
	RightAlias string         `json:"right_alias"`
	Source     string         `json:"source"`
	Pairs      []JoinKeyPair  `json:"pairs"`
	LeftInput  ScopeReference `json:"left_input"`
	RightInput ScopeReference `json:"right_input"`
}

func joinAliasScan(budget *parseBudget, text string) {
	for range len(text) {
		budget.step()
	}
}

// Read identity only after charging its authored span. Quoted operands decode
// one native escape layer; unquoted field paths use the parser's token identity.
func (e *conditionExtractor) joinAliasOperand(ctx antlr.ParserRuleContext) string {
	if ctx == nil {
		return ""
	}
	source := e.sourceOf(ctx)
	joinAliasScan(e.budget, source)
	return decodeSPLQuotedOperand(trimMatchingQuotes(ctx.GetText()))
}

// Detect the active clause even when parser recovery retained only its prefix.
// The canonical subsearch boundary keeps child WHERE commands out of this scan.
func (e *conditionExtractor) joinAliasClause(ctx *JoinCommandContext) IJoinAliasWhereContext {
	for _, child := range ctx.GetChildren() {
		e.budget.step()
		if clause, ok := child.(IJoinAliasWhereContext); ok {
			return clause
		}
	}
	return nil
}

func (e *conditionExtractor) hasJoinAliasClause(ctx *JoinCommandContext, clause IJoinAliasWhereContext) bool {
	if clause != nil {
		return true
	}
	if ctx.GetStart() == nil || e.tokenStream == nil {
		return false
	}
	end := len(e.tokenStream.GetAllTokens())
	for _, node := range ctx.GetChildren() {
		e.budget.step()
		if child, ok := node.(ISubsearchContext); ok && child.GetStart() != nil {
			end = child.GetStart().GetTokenIndex()
			break
		}
	}
	tokens := e.tokenStream.GetAllTokens()
	if e.completeJoinLegacyHeader(ctx, tokens, end) {
		return false
	}
	children, childIndex := ctx.GetChildren(), 0
	for i := ctx.GetStart().GetTokenIndex() + 1; i < end; i++ {
		e.budget.step()
		optionOwned := false
		// Skip only complete option ranges. A recovered field-list prefix
		// cannot hide the WHERE of a malformed/mixed alias clause.
		for childIndex < len(children) {
			e.budget.step()
			option, ok := children[childIndex].(IJoinOptionContext)
			if !ok || option.GetStart() == nil || option.GetStop() == nil {
				childIndex++
				continue
			}
			start, stop := option.GetStart().GetTokenIndex(), option.GetStop().GetTokenIndex()
			if start > i {
				break
			}
			childIndex++
			if start == i && stop >= start && stop < end && joinAliasRuleComplete(option, e.budget) {
				i, optionOwned = stop, true
				break
			}
		}
		if optionOwned {
			continue
		}
		switch tokens[i].GetTokenType() {
		case SPLParserPIPE, antlr.TokenEOF, SPLParserLBRACKET:
			return false
		case SPLParserWHERE:
			return true
		}
	}
	return false
}

// A legacy header is complete only when clean typed options/fields cover all
// header tokens up to the actual subsearch. A valid field-list prefix followed
// by an unowned equality/operator is therefore not mistaken for a full header.
func (e *conditionExtractor) completeJoinLegacyHeader(ctx *JoinCommandContext, tokens []antlr.Token, end int) bool {
	cursor := ctx.GetStart().GetTokenIndex() + 1
	for _, child := range ctx.GetChildren() {
		e.budget.step()
		if _, ok := child.(ISubsearchContext); ok {
			break
		}
		if terminal, ok := child.(antlr.TerminalNode); ok && terminal.GetSymbol().GetTokenIndex() == cursor-1 && terminal.GetSymbol().GetTokenType() == SPLParserJOIN {
			continue
		}
		var rule antlr.ParserRuleContext
		switch typed := child.(type) {
		case IJoinOptionContext:
			rule = typed
		case IFieldListContext:
			rule = typed
		default:
			return false
		}
		for cursor < end && tokens[cursor].GetChannel() != antlr.TokenDefaultChannel {
			e.budget.step()
			cursor++
		}
		if rule.GetStart() == nil || rule.GetStop() == nil || rule.GetStart().GetTokenIndex() != cursor || rule.GetStop().GetTokenIndex() < cursor || rule.GetStop().GetTokenIndex() >= end || !joinAliasRuleComplete(rule, e.budget) {
			return false
		}
		cursor = rule.GetStop().GetTokenIndex() + 1
	}
	for cursor < end && tokens[cursor].GetChannel() != antlr.TokenDefaultChannel {
		e.budget.step()
		cursor++
	}
	return cursor == end && end < len(tokens) && tokens[end].GetTokenType() == SPLParserLBRACKET
}

func joinAliasRuleComplete(node antlr.Tree, budget *parseBudget) bool {
	budget.step()
	if _, bad := node.(antlr.ErrorNode); bad {
		return false
	}
	if terminal, ok := node.(antlr.TerminalNode); ok {
		return terminal.GetSymbol().GetTokenIndex() >= 0
	}
	if rule, ok := node.(antlr.ParserRuleContext); ok && (rule.GetStart() == nil || rule.GetStop() == nil || rule.GetStart().GetTokenIndex() < 0 || rule.GetStop().GetTokenIndex() < rule.GetStart().GetTokenIndex()) {
		return false
	}
	for _, child := range node.GetChildren() {
		if !joinAliasRuleComplete(child, budget) {
			return false
		}
	}
	return true
}

func (e *conditionExtractor) joinAliasError(source, reason string) {
	e.budget.reserveOutput(1)
	joinAliasScan(e.budget, source)
	e.errors = append(e.errors, fmt.Sprintf("invalid join alias clause %q: %s", source, reason))
}

func (e *conditionExtractor) extractJoinAlias(ctx *JoinCommandContext, clause IJoinAliasWhereContext, active bool) *JoinAliasInfo {
	if !active {
		return nil
	}
	e.aliasJoinActive = true
	if clause == nil {
		e.joinAliasError(e.sourceOf(ctx), "malformed or mixed field-list/alias clause")
		return nil
	}
	source := e.sourceOf(clause)
	var left, right string
	leftCount, rightCount := 0, 0
	for _, child := range ctx.GetChildren() {
		e.budget.step()
		opt, ok := child.(IJoinOptionContext)
		if !ok || opt.IDENTIFIER() == nil {
			continue
		}
		key := opt.IDENTIFIER().GetText()
		joinAliasScan(e.budget, key)
		isLeft, isRight := strings.EqualFold(key, "left"), strings.EqualFold(key, "right")
		if !isLeft && !isRight {
			continue
		}
		var value string
		switch {
		case opt.QUOTED_STRING() != nil:
			raw := opt.QUOTED_STRING().GetText()
			joinAliasScan(e.budget, raw)
			value = decodeSPLQuotedOperand(trimMatchingQuotes(raw))
		case opt.FieldName() != nil:
			value = e.joinAliasOperand(opt.FieldName())
		case opt.NUMBER() != nil:
			value = opt.NUMBER().GetText()
			joinAliasScan(e.budget, value)
		}
		if isLeft {
			leftCount++
			left = value
		} else {
			rightCount++
			right = value
		}
	}
	if leftCount != 1 || rightCount != 1 || left == "" || right == "" || left == right {
		e.joinAliasError(source, "requires distinct nonempty left/right aliases declared exactly once")
		return nil
	}
	e.budget.reserveOutput(1)
	info := &JoinAliasInfo{LeftAlias: left, RightAlias: right, Source: source}
	for _, child := range clause.GetChildren() {
		e.budget.step()
		pair, ok := child.(IJoinAliasPairContext)
		if !ok {
			continue
		}
		var operands [2]string
		n := 0
		for _, node := range pair.GetChildren() {
			e.budget.step()
			if operand, ok := node.(IFieldOrQuotedContext); ok {
				if n >= 2 {
					break
				}
				operands[n] = e.joinAliasOperand(operand)
				n++
			}
		}
		if n != 2 || pair.EQ() == nil {
			e.joinAliasError(source, "expected side-qualified equality pairs")
			return nil
		}
		ll, lr := joinAliasSuffix(operands[0], left, e.budget), joinAliasSuffix(operands[0], right, e.budget)
		rl, rr := joinAliasSuffix(operands[1], left, e.budget), joinAliasSuffix(operands[1], right, e.budget)
		forward, reverse := ll != "" && rr != "", lr != "" && rl != ""
		if forward == reverse {
			reason := "unknown qualifier or same-side equality"
			if forward {
				reason = "ambiguous alias-prefix resolution is unsupported"
			}
			e.joinAliasError(source, reason)
			return nil
		}
		p := JoinKeyPair{LeftField: ll, RightField: rr, Source: e.sourceOf(pair)}
		if reverse {
			p.LeftField, p.RightField = rl, lr
		}
		e.budget.reserveOutput(1)
		info.Pairs = append(info.Pairs, p)
	}
	if len(info.Pairs) == 0 {
		e.joinAliasError(source, "requires at least one equality pair")
		return nil
	}
	return info
}

func joinAliasSuffix(operand, alias string, budget *parseBudget) string {
	budget.step()
	if len(operand) <= len(alias)+1 {
		return ""
	}
	joinAliasScan(budget, alias)
	if !strings.HasPrefix(operand, alias) || operand[len(alias)] != '.' {
		return ""
	}
	return operand[len(alias)+1:]
}

func cloneJoinAlias(info *JoinAliasInfo, budget *parseBudget) *JoinAliasInfo {
	budget.step()
	if info == nil {
		return nil
	}
	budget.reserveOutput(3) // carrier and both independent scope references
	copy := *info
	budget.reserveOutput(len(info.Pairs))
	copy.Pairs = make([]JoinKeyPair, len(info.Pairs))
	for i, pair := range info.Pairs {
		budget.step()
		copy.Pairs[i] = pair
	}
	return &copy
}
