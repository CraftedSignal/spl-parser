package spl

import (
	"fmt"
	"strings"
	"time"

	"github.com/antlr4-go/antlr/v4"
)

// MaxParseTime is the maximum time allowed for parsing a single query.
// Queries that exceed this are returned with an error.
var MaxParseTime = 5 * time.Second

// Condition represents a field condition extracted from an SPL query
type Condition struct {
	Field        string   `json:"field"`
	Operator     string   `json:"operator"`
	Value        string   `json:"value"`
	Negated      bool     `json:"negated"`
	PipeStage    int      `json:"pipe_stage"`
	LogicalOp    string   `json:"logical_op"`              // "AND" or "OR" connecting to previous condition
	Alternatives []string `json:"alternatives,omitempty"`  // For OR conditions on same field
	IsComputed   bool     `json:"is_computed,omitempty"`   // True if field was created by eval/rex
	SourceField  string   `json:"source_field,omitempty"`  // Original field before transformation (for computed fields)
}

// ParseResult contains all conditions extracted from the query
type ParseResult struct {
	Conditions     []Condition       `json:"conditions"`
	GroupByFields  []string          `json:"group_by_fields,omitempty"`  // Fields from stats/eventstats/streamstats BY clauses
	ComputedFields map[string]string `json:"computed_fields,omitempty"`  // Map of computed field name -> source field (from eval/rex)
	Commands       []string          `json:"commands,omitempty"`         // List of commands used in the query (stats, eventstats, etc.)
	Joins          []JoinInfo        `json:"joins,omitempty"`            // Extracted join/append info
	Errors         []string          `json:"errors,omitempty"`
}

// FieldProvenance indicates where a field originates relative to a join
type FieldProvenance string

const (
	ProvenanceMain      FieldProvenance = "main"      // Field exists in main query before join
	ProvenanceJoined    FieldProvenance = "joined"     // Field comes from the joined subsearch
	ProvenanceJoinKey   FieldProvenance = "join_key"   // Field is used as a join key (both sides)
	ProvenanceAmbiguous FieldProvenance = "ambiguous"  // Cannot determine provenance
)

// JoinInfo captures the structured decomposition of a JOIN or APPEND command
type JoinInfo struct {
	Type           string            `json:"type"`                      // "inner", "left", "outer" (default: "inner")
	JoinFields     []string          `json:"join_fields,omitempty"`     // Fields to join ON (from fieldList)
	Options        map[string]string `json:"options,omitempty"`         // All joinOption key=value pairs
	Subsearch      *ParseResult      `json:"subsearch"`                 // Recursively parsed subsearch
	PipeStage      int               `json:"pipe_stage"`                // Pipeline stage where join appears
	IsAppend       bool              `json:"is_append,omitempty"`       // True if this is an APPEND, not JOIN
	ExposedFields  []string          `json:"exposed_fields,omitempty"`  // Fields the subsearch makes available
}

// SearchScopeMetadata are fields that define WHERE to search, not WHAT to match
// These are Splunk infrastructure metadata, not part of event data
// Note: "host" is NOT included because it's a meaningful field that appears in event data
// and is commonly used in detection rules (unlike index/sourcetype/source which are routing metadata)
var SearchScopeMetadata = map[string]bool{
	"index":         true, // Which index to search
	"sourcetype":    true, // Data format type
	"source":        true, // File path of the data
	"earliest":      true, // Time range start
	"latest":        true, // Time range end
	"splunk_server": true, // Server to search
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
// Note: This excludes time-range modifiers but NOT index/sourcetype/source which provide
// useful context for rules. For test data generation filtering, use IsSearchScopeMetadata.
func isExcludedField(fieldLower string) bool {
	return fieldLower == "earliest" || fieldLower == "latest" || fieldLower == "splunk_server" ||
		splCommandKeywords[fieldLower]
}

// IsSearchScopeMetadata returns true if the field is search scope metadata
// (index, sourcetype, source, etc.) rather than event data.
// Use this to filter fields when determining what fields to include in test data.
func IsSearchScopeMetadata(field string) bool {
	return SearchScopeMetadata[strings.ToLower(field)]
}

// IsCommandKeyword returns true if the string is a SPL command keyword
func IsCommandKeyword(field string) bool {
	return splCommandKeywords[strings.ToLower(field)]
}

// conditionExtractor walks the parse tree to extract conditions
type conditionExtractor struct {
	*BaseSPLParserListener
	conditions      []Condition
	groupByFields   []string          // Fields from stats BY clauses
	computedFields  map[string]string // Fields created by eval commands: computed field -> source field
	commands        []string          // Commands used in the query (stats, eventstats, etc.)
	joins           []JoinInfo        // Extracted join info
	currentStage    int
	inSubsearch     int // depth of subsearch nesting
	inFunctionCall  int // depth of function call nesting (eval, count, etc.)
	inStatsFunction int // depth of stats function nesting (count(), sum(), etc.)
	negated         bool
	lastLogicalOp   string
	errors          []string
	tokenStream     *antlr.CommonTokenStream // Needed to extract subsearch text
	originalQuery   string                   // Original query string for text extraction
}

// errorListener collects parse errors
type errorListener struct {
	*antlr.DefaultErrorListener
	errors []string
}

func (l *errorListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, e antlr.RecognitionException) {
	l.errors = append(l.errors, msg)
}

// ExtractConditions parses an SPL query and extracts all field conditions.
// Uses a timeout (MaxParseTime) to abort queries that cause the parser to hang
// on deeply nested expressions. Recovers from panics.
func ExtractConditions(query string) *ParseResult {
	ch := make(chan *ParseResult, 1)
	go func() {
		ch <- extractConditionsInternal(query)
	}()

	select {
	case result := <-ch:
		return result
	case <-time.After(MaxParseTime):
		return &ParseResult{
			Conditions: []Condition{},
			Commands:   []string{},
			Errors:     []string{fmt.Sprintf("parser timeout: query took longer than %s to parse", MaxParseTime)},
		}
	}
}

func extractConditionsInternal(query string) (result *ParseResult) {
	defer func() {
		if r := recover(); r != nil {
			result = &ParseResult{
				Conditions: []Condition{},
				Commands:   []string{},
				Errors:     []string{fmt.Sprintf("parser panic: %v", r)},
			}
		}
	}()

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
		computedFields: make(map[string]string), // computed field -> source field
		commands:       make([]string, 0),
		joins:          make([]JoinInfo, 0),
		lastLogicalOp:  "AND", // default
		tokenStream:    stream,
		originalQuery:  query,
	}
	antlr.ParseTreeWalkerDefault.Walk(extractor, tree)

	// Combine errors
	allErrors := append(lexerErrors.errors, parserErrors.errors...)
	allErrors = append(allErrors, extractor.errors...)

	// Post-process to group OR conditions on same field
	conditions := groupORConditions(extractor.conditions)

	return &ParseResult{
		Conditions:     conditions,
		GroupByFields:  extractor.groupByFields,
		ComputedFields: extractor.computedFields,
		Commands:       extractor.commands,
		Joins:          extractor.joins,
		Errors:         allErrors,
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

// extractSubsearchText extracts the raw query text inside a subsearch's brackets
// using character positions from the original query string. We use the original
// query rather than GetTextFromTokens because the latter strips whitespace
// (WS tokens are on the HIDDEN channel).
func (e *conditionExtractor) extractSubsearchText(ctx *SubsearchContext) string {
	if ctx == nil || ctx.Query() == nil {
		return ""
	}
	queryCtx := ctx.Query()
	start := queryCtx.GetStart()
	stop := queryCtx.GetStop()
	if start == nil || stop == nil {
		return queryCtx.GetText()
	}
	startPos := start.GetStart()
	stopPos := stop.GetStop()
	if startPos >= 0 && stopPos >= startPos && stopPos < len(e.originalQuery) {
		return e.originalQuery[startPos : stopPos+1]
	}
	return queryCtx.GetText()
}

// EnterJoinCommand extracts join metadata and recursively parses the subsearch
func (e *conditionExtractor) EnterJoinCommand(ctx *JoinCommandContext) {
	e.commands = append(e.commands, "join")

	info := JoinInfo{
		Type:      "inner", // SPL default
		Options:   make(map[string]string),
		PipeStage: e.currentStage,
	}

	// Extract join options (e.g., type=left, max=1)
	for _, opt := range ctx.AllJoinOption() {
		if opt.IDENTIFIER() != nil && opt.EQ() != nil {
			key := strings.ToLower(opt.IDENTIFIER().GetText())
			var val string
			if opt.QUOTED_STRING() != nil {
				val = strings.Trim(opt.QUOTED_STRING().GetText(), "\"'")
			} else if opt.FieldName() != nil {
				val = opt.FieldName().GetText()
			} else if opt.NUMBER() != nil {
				val = opt.NUMBER().GetText()
			}
			info.Options[key] = val
			if key == "type" {
				info.Type = strings.ToLower(val)
			}
		}
	}

	// Extract join fields (the ON fields from fieldList)
	if ctx.FieldList() != nil {
		for _, foq := range ctx.FieldList().AllFieldOrQuoted() {
			if foq.FieldName() != nil {
				info.JoinFields = append(info.JoinFields, foq.FieldName().GetText())
			} else if foq.QUOTED_STRING() != nil {
				info.JoinFields = append(info.JoinFields, strings.Trim(foq.QUOTED_STRING().GetText(), "\"'"))
			}
		}
	}

	// Recursively parse the subsearch
	if ctx.Subsearch() != nil {
		subText := e.extractSubsearchText(ctx.Subsearch().(*SubsearchContext))
		if subText != "" {
			info.Subsearch = ExtractConditions(subText)
			info.ExposedFields = deriveExposedFields(info.Subsearch, info.JoinFields)
		}
	}

	e.joins = append(e.joins, info)
}

// EnterAppendCommand extracts append subsearch info
func (e *conditionExtractor) EnterAppendCommand(ctx *AppendCommandContext) {
	e.commands = append(e.commands, "append")

	info := JoinInfo{
		Type:      "append",
		IsAppend:  true,
		PipeStage: e.currentStage,
	}

	if ctx.Subsearch() != nil {
		subText := e.extractSubsearchText(ctx.Subsearch().(*SubsearchContext))
		if subText != "" {
			info.Subsearch = ExtractConditions(subText)
			info.ExposedFields = deriveExposedFields(info.Subsearch, nil)
		}
	}

	e.joins = append(e.joins, info)
}

// deriveExposedFields determines which fields a subsearch makes available
// after the join. Uses a fallback chain:
// 1. Explicit output commands (table/fields) -> exact field list
// 2. Condition fields from the subsearch
// 3. Computed fields from eval/rex in the subsearch
func deriveExposedFields(subResult *ParseResult, joinFields []string) []string {
	if subResult == nil {
		return nil
	}

	fieldSet := make(map[string]bool)

	// Check if subsearch has table/fields command — if so, those are the explicit outputs
	hasExplicitOutput := false
	for _, cmd := range subResult.Commands {
		if cmd == "table" || cmd == "fields" {
			hasExplicitOutput = true
			break
		}
	}

	if hasExplicitOutput {
		// When table/fields is present, use those as the definitive output field list
		for _, f := range subResult.GroupByFields {
			fieldSet[f] = true
		}
	} else {
		// No explicit output — fall back to condition fields and computed fields
		for _, c := range subResult.Conditions {
			if !IsSearchScopeMetadata(c.Field) {
				fieldSet[c.Field] = true
			}
		}
		for computed := range subResult.ComputedFields {
			fieldSet[computed] = true
		}
	}

	// Include join fields (they exist on both sides by definition)
	for _, f := range joinFields {
		fieldSet[f] = true
	}

	result := make([]string, 0, len(fieldSet))
	for f := range fieldSet {
		result = append(result, f)
	}
	return result
}

// ClassifyFieldProvenance determines where a field originates relative to
// the first join in the query.
// Returns ProvenanceAmbiguous if no joins exist or provenance can't be determined.
func ClassifyFieldProvenance(result *ParseResult, field string) FieldProvenance {
	if result == nil || len(result.Joins) == 0 {
		return ProvenanceAmbiguous
	}

	fieldLower := strings.ToLower(field)

	// Check join keys first (they exist on both sides)
	for _, j := range result.Joins {
		for _, jf := range j.JoinFields {
			if strings.ToLower(jf) == fieldLower {
				return ProvenanceJoinKey
			}
		}
	}

	// Check if field is in exposed fields from any join's subsearch
	for _, j := range result.Joins {
		for _, ef := range j.ExposedFields {
			if strings.ToLower(ef) == fieldLower {
				return ProvenanceJoined
			}
		}
	}

	// Check if field is in main query conditions (before any join)
	firstJoinStage := -1
	for _, j := range result.Joins {
		if firstJoinStage == -1 || j.PipeStage < firstJoinStage {
			firstJoinStage = j.PipeStage
		}
	}

	for _, c := range result.Conditions {
		if strings.ToLower(c.Field) == fieldLower && c.PipeStage < firstJoinStage {
			return ProvenanceMain
		}
	}

	// Field exists in conditions but after join — check if it's from main query scope
	// by seeing if it appears in pre-join computed fields
	if _, ok := result.ComputedFields[fieldLower]; ok {
		return ProvenanceMain
	}

	return ProvenanceAmbiguous
}

// EnterFunctionCall handles function calls.
// - For cidrmatch, match, and like: extract as conditions
// - For other functions (eval, count, sum, etc.): track depth to skip nested conditions
func (e *conditionExtractor) EnterFunctionCall(ctx *FunctionCallContext) {
	// Skip function calls inside subsearches
	if e.inSubsearch > 0 {
		e.inFunctionCall++
		return
	}

	// Check for cidrmatch(cidr, field) - extracts a CIDR match condition
	if ctx.CIDRMATCH() != nil {
		args := ctx.ArgumentList()
		if args != nil {
			allArgs := args.AllExpression()
			if len(allArgs) >= 2 {
				// First arg is CIDR, second is field
				cidr := strings.Trim(allArgs[0].GetText(), "\"'")
				field := allArgs[1].GetText()
				cond := Condition{
					Field:     field,
					Operator:  "cidrmatch",
					Value:     cidr,
					Negated:   e.negated,
					PipeStage: e.currentStage,
					LogicalOp: e.lastLogicalOp,
				}
				e.conditions = append(e.conditions, cond)
				e.lastLogicalOp = "AND"
			}
		}
		return // Don't increment inFunctionCall for these
	}

	// Check for match(field, regex) - extracts a regex match condition
	if ctx.MATCH() != nil {
		args := ctx.ArgumentList()
		if args != nil {
			allArgs := args.AllExpression()
			if len(allArgs) >= 2 {
				// First arg is field, second is regex
				field := allArgs[0].GetText()
				regex := strings.Trim(allArgs[1].GetText(), "\"'")
				cond := Condition{
					Field:     field,
					Operator:  "matches",
					Value:     regex,
					Negated:   e.negated,
					PipeStage: e.currentStage,
					LogicalOp: e.lastLogicalOp,
				}
				e.conditions = append(e.conditions, cond)
				e.lastLogicalOp = "AND"
			}
		}
		return // Don't increment inFunctionCall for these
	}

	// Check for like(field, pattern) - extracts a like pattern condition
	if ctx.LIKE() != nil {
		args := ctx.ArgumentList()
		if args != nil {
			allArgs := args.AllExpression()
			if len(allArgs) >= 2 {
				// First arg is field, second is pattern
				field := allArgs[0].GetText()
				pattern := strings.Trim(allArgs[1].GetText(), "\"'")
				// Convert SQL LIKE pattern to wildcard
				pattern = strings.ReplaceAll(pattern, "%", "*")
				pattern = strings.ReplaceAll(pattern, "_", "?")
				cond := Condition{
					Field:     field,
					Operator:  "like",
					Value:     pattern,
					Negated:   e.negated,
					PipeStage: e.currentStage,
					LogicalOp: e.lastLogicalOp,
				}
				e.conditions = append(e.conditions, cond)
				e.lastLogicalOp = "AND"
			}
		}
		return // Don't increment inFunctionCall for these
	}

	// For other function calls, track depth to skip nested conditions
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

// EnterStatsCommand extracts group-by fields from stats commands
func (e *conditionExtractor) EnterStatsCommand(ctx *StatsCommandContext) {
	e.commands = append(e.commands, "stats")
	e.extractByFields(ctx.FieldList())
}

// EnterEventstatsCommand extracts group-by fields from eventstats commands
func (e *conditionExtractor) EnterEventstatsCommand(ctx *EventstatsCommandContext) {
	e.commands = append(e.commands, "eventstats")
	e.extractByFields(ctx.FieldList())
}

// EnterStreamstatsCommand extracts group-by fields from streamstats commands
func (e *conditionExtractor) EnterStreamstatsCommand(ctx *StreamstatsCommandContext) {
	e.commands = append(e.commands, "streamstats")
	e.extractByFields(ctx.FieldList())
}

// EnterTimechartCommand extracts group-by fields from timechart commands
func (e *conditionExtractor) EnterTimechartCommand(ctx *TimechartCommandContext) {
	e.commands = append(e.commands, "timechart")
	if ctx.FieldName() != nil {
		field := ctx.FieldName().GetText()
		if !isExcludedField(strings.ToLower(field)) {
			e.groupByFields = append(e.groupByFields, field)
		}
	}
}

// EnterChartCommand extracts group-by fields from chart commands
func (e *conditionExtractor) EnterChartCommand(ctx *ChartCommandContext) {
	e.commands = append(e.commands, "chart")
	e.extractByFields(ctx.FieldList())
	// Also extract the OVER field if present
	if ctx.FieldName() != nil {
		field := ctx.FieldName().GetText()
		if !isExcludedField(strings.ToLower(field)) {
			e.groupByFields = append(e.groupByFields, field)
		}
	}
}

// extractByFields extracts field names from a FieldList context (used in BY clauses)
func (e *conditionExtractor) extractByFields(fieldList IFieldListContext) {
	if fieldList == nil {
		return
	}

	// FieldList contains FieldOrQuoted elements, each of which has a FieldName
	for _, fieldOrQuoted := range fieldList.AllFieldOrQuoted() {
		if fieldOrQuoted.FieldName() != nil {
			field := fieldOrQuoted.FieldName().GetText()
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.groupByFields = append(e.groupByFields, field)
			}
		} else if fieldOrQuoted.QUOTED_STRING() != nil {
			// Handle quoted field name
			field := fieldOrQuoted.QUOTED_STRING().GetText()
			// Remove quotes
			field = strings.Trim(field, `"'`)
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.groupByFields = append(e.groupByFields, field)
			}
		}
	}
}

// EnterDedupCommand extracts fields from dedup commands
func (e *conditionExtractor) EnterDedupCommand(ctx *DedupCommandContext) {
	e.extractByFields(ctx.FieldList())
}

// EnterFieldsCommand extracts fields from fields commands (field selection)
func (e *conditionExtractor) EnterFieldsCommand(ctx *FieldsCommandContext) {
	e.commands = append(e.commands, "fields")
	e.extractByFields(ctx.FieldList())
}

// EnterTableCommand extracts fields from table commands (display fields)
func (e *conditionExtractor) EnterTableCommand(ctx *TableCommandContext) {
	e.commands = append(e.commands, "table")
	e.extractByFields(ctx.FieldList())
}

// EnterTopCommand extracts fields from top commands
func (e *conditionExtractor) EnterTopCommand(ctx *TopCommandContext) {
	for _, fieldList := range ctx.AllFieldList() {
		e.extractByFields(fieldList)
	}
}

// EnterRareCommand extracts fields from rare commands
func (e *conditionExtractor) EnterRareCommand(ctx *RareCommandContext) {
	for _, fieldList := range ctx.AllFieldList() {
		e.extractByFields(fieldList)
	}
}

// EnterSortCommand extracts fields from sort commands
func (e *conditionExtractor) EnterSortCommand(ctx *SortCommandContext) {
	for _, sortField := range ctx.AllSortField() {
		if sortField.FieldName() != nil {
			field := sortField.FieldName().GetText()
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.groupByFields = append(e.groupByFields, field)
			}
		}
	}
}

// EnterEvalCommand tracks eval commands
func (e *conditionExtractor) EnterEvalCommand(ctx *EvalCommandContext) {
	e.commands = append(e.commands, "eval")
}

// EnterWhereCommand tracks where commands
func (e *conditionExtractor) EnterWhereCommand(ctx *WhereCommandContext) {
	e.commands = append(e.commands, "where")
}

// EnterRexCommand tracks rex commands and extracts computed fields from named capture groups
// rex field=CommandLine "(?<script>[^\s]+\.ps1)" creates computed field "script" from "CommandLine"
func (e *conditionExtractor) EnterRexCommand(ctx *RexCommandContext) {
	e.commands = append(e.commands, "rex")

	// Skip rex in subsearches
	if e.inSubsearch > 0 {
		return
	}

	// Find the source field from field=XXX option
	sourceField := "_raw" // Default source is _raw
	for _, opt := range ctx.AllRexOption() {
		if opt.IDENTIFIER() != nil && strings.ToLower(opt.IDENTIFIER().GetText()) == "field" {
			// Get the field value
			if opt.FieldName() != nil {
				sourceField = opt.FieldName().GetText()
			} else if opt.QUOTED_STRING() != nil {
				sourceField = strings.Trim(opt.QUOTED_STRING().GetText(), "\"'")
			}
			break
		}
	}

	// Get the regex pattern and extract named capture groups
	if ctx.QUOTED_STRING() != nil {
		pattern := ctx.QUOTED_STRING().GetText()
		captureGroups := extractNamedCaptureGroups(pattern)

		// Map each captured field to the source field
		for _, captured := range captureGroups {
			e.computedFields[strings.ToLower(captured)] = sourceField
		}
	}
}

// extractNamedCaptureGroups extracts named capture group names from a regex pattern
// Pattern: (?<name>...) or (?P<name>...) returns ["name", ...]
func extractNamedCaptureGroups(pattern string) []string {
	var groups []string
	// Look for (?<name> or (?P<name> patterns
	i := 0
	for i < len(pattern)-4 {
		if pattern[i] == '(' && pattern[i+1] == '?' {
			start := i + 2
			// Check for (?<name> or (?P<name>
			if start < len(pattern) && pattern[start] == '<' {
				start++ // skip '<'
			} else if start < len(pattern) && pattern[start] == 'P' && start+1 < len(pattern) && pattern[start+1] == '<' {
				start += 2 // skip 'P<'
			} else {
				i++
				continue
			}

			// Extract the group name until '>'
			end := start
			for end < len(pattern) && pattern[end] != '>' {
				end++
			}
			if end > start && end < len(pattern) {
				groups = append(groups, pattern[start:end])
			}
		}
		i++
	}
	return groups
}

// EnterEvalAssignment tracks computed fields from eval commands
func (e *conditionExtractor) EnterEvalAssignment(ctx *EvalAssignmentContext) {
	// Skip eval assignments in subsearches
	if e.inSubsearch > 0 {
		return
	}

	// Extract the field name being assigned to and try to find the source field
	if ctx.FieldName() != nil {
		computedField := ctx.FieldName().GetText()
		sourceField := ""

		// Try to extract the source field from the expression
		// Expression is typically: function(sourceField) or function(sourceField, ...)
		if ctx.Expression() != nil {
			sourceField = extractFirstFieldFromExpression(ctx.Expression())
		}

		e.computedFields[strings.ToLower(computedField)] = sourceField
	}
}

// extractFirstFieldFromExpression tries to extract the first field name from an expression
// This handles patterns like:
// - Function calls: lower(CommandLine), coalesce(field1, field2)
// - String concatenation: Process."-".CommandLine (SPL uses . for concat)
// - Simple identifiers: fieldName
func extractFirstFieldFromExpression(ctx IExpressionContext) string {
	if ctx == nil {
		return ""
	}

	text := ctx.GetText()
	if text == "" {
		return ""
	}

	// Extract all potential field names from the expression
	fields := extractFieldNamesFromText(text)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// extractFieldNamesFromText extracts all field name identifiers from expression text
// Returns field names in order of appearance, filtering out SPL keywords and literals
func extractFieldNamesFromText(text string) []string {
	var fields []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	// Track if we're inside a function name (before the opening paren)
	// We want to skip function names but include their arguments

	for i, ch := range text {
		// Handle quoted strings - skip their contents
		if (ch == '"' || ch == '\'') && (i == 0 || text[i-1] != '\\') {
			if !inQuote {
				inQuote = true
				quoteChar = ch
			} else if ch == quoteChar {
				inQuote = false
				quoteChar = 0
			}
			continue
		}

		if inQuote {
			continue
		}

		// Check if this is a valid identifier character
		isIdentChar := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_'

		if isIdentChar {
			current.WriteRune(ch)
		} else {
			// End of identifier
			if current.Len() > 0 {
				identifier := current.String()
				current.Reset()

				// Skip if it's a keyword, function name, or starts with digit
				if !isReservedWord(identifier) && !startsWithDigit(identifier) {
					// Check if this is followed by '(' - that means it's a function name
					isFunction := false
					for j := i; j < len(text); j++ {
						if text[j] == ' ' || text[j] == '\t' {
							continue
						}
						if text[j] == '(' {
							isFunction = true
						}
						break
					}

					if !isFunction {
						fields = append(fields, identifier)
					}
				}
			}
		}
	}

	// Don't forget the last identifier
	if current.Len() > 0 {
		identifier := current.String()
		if !isReservedWord(identifier) && !startsWithDigit(identifier) {
			fields = append(fields, identifier)
		}
	}

	return fields
}

// isReservedWord checks if an identifier is an SPL reserved word or function name
func isReservedWord(word string) bool {
	lower := strings.ToLower(word)
	reserved := map[string]bool{
		// SPL keywords
		"and": true, "or": true, "not": true, "as": true, "by": true,
		"true": true, "false": true, "null": true,
		// Common functions that appear in eval expressions
		"lower": true, "upper": true, "substr": true, "len": true,
		"if": true, "case": true, "coalesce": true, "nullif": true,
		"tonumber": true, "tostring": true, "typeof": true,
		"now": true, "time": true, "relative_time": true,
		"split": true, "mvappend": true, "mvcount": true, "mvindex": true,
		"replace": true, "match": true, "like": true, "cidrmatch": true,
		"isnull": true, "isnotnull": true, "isnum": true, "isstr": true,
	}
	return reserved[lower]
}

// startsWithDigit checks if a string starts with a digit (not a valid identifier start)
func startsWithDigit(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

// EnterBareWord extracts bare search terms (quoted strings used as fulltext search)
func (e *conditionExtractor) EnterBareWord(ctx *BareWordContext) {
	// Skip inside subsearches
	if e.inSubsearch > 0 {
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
	// Skip conditions inside subsearches (like join)
	if e.inSubsearch > 0 {
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

		op := ctx.ComparisonOp().GetText()
		value := extractValue(ctx.Value())

		// Check if this is a computed field and get its source field
		sourceField, isComputed := e.computedFields[fieldLower]

		cond := Condition{
			Field:       field,
			Operator:    op,
			Value:       value,
			Negated:     e.negated,
			PipeStage:   e.currentStage,
			LogicalOp:   e.lastLogicalOp,
			IsComputed:  isComputed,
			SourceField: sourceField,
		}
		e.conditions = append(e.conditions, cond)
		e.lastLogicalOp = "AND" // reset to default
	}

	// Check for IN operator: field IN (value1, value2, ...)
	// Create a single "in" condition with all values rather than expanding to multiple "=" conditions.
	// This preserves correct semantics for NOT field IN (...) which requires AND logic (not match ANY),
	// unlike expanded form which would be OR logic.
	if ctx.FieldName() != nil && ctx.IN() != nil && ctx.ValueList() != nil {
		field := ctx.FieldName().GetText()
		fieldLower := strings.ToLower(field)

		// Skip SPL keywords
		if isExcludedField(fieldLower) {
			return
		}

		// Check if this is a computed field and get its source field
		sourceField, isComputed := e.computedFields[fieldLower]

		values := extractValueList(ctx.ValueList())

		// Create a single IN condition with all values
		cond := Condition{
			Field:        field,
			Operator:     "in",
			Value:        values[0], // Primary value for backward compatibility
			Negated:      e.negated,
			PipeStage:    e.currentStage,
			LogicalOp:    e.lastLogicalOp,
			Alternatives: values, // All values in the IN list
			IsComputed:   isComputed,
			SourceField:  sourceField,
		}
		e.conditions = append(e.conditions, cond)
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

// IsStatisticalQuery checks if the parse result contains aggregation commands
// (stats, eventstats, streamstats, chart, timechart) that create computed fields
// making static analysis unreliable
func IsStatisticalQuery(result *ParseResult) bool {
	statisticalCommands := map[string]bool{
		"stats":       true,
		"eventstats":  true,
		"streamstats": true,
		"chart":       true,
		"timechart":   true,
	}
	for _, cmd := range result.Commands {
		if statisticalCommands[cmd] {
			return true
		}
	}
	return false
}

// HasUnmappedComputedFields checks if any computed field used in conditions
// could not be traced back to a source field
func HasUnmappedComputedFields(result *ParseResult) bool {
	for _, cond := range result.Conditions {
		if cond.IsComputed && cond.SourceField == "" {
			return true
		}
	}
	return false
}

// HasComplexWhereConditions checks if the query has where clauses with functions
// that can't be validated statically (match, like, cidrmatch, etc.)
func HasComplexWhereConditions(result *ParseResult) bool {
	// Check if "where" command is used
	hasWhere := false
	for _, cmd := range result.Commands {
		if cmd == "where" {
			hasWhere = true
			break
		}
	}
	if !hasWhere {
		return false
	}

	// Check for conditions with complex operators that came from function calls
	complexOperators := map[string]bool{
		"matches":   true, // from match()
		"like":      true, // from like()
		"cidrmatch": true, // from cidrmatch()
	}

	for _, cond := range result.Conditions {
		if complexOperators[cond.Operator] {
			return true
		}
		// Also check for negated conditions in where clauses
		if cond.Negated && cond.PipeStage > 0 {
			return true
		}
	}

	return false
}

// PipelineStageInfo describes a single stage in a SPL pipeline
type PipelineStageInfo struct {
	Index         int    `json:"index"`          // 0-based stage index
	CommandType   string `json:"command_type"`   // e.g. "search", "where", "eval", "stats", "generic"
	IsAggregation bool   `json:"is_aggregation"` // true for stats, eventstats, streamstats, chart, timechart, transaction, dedup, top, rare
	OriginalText  string `json:"original_text"`  // Original text of this pipeline stage from the parsed query
}

// aggregationCommands are commands that aggregate multiple events, making them
// unsuitable for single-event test validation
var aggregationCommands = map[string]bool{
	"stats": true, "eventstats": true, "streamstats": true,
	"chart": true, "timechart": true,
	"transaction": true, "dedup": true, "top": true, "rare": true,
}

// ClassifyPipelineStages parses a SPL query and returns metadata about each
// pipeline stage. This allows callers to make decisions based on stage type
// (e.g. stopping at aggregation stages) without brittle string splitting.
// Returns nil if parsing fails.
func ClassifyPipelineStages(query string) []PipelineStageInfo {
	ch := make(chan []PipelineStageInfo, 1)
	go func() {
		ch <- classifyPipelineStagesInternal(query)
	}()

	select {
	case result := <-ch:
		return result
	case <-time.After(MaxParseTime):
		return nil
	}
}

func classifyPipelineStagesInternal(query string) (result []PipelineStageInfo) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
		}
	}()

	input := antlr.NewInputStream(query)
	lexer := NewSPLLexer(input)
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := NewSPLParser(stream)
	parser.RemoveErrorListeners()

	tree := parser.Query()

	stages := tree.AllPipelineStage()
	infos := make([]PipelineStageInfo, len(stages))

	for i, stage := range stages {
		cmdType := classifyStage(stage)

		// Extract original text with whitespace from the query string using
		// ANTLR token positions, since GetText() strips whitespace.
		originalText := stage.GetText()
		startToken := stage.GetStart()
		stopToken := stage.GetStop()
		if startToken != nil && stopToken != nil {
			start := startToken.GetStart()
			stop := stopToken.GetStop()
			if start >= 0 && stop >= start && stop < len(query) {
				originalText = query[start : stop+1]
			}
		}

		infos[i] = PipelineStageInfo{
			Index:         i,
			CommandType:   cmdType,
			IsAggregation: aggregationCommands[cmdType],
			OriginalText:  originalText,
		}
	}

	return infos
}

// FirstJoinOrSubsearchStage returns the pipeline stage index of the first
// join or append command. Returns -1 if no such stage exists.
// This is useful for filtering out conditions that come from after a join,
// since those fields originate from a different index and aren't available
// in injected test data.
func FirstJoinOrSubsearchStage(query string) int {
	stages := ClassifyPipelineStages(query)
	for _, s := range stages {
		if s.CommandType == "join" || s.CommandType == "append" {
			return s.Index
		}
	}
	return -1
}

// classifyStage determines the command type of a pipeline stage by checking
// which typed child context is non-nil.
func classifyStage(stage IPipelineStageContext) string {
	if stage.SearchCommand() != nil {
		return "search"
	}
	if stage.WhereCommand() != nil {
		return "where"
	}
	if stage.EvalCommand() != nil {
		return "eval"
	}
	if stage.StatsCommand() != nil {
		return "stats"
	}
	if stage.TableCommand() != nil {
		return "table"
	}
	if stage.FieldsCommand() != nil {
		return "fields"
	}
	if stage.RenameCommand() != nil {
		return "rename"
	}
	if stage.RexCommand() != nil {
		return "rex"
	}
	if stage.DedupCommand() != nil {
		return "dedup"
	}
	if stage.SortCommand() != nil {
		return "sort"
	}
	if stage.HeadCommand() != nil {
		return "head"
	}
	if stage.TailCommand() != nil {
		return "tail"
	}
	if stage.TopCommand() != nil {
		return "top"
	}
	if stage.RareCommand() != nil {
		return "rare"
	}
	if stage.LookupCommand() != nil {
		return "lookup"
	}
	if stage.JoinCommand() != nil {
		return "join"
	}
	if stage.AppendCommand() != nil {
		return "append"
	}
	if stage.TransactionCommand() != nil {
		return "transaction"
	}
	if stage.SpathCommand() != nil {
		return "spath"
	}
	if stage.EventstatsCommand() != nil {
		return "eventstats"
	}
	if stage.StreamstatsCommand() != nil {
		return "streamstats"
	}
	if stage.TimechartCommand() != nil {
		return "timechart"
	}
	if stage.ChartCommand() != nil {
		return "chart"
	}
	if stage.FillnullCommand() != nil {
		return "fillnull"
	}
	if stage.MakemvCommand() != nil {
		return "makemv"
	}
	if stage.MvexpandCommand() != nil {
		return "mvexpand"
	}
	if stage.FormatCommand() != nil {
		return "format"
	}
	if stage.ConvertCommand() != nil {
		return "convert"
	}
	if stage.BucketCommand() != nil {
		return "bucket"
	}
	if stage.RestCommand() != nil {
		return "rest"
	}
	if stage.GenericCommand() != nil {
		return "generic"
	}
	return "unknown"
}

// GetEventTypeFromConditions detects Windows Event types based on EventCode/EventID conditions
// Returns event type strings like "windows_4688", "sysmon_1", etc.
func GetEventTypeFromConditions(result *ParseResult) string {
	var eventCode string
	var hasSysmon bool

	for _, cond := range result.Conditions {
		fieldLower := strings.ToLower(cond.Field)

		// Check for EventCode or EventID
		if fieldLower == "eventcode" || fieldLower == "eventid" {
			eventCode = cond.Value
		}

		// Check for sourcetype containing sysmon
		if fieldLower == "sourcetype" && strings.Contains(strings.ToLower(cond.Value), "sysmon") {
			hasSysmon = true
		}
	}

	if eventCode == "" {
		return ""
	}

	// Map event codes to event types
	if hasSysmon {
		switch eventCode {
		case "1":
			return "sysmon_1"
		case "3":
			return "sysmon_3"
		}
	}

	switch eventCode {
	case "4688":
		return "windows_4688"
	case "4624":
		return "windows_4624"
	case "4625":
		return "windows_4625"
	}

	return ""
}
