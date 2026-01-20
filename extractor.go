package spl

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

// Condition represents a field condition extracted from an SPL query
type Condition struct {
	Field       string   `json:"field"`
	Operator    string   `json:"operator"`
	Value       string   `json:"value"`
	Negated     bool     `json:"negated"`
	PipeStage   int      `json:"pipe_stage"`
	LogicalOp   string   `json:"logical_op"` // "AND" or "OR" connecting to previous condition
	Alternatives []string `json:"alternatives,omitempty"` // For OR conditions on same field
}

// ParseResult contains all conditions extracted from the query
type ParseResult struct {
	Conditions []Condition `json:"conditions"`
	Errors     []string    `json:"errors,omitempty"`
}

// splMetadataFields are index-time metadata fields
// These are now included in extraction for completeness
var splMetadataFields = map[string]bool{
	// Previously excluded, now included:
	// "index", "sourcetype", "source" - useful for rule context
	// Only exclude time-range modifiers that aren't actual conditions
	"earliest": true, "latest": true, "splunk_server": true,
}

// splCommandKeywords are SPL command keywords that should be excluded
// These are not field names
var splCommandKeywords = map[string]bool{
	"count": true, "sum": true, "avg": true, "min": true, "max": true,
	"search": true, "where": true, "eval": true, "stats": true,
	"table": true, "fields": true, "rename": true, "sort": true,
	"head": true, "tail": true, "dedup": true, "by": true,
	"as": true, "and": true, "or": true, "not": true,
	"span": true,
}

// isExcludedField returns true if a field should be excluded from condition extraction
func isExcludedField(fieldLower string) bool {
	return splMetadataFields[fieldLower] || splCommandKeywords[fieldLower]
}

// conditionExtractor walks the parse tree to extract conditions
type conditionExtractor struct {
	*BaseSPLParserListener
	conditions      []Condition
	computedFields  map[string]bool // Fields created by eval commands
	currentStage    int
	inSubsearch     int  // depth of subsearch nesting
	inMultisearch   int  // depth of multisearch nesting (extract from these subsearches)
	inFunctionCall  int  // depth of function call nesting (eval, count, etc.)
	inStatsFunction int  // depth of stats function nesting (count(), sum(), etc.)
	negated         bool
	lastLogicalOp   string
	errors          []string
}

// errorListener collects parse errors
type errorListener struct {
	*antlr.DefaultErrorListener
	errors []string
}

func (l *errorListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, e antlr.RecognitionException) {
	l.errors = append(l.errors, msg)
}

// ExtractConditions parses an SPL query and extracts all field conditions
func ExtractConditions(query string) *ParseResult {
	input := antlr.NewInputStream(query)
	lexer := NewSPLLexer(input)

	// Remove default error listener and add our own
	lexer.RemoveErrorListeners()
	lexerErrors := &errorListener{}
	lexer.AddErrorListener(lexerErrors)

	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := NewSPLParser(stream)

	// Remove default error listener and add our own
	parser.RemoveErrorListeners()
	parserErrors := &errorListener{}
	parser.AddErrorListener(parserErrors)

	// Parse the query
	tree := parser.Query()

	// Walk the tree to extract conditions
	extractor := &conditionExtractor{
		conditions:     make([]Condition, 0),
		computedFields: make(map[string]bool),
		lastLogicalOp:  "AND", // default
	}
	antlr.ParseTreeWalkerDefault.Walk(extractor, tree)

	// Combine errors
	allErrors := append(lexerErrors.errors, parserErrors.errors...)
	allErrors = append(allErrors, extractor.errors...)

	// Post-process to group OR conditions on same field
	conditions := groupORConditions(extractor.conditions)

	return &ParseResult{
		Conditions: conditions,
		Errors:     allErrors,
	}
}

// ExitPipelineStage increments the stage counter after processing each stage
func (e *conditionExtractor) ExitPipelineStage(ctx *PipelineStageContext) {
	e.currentStage++
}

// EnterSubsearch tracks when we enter a subsearch
func (e *conditionExtractor) EnterSubsearch(ctx *SubsearchContext) {
	e.inSubsearch++
}

// ExitSubsearch tracks when we exit a subsearch
func (e *conditionExtractor) ExitSubsearch(ctx *SubsearchContext) {
	e.inSubsearch--
}

// EnterMultisearchCommand tracks when we enter a multisearch command
// Multisearch subsearches contain the actual search conditions we want to extract
func (e *conditionExtractor) EnterMultisearchCommand(ctx *MultisearchCommandContext) {
	e.inMultisearch++
}

// ExitMultisearchCommand tracks when we exit a multisearch command
func (e *conditionExtractor) ExitMultisearchCommand(ctx *MultisearchCommandContext) {
	e.inMultisearch--
}

// shouldSkipSubsearch returns true if we should skip extracting from current subsearch
// We extract from multisearch subsearches but skip join/append/lookup subsearches
func (e *conditionExtractor) shouldSkipSubsearch() bool {
	return e.inSubsearch > 0 && e.inMultisearch == 0
}

// EnterFunctionCall tracks when we enter a function call (eval, count, sum, etc.)
// Conditions inside function calls are not filter conditions - they're aggregation expressions
func (e *conditionExtractor) EnterFunctionCall(ctx *FunctionCallContext) {
	e.inFunctionCall++
}

// ExitFunctionCall tracks when we exit a function call
func (e *conditionExtractor) ExitFunctionCall(ctx *FunctionCallContext) {
	e.inFunctionCall--
}

// EnterStatsFunction tracks when we enter a stats function (count(), sum(), etc.)
// Conditions inside stats functions are aggregation expressions, not filter conditions
func (e *conditionExtractor) EnterStatsFunction(ctx *StatsFunctionContext) {
	e.inStatsFunction++
}

// ExitStatsFunction tracks when we exit a stats function
func (e *conditionExtractor) ExitStatsFunction(ctx *StatsFunctionContext) {
	e.inStatsFunction--
}

// EnterEvalAssignment tracks computed fields from eval commands
func (e *conditionExtractor) EnterEvalAssignment(ctx *EvalAssignmentContext) {
	// Skip eval assignments in subsearches
	if e.inSubsearch > 0 {
		return
	}

	// Extract the field name being assigned to
	if ctx.FieldName() != nil {
		field := ctx.FieldName().GetText()
		e.computedFields[strings.ToLower(field)] = true
	}
}

// EnterBareWord extracts bare search terms (quoted strings used as fulltext search)
func (e *conditionExtractor) EnterBareWord(ctx *BareWordContext) {
	// Skip inside non-multisearch subsearches
	if e.shouldSkipSubsearch() {
		return
	}

	// Only extract quoted strings as keyword conditions
	if ctx.QUOTED_STRING() != nil {
		value := ctx.QUOTED_STRING().GetText()
		// Remove quotes
		value = strings.Trim(value, "\"'")

		// Create a keyword condition (field="_raw" or "_keyword")
		cond := Condition{
			Field:     "_raw",
			Operator:  "contains",
			Value:     value,
			Negated:   e.negated,
			PipeStage: e.currentStage,
			LogicalOp: e.lastLogicalOp,
		}
		e.conditions = append(e.conditions, cond)
		e.lastLogicalOp = "AND"
	}
}

// EnterCondition extracts field conditions
func (e *conditionExtractor) EnterCondition(ctx *ConditionContext) {
	// Skip conditions inside non-multisearch subsearches (like join/append)
	if e.shouldSkipSubsearch() {
		return
	}

	// Skip conditions inside function calls (like count(), sum(), etc.)
	// These are aggregation expressions, not filter conditions
	if e.inFunctionCall > 0 {
		return
	}

	// Skip conditions inside stats functions (like stats count(eval(field="x")))
	// These are aggregation expressions, not filter conditions
	if e.inStatsFunction > 0 {
		return
	}

	// Check for field comparison: field op value
	if ctx.FieldName() != nil && ctx.ComparisonOp() != nil && ctx.Value() != nil {
		field := ctx.FieldName().GetText()
		fieldLower := strings.ToLower(field)

		// Skip SPL keywords (metadata fields like index, sourcetype, etc.)
		if isExcludedField(fieldLower) {
			return
		}

		// Skip computed fields (fields created by eval)
		if e.computedFields[fieldLower] {
			return
		}

		op := ctx.ComparisonOp().GetText()
		value := extractValue(ctx.Value())

		cond := Condition{
			Field:     field,
			Operator:  op,
			Value:     value,
			Negated:   e.negated,
			PipeStage: e.currentStage,
			LogicalOp: e.lastLogicalOp,
		}
		e.conditions = append(e.conditions, cond)
		e.lastLogicalOp = "AND" // reset to default
	}

	// Check for IN operator: field IN (value1, value2, ...)
	if ctx.FieldName() != nil && ctx.IN() != nil && ctx.ValueList() != nil {
		field := ctx.FieldName().GetText()
		fieldLower := strings.ToLower(field)

		// Skip SPL keywords
		if isExcludedField(fieldLower) {
			return
		}

		// Skip computed fields
		if e.computedFields[fieldLower] {
			return
		}

		values := extractValueList(ctx.ValueList())

		// Create a condition for each value in the IN list
		for i, value := range values {
			logOp := e.lastLogicalOp
			if i > 0 {
				logOp = "OR"
			}
			cond := Condition{
				Field:     field,
				Operator:  "=",
				Value:     value,
				Negated:   e.negated,
				PipeStage: e.currentStage,
				LogicalOp: logOp,
			}
			e.conditions = append(e.conditions, cond)
		}
		e.lastLogicalOp = "AND"
	}
}

// EnterNotExpression tracks negation
func (e *conditionExtractor) EnterNotExpression(ctx *NotExpressionContext) {
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// ExitNotExpression resets negation
func (e *conditionExtractor) ExitNotExpression(ctx *NotExpressionContext) {
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// EnterSearchTerm handles NOT in search terms
func (e *conditionExtractor) EnterSearchTerm(ctx *SearchTermContext) {
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// ExitSearchTerm resets negation for search terms
func (e *conditionExtractor) ExitSearchTerm(ctx *SearchTermContext) {
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// EnterLogicalOp tracks the logical operator
func (e *conditionExtractor) EnterLogicalOp(ctx *LogicalOpContext) {
	if ctx.OR() != nil {
		e.lastLogicalOp = "OR"
	} else {
		e.lastLogicalOp = "AND"
	}
}

// EnterOrExpression handles OR in where clauses
func (e *conditionExtractor) EnterOrExpression(ctx *OrExpressionContext) {
	// If there are multiple andExpressions, they're connected by OR
	if len(ctx.AllAndExpression()) > 1 {
		// Mark that the next conditions will be ORed
	}
}

// extractValue gets the string value from a value context
func extractValue(ctx IValueContext) string {
	if ctx == nil {
		return ""
	}

	text := ctx.GetText()

	// Remove quotes if present
	if ctx.QUOTED_STRING() != nil {
		text = strings.Trim(text, "\"'")
	}

	return text
}

// extractValueList gets all values from a value list context
func extractValueList(ctx IValueListContext) []string {
	if ctx == nil {
		return nil
	}

	var values []string
	for _, v := range ctx.AllValue() {
		values = append(values, extractValue(v))
	}
	return values
}

// groupORConditions groups consecutive OR conditions on the same field
func groupORConditions(conditions []Condition) []Condition {
	if len(conditions) == 0 {
		return conditions
	}

	result := make([]Condition, 0, len(conditions))

	for i := 0; i < len(conditions); i++ {
		cond := conditions[i]

		// Look ahead for OR conditions on the same field
		if i+1 < len(conditions) && conditions[i+1].LogicalOp == "OR" {
			fieldLower := strings.ToLower(cond.Field)
			alternatives := []string{cond.Value}

			j := i + 1
			for j < len(conditions) {
				next := conditions[j]
				if next.LogicalOp == "OR" && strings.ToLower(next.Field) == fieldLower && next.Operator == cond.Operator {
					alternatives = append(alternatives, next.Value)
					j++
				} else {
					break
				}
			}

			if len(alternatives) > 1 {
				cond.Alternatives = alternatives
				result = append(result, cond)
				i = j - 1 // skip the grouped conditions
				continue
			}
		}

		result = append(result, cond)
	}

	return result
}

// DeduplicateConditions removes duplicate conditions, keeping the latest pipe stage
func DeduplicateConditions(conditions []Condition) []Condition {
	if len(conditions) == 0 {
		return conditions
	}

	// Group by field (case-insensitive)
	fieldConditions := make(map[string][]Condition)
	for _, cond := range conditions {
		// Skip pure wildcards
		if cond.Value == "*" {
			continue
		}
		fieldLower := strings.ToLower(cond.Field)
		fieldConditions[fieldLower] = append(fieldConditions[fieldLower], cond)
	}

	// Keep only conditions from the latest pipe stage for each field
	result := make([]Condition, 0)
	seen := make(map[string]bool)

	for _, conds := range fieldConditions {
		// Find max pipe stage
		maxStage := -1
		for _, c := range conds {
			if c.PipeStage > maxStage {
				maxStage = c.PipeStage
			}
		}

		// Keep only conditions from max stage
		for _, cond := range conds {
			if cond.PipeStage == maxStage {
				key := strings.ToLower(cond.Field) + "|" + cond.Operator + "|" + cond.Value
				if !seen[key] {
					seen[key] = true
					result = append(result, cond)
				}
			}
		}
	}

	return result
}
