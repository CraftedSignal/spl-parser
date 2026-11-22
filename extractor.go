package spl

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/antlr4-go/antlr/v4"
)

// MaxParseTime caps cooperative parsing checks. ANTLR may continue inside a
// prediction between checks; this is not a hard wall-clock or memory bound.
var MaxParseTime = 5 * time.Second

// Condition represents a field condition extracted from an SPL query
type Condition struct {
	StageID             string           `json:"stage_id,omitempty"`
	InputScopeID        string           `json:"input_scope_id,omitempty"`
	FieldBinding        *FieldBindingRef `json:"field_binding,omitempty"`
	pendingFieldBinding pendingFieldBinding
	Field               string           `json:"field"`
	Context             PredicateContext `json:"context,omitempty"`
	LHS                 *Operand         `json:"lhs,omitempty"`
	RHS                 *Operand         `json:"rhs,omitempty"`
	Operator            string           `json:"operator"`
	Value               string           `json:"value"`
	ValueReference      string           `json:"value_reference,omitempty"` // Field referenced by a where comparison
	Negated             bool             `json:"negated"`
	PipeStage           int              `json:"pipe_stage"`
	LogicalOp           string           `json:"logical_op"`             // "AND" or "OR" connecting to previous condition
	Alternatives        []string         `json:"alternatives,omitempty"` // For OR conditions on same field
	IsComputed          bool             `json:"is_computed,omitempty"`  // True if field was created by eval/rex
	SourceField         string           `json:"source_field,omitempty"` // Original field before transformation (for computed fields)
}

// PredicateContext identifies the SPL syntax that assigned operand roles.
type PredicateContext string

const (
	PredicateSearch   PredicateContext = "search"
	PredicateEval     PredicateContext = "eval"
	PredicateResource PredicateContext = "resource"
)

// OperandKind describes syntax and role, not an event column's runtime type.
type OperandKind string

const (
	OperandString OperandKind = "string"
	OperandNumber OperandKind = "number"
	OperandField  OperandKind = "field"
	OperandBare   OperandKind = "bare"
	OperandList   OperandKind = "list"
)

type Operand struct {
	FieldBinding        *FieldBindingRef `json:"field_binding,omitempty"`
	pendingFieldBinding pendingFieldBinding
	Kind                OperandKind `json:"kind"`
	Source              string      `json:"source,omitempty"`
	Value               string      `json:"value,omitempty"`
	Items               []Operand   `json:"items,omitempty"`
}

// ScalarPredicate retains a complete predicate whose semantics are opaque here.
type ScalarPredicate struct {
	StageID      string           `json:"stage_id,omitempty"`
	InputScopeID string           `json:"input_scope_id,omitempty"`
	Expression   string           `json:"expression"`
	Context      PredicateContext `json:"context"`
	Negated      bool             `json:"negated,omitempty"`
	PipeStage    int              `json:"pipe_stage"`
}

// ParseResult contains all conditions extracted from the query
type ParseResult struct {
	Dataflow             *QueryDataflow       `json:"dataflow,omitempty"`
	Conditions           []Condition          `json:"conditions"`
	Expression           *ConditionExpression `json:"expression,omitempty"`
	ExpressionIncomplete bool                 `json:"expression_incomplete,omitempty"`
	Warnings             []string             `json:"warnings,omitempty"`
	GroupByFields        []string             `json:"group_by_fields,omitempty"` // Fields from stats/eventstats/streamstats BY clauses
	Aggregations         []Aggregation        `json:"aggregations,omitempty"`    // Aggregations computed by stats-like commands, in query order
	ComputedFields       map[string]string    `json:"computed_fields,omitempty"` // Map of computed field name -> source field (from eval/rex)
	FieldAliases         map[string]string    `json:"field_aliases,omitempty"`   // Map of new name -> original name (from rename)
	Commands             []string             `json:"commands,omitempty"`        // List of commands used in the query (stats, eventstats, etc.)
	Joins                []JoinInfo           `json:"joins,omitempty"`           // Extracted join/append info
	Subsearches          []*ParseResult       `json:"subsearches,omitempty"`     // Recursively parsed direct subsearches
	Errors               []string             `json:"errors,omitempty"`
}

// Aggregation is one stats-like aggregation and the column it outputs.
// Conditions with a higher PipeStage filter the aggregated results.
type Aggregation struct {
	StageID   string `json:"stage_id,omitempty"`
	BindingID string `json:"binding_id,omitempty"`
	Command   string `json:"command"`         // stats, eventstats, streamstats, chart, timechart, tstats, mstats, top or rare
	Function  string `json:"function"`        // Lower-case aggregation function, e.g. count, dc, sum
	Field     string `json:"field,omitempty"` // Aggregated field when the argument is a plain field
	Column    string `json:"column"`          // Output column: the alias, or Splunk's default name such as count or dc(src)
	PipeStage int    `json:"pipe_stage"`      // Pipeline stage of the aggregating command
}

// FieldProvenance indicates where a field originates relative to a join
type FieldProvenance string

const (
	ProvenanceMain      FieldProvenance = "main"      // Field exists in main query before join
	ProvenanceJoined    FieldProvenance = "joined"    // Field comes from the joined subsearch
	ProvenanceJoinKey   FieldProvenance = "join_key"  // Field is used as a join key (both sides)
	ProvenanceAmbiguous FieldProvenance = "ambiguous" // Cannot determine provenance
)

// JoinInfo captures the structured decomposition of a JOIN or APPEND command
type JoinInfo struct {
	Type          string            `json:"type"`                     // "inner", "left", "outer" (default: "inner")
	JoinFields    []string          `json:"join_fields,omitempty"`    // Fields to join ON (from fieldList)
	Options       map[string]string `json:"options,omitempty"`        // All joinOption key=value pairs
	Subsearch     *ParseResult      `json:"subsearch"`                // Recursively parsed subsearch
	PipeStage     int               `json:"pipe_stage"`               // Pipeline stage where join appears
	IsAppend      bool              `json:"is_append,omitempty"`      // True if this is an APPEND, not JOIN
	ExposedFields []string          `json:"exposed_fields,omitempty"` // Fields the subsearch makes available, sorted
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

// statsColumnNames are the command keywords that name a stats output column
// when the function has no alias.
var statsColumnNames = map[string]bool{
	"count": true, "sum": true, "avg": true, "min": true, "max": true,
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
	budget *parseBudget
	*BaseSPLParserListener
	conditions        []Condition
	conditionSources  conditionSourceBindings
	scalarRoots       map[antlr.Tree]bool
	scalarDepth       int
	groupByFields     []string          // Fields from stats BY clauses
	aggregations      []Aggregation     // Aggregations computed by stats-like commands
	computedFields    map[string]string // Fields created by eval commands: computed field -> source field
	fieldAliases      map[string]string // Rename mappings: new name -> original name
	commands          []string          // Commands used in the query (stats, eventstats, etc.)
	joins             []JoinInfo        // Extracted join info
	subsearches       []*ParseResult    // Direct subsearch parse results
	currentStage      int               // PipeStage of the owned stage being walked
	nextStage         int               // advanced by every stage, subsearch stages included
	stageNumbers      map[*PipelineStageContext]int
	subsearchIndices  map[*SubsearchContext]int
	dataflowOwners    []dataflowOwnerFrame
	ownerByContext    map[antlr.ParserRuleContext]int
	canonicalChildren map[antlr.ParserRuleContext]string
	inSubsearch       int // depth of subsearch nesting
	inFunctionCall    int // depth of function call nesting (eval, count, etc.)
	inStatsFunction   int // depth of stats function nesting (count(), sum(), etc.)
	inEvalCommand     int // depth of eval command expressions; assignments compute fields, they do not filter events
	inWhereCommand    int // depth of where command predicates
	negated           bool
	lastLogicalOp     string
	errors            []string
	tokenStream       *antlr.CommonTokenStream // Needed to extract subsearch text
	originalQuery     string                   // Original query string for text extraction
	sourceMap         splSourceMap
}

// errorListener collects parse errors
type errorListener struct {
	*antlr.DefaultErrorListener
	errors                        []string
	budget                        *parseBudget
	truncatedNativeWherePredicate bool
}

func (l *errorListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, e antlr.RecognitionException) {
	if l.budget != nil {
		l.budget.step()
		if len(l.errors) >= 32 {
			l.budget.reject("syntax error count")
		}
		if len(msg) > 1024 {
			msg = msg[:1024]
		}
	}
	l.errors = append(l.errors, msg)
	if parser, ok := recognizer.(*antlr.BaseParser); ok && nativeWhereBoundaryError(parser, offendingSymbol, l.budget) {
		l.truncatedNativeWherePredicate = true
	}
}

// hasUnparsedTail reports whether tokens follow the parsed query. The root rule
// reports them as a syntax error and skips them, so the stages they hold are
// missing from the tree.
func hasUnparsedTail(root IRootContext) bool {
	for _, child := range root.GetChildren() {
		if _, ok := child.(antlr.ErrorNode); ok {
			return true
		}
	}
	return false
}

// nativeWhereBoundaryError uses the existing parser's rule stack and offending
// token to distinguish a missing where operand/group from portable syntax.
func nativeWhereBoundaryError(parser *antlr.BaseParser, offendingSymbol interface{}, budget *parseBudget) bool {
	token, ok := offendingSymbol.(antlr.Token)
	if !ok {
		return false
	}
	switch token.GetTokenType() {
	case antlr.TokenEOF, SPLParserPIPE, SPLParserRBRACKET, SPLParserRPAREN:
	default:
		return false
	}
	insideExpression := false
	for rule := parser.GetParserRuleContext(); rule != nil; {
		if budget != nil {
			budget.step()
		}
		switch rule.(type) {
		case *ExpressionContext, *ComparisonExpressionContext, *PrimaryExpressionContext,
			*AdditiveExpressionContext, *MultiplicativeExpressionContext:
			insideExpression = true
		case *WhereCommandContext:
			return insideExpression
		}
		parent, ok := rule.GetParent().(antlr.ParserRuleContext)
		if !ok {
			break
		}
		rule = parent
	}
	return false
}

func normalizeSPLQuery(query string) string {
	normalized := query
	normalized = normalizeSPLUnicodeWhitespace(normalized)
	normalized = stripSPLLineContinuations(normalized)
	normalized = stripSPLBacktickComments(normalized)
	normalized = normalizeSPLDoubleEquals(normalized)
	normalized = normalizeSPLBangFunctions(normalized)
	normalized = normalizeSPLAdjacentWildcardPredicates(normalized)
	normalized = normalizeSPLInputlookupKeywordOptions(normalized)
	return normalized
}

func stripSPLLineContinuations(query string) string {
	var b strings.Builder
	b.Grow(len(query))
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(query); i++ {
		c := query[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			b.WriteByte(c)
			continue
		}
		if inString {
			b.WriteByte(c)
			if c == stringChar && !isEscapedByte(query, i) {
				inString = false
			}
			continue
		}
		if c == '\\' {
			j := i + 1
			for j < len(query) && (query[j] == ' ' || query[j] == '\t') {
				j++
			}
			if j < len(query) && (query[j] == '\n' || query[j] == '\r') {
				i = j - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func stripSPLBacktickComments(query string) string {
	var b strings.Builder
	b.Grow(len(query))
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(query); {
		c := query[i]
		if !inString && strings.HasPrefix(query[i:], "```") {
			end := strings.Index(query[i+3:], "```")
			if end >= 0 {
				i += 3 + end + 3
			} else {
				i += 3
				for i < len(query) && query[i] != '\n' && query[i] != '\r' {
					i++
				}
			}
			b.WriteByte(' ')
			continue
		}
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			b.WriteByte(c)
			i++
			continue
		}
		if inString {
			b.WriteByte(c)
			if c == stringChar && !isEscapedByte(query, i) {
				inString = false
			}
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func normalizeSPLDoubleEquals(query string) string {
	var b strings.Builder
	b.Grow(len(query))
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(query); i++ {
		c := query[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			b.WriteByte(c)
			continue
		}
		if inString {
			b.WriteByte(c)
			if c == stringChar && !isEscapedByte(query, i) {
				inString = false
			}
			continue
		}
		if c == '=' && i+1 < len(query) && query[i+1] == '=' {
			b.WriteByte('=')
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func normalizeSPLBangFunctions(query string) string {
	replacements := []string{
		"!isnull(", "NOT isnull(",
		"!isnotnull(", "NOT isnotnull(",
		"!match(", "NOT match(",
		"!like(", "NOT like(",
		"!cidrmatch(", "NOT cidrmatch(",
	}
	return replaceOutsideSPLStrings(query, replacements...)
}

var inputlookupAppendOptionPattern = regexp.MustCompile(`(?i)(\binputlookup\b(?:(?:\s+[A-Za-z_][A-Za-z0-9_]*\s*=\s*[^\s|]+)*)\s+)append(\s*=)`)

func normalizeSPLInputlookupKeywordOptions(query string) string {
	return replaceOutsideSPLStringsPattern(query, inputlookupAppendOptionPattern, `${1}append_option${2}`)
}

func replaceOutsideSPLStringsPattern(query string, pattern *regexp.Regexp, replacement string) string {
	var b strings.Builder
	b.Grow(len(query))
	inString := false
	stringChar := byte(0)
	tokenStart := 0
	flushOutside := func(end int) {
		if tokenStart < end {
			b.WriteString(pattern.ReplaceAllString(query[tokenStart:end], replacement))
		}
	}

	for i := 0; i < len(query); i++ {
		c := query[i]
		if !inString && (c == '"' || c == '\'') {
			flushOutside(i)
			inString = true
			stringChar = c
			tokenStart = i
			continue
		}
		if inString && c == stringChar && !isEscapedByte(query, i) {
			b.WriteString(query[tokenStart : i+1])
			inString = false
			tokenStart = i + 1
		}
	}
	if inString {
		b.WriteString(query[tokenStart:])
	} else {
		flushOutside(len(query))
	}
	return b.String()
}

func normalizeSPLUnicodeWhitespace(query string) string {
	var b strings.Builder
	b.Grow(len(query))
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(query); {
		c := query[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			b.WriteByte(c)
			i++
			continue
		}
		if inString {
			b.WriteByte(c)
			if c == stringChar && !isEscapedByte(query, i) {
				inString = false
			}
			i++
			continue
		}
		if c < utf8.RuneSelf {
			b.WriteByte(c)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(query[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteByte(c)
			i++
			continue
		}
		switch {
		case r == 0x200b || r == 0x200c || r == 0x200d || r == 0xfeff:
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

func replaceOutsideSPLStrings(query string, replacements ...string) string {
	replacer := strings.NewReplacer(replacements...)
	var b strings.Builder
	b.Grow(len(query))
	inString := false
	stringChar := byte(0)
	tokenStart := 0

	flushOutside := func(end int) {
		if tokenStart < end {
			b.WriteString(replacer.Replace(query[tokenStart:end]))
		}
	}

	for i := 0; i < len(query); i++ {
		c := query[i]
		if !inString && (c == '"' || c == '\'') {
			flushOutside(i)
			inString = true
			stringChar = c
			tokenStart = i
			continue
		}
		if inString && c == stringChar && !isEscapedByte(query, i) {
			b.WriteString(query[tokenStart : i+1])
			inString = false
			tokenStart = i + 1
		}
	}
	if inString {
		b.WriteString(query[tokenStart:])
	} else {
		flushOutside(len(query))
	}
	return b.String()
}

func isEscapedByte(s string, idx int) bool {
	backslashes := 0
	for i := idx - 1; i >= 0 && s[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

var (
	portableInConditionPattern         = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT\b|!)\s*)?((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s+IN(?::[A-Za-z]+)?\s*\(([^)]*)\)`)
	portableFunctionInConditionPattern = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT\b|!)\s*)?(?:lower|upper)\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)\s+IN(?::[A-Za-z]+)?\s*\(([^)]*)\)`)
	portableComparisonPattern          = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT\b|!)\s*)?((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*(contains|starts_with|matches|!=|>=|<=|==|=|>|<)\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:\([^)]*\))|(?:[^\s),\]]+))`)
	portableFunctionComparisonPattern  = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT\b|!)\s*)?(?:lower|upper)\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)\s*(contains|starts_with|matches|!=|>=|<=|==|=|>|<)\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:\([^)]*\))|(?:[^\s),\]]+))`)
	portableNetIPSubnetPattern         = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT\b|!)\s*)?net_ipsubnet\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*,\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:[^\s),\]]+))\s*\)`)
	portableCidrMatchPattern           = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT\b|!)\s*)?cidrmatch\s*\(\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:[^\s),\]]+))\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)`)
	portableNullFunctionPattern        = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT\b|!)\s*)?(isnotnull|isnull)\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)`)
	quotedOptionValuePattern           = `(?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')`
)

func extractPortableSPLConditions(query string, budget *parseBudget) []Condition {
	budget.step()

	normalized := normalizeSPLQuery(query)
	conditions := extractPortableSPLConditionsDepth(normalized, 0, budget)
	if len(conditions) > 0 {
		return conditions
	}
	return nil
}

func extractPortableSPLConditionsDepth(normalized string, depth int, budget *parseBudget) []Condition {
	defer budget.enterInterpretation(normalized)()
	budget.step()

	if budget != nil && depth > budget.limits.maxNestingDepth {
		budget.reject("embedded nesting depth")
	}

	var conditions []Condition
	// After a leading pipe the first segment is a command, not the base search.
	leadingPipe := strings.HasPrefix(strings.TrimSpace(normalized), "|")
	for stage, segment := range splitSPLPipelineSegments(normalized) {
		budget.step()

		text, ok := portablePredicateSegment(segment, stage == 0 && !leadingPipe)
		if !ok {
			continue
		}
		conditions = budget.appendConditions(conditions, extractPortablePredicatesFromText(text, stage, budget)...)
	}
	conditions = budget.appendConditions(conditions, extractLDAPSearchConditions(normalized, budget)...)
	conditions = budget.appendConditions(conditions, extractDatasetPowerQueryConditions(normalized, budget)...)
	conditions = budget.appendConditions(conditions, extractQuotedSearchOptionConditions(normalized, depth, budget)...)
	for _, segment := range splitSPLPipelineSegments(normalized) {
		budget.step()

		// A join or append subsearch filters its own rows, as JoinInfo records.
		if trimmed := strings.TrimSpace(segment); hasWordAt(trimmed, 0, "join") || hasWordAt(trimmed, 0, "append") {
			continue
		}
		conditions = budget.appendConditions(conditions, extractSubsearchConditions(segment, depth, budget)...)
	}
	return deduplicateConditions(conditions, budget)
}

func splitSPLPipelineSegments(query string) []string {
	var segments []string
	start := 0
	inString := false
	stringChar := byte(0)
	bracketDepth := 0
	parenDepth := 0

	for i := 0; i < len(query); i++ {
		c := query[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if inString {
			if c == stringChar && !isEscapedByte(query, i) {
				inString = false
			}
			continue
		}
		switch c {
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '|':
			if bracketDepth == 0 && parenDepth == 0 {
				// A leading pipe opens the first command, so the blank text
				// before it is no stage, as in the parser's numbering.
				if len(segments) > 0 || strings.TrimSpace(query[start:i]) != "" {
					segments = append(segments, query[start:i])
				}
				start = i + 1
			}
		}
	}
	segments = append(segments, query[start:])
	return segments
}

func portablePredicateSegment(segment string, first bool) (string, bool) {
	trimmed := strings.TrimSpace(segment)
	lower := strings.ToLower(trimmed)
	switch {
	case trimmed == "":
		return "", false
	case first:
		return trimmed, true
	case strings.HasPrefix(lower, "search "):
		return strings.TrimSpace(trimmed[len("search "):]), true
	case strings.HasPrefix(lower, "where "):
		return strings.TrimSpace(trimmed[len("where "):]), true
	case strings.HasPrefix(lower, "tstats "):
		if idx := strings.Index(strings.ToLower(trimmed), " where "); idx >= 0 {
			return strings.TrimSpace(trimmed[idx+len(" where "):]), true
		}
	}
	return "", false
}

func extractPortablePredicatesFromText(text string, stage int, budget *parseBudget) []Condition {
	budget.step()

	text = stripBracketedSubsearches(text)
	var matches []portableConditionMatch
	seenSpans := make([][2]int, 0)
	for _, match := range portableInConditionPattern.FindAllStringSubmatchIndex(text, -1) {
		budget.step()

		portableMatch := portableInMatch(text, match, stage)
		budget.outputSize(len(matches) + 1)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}
	for _, match := range portableFunctionInConditionPattern.FindAllStringSubmatchIndex(text, -1) {
		budget.step()

		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		portableMatch := portableInMatch(text, match, stage)
		budget.outputSize(len(matches) + 1)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}
	for _, match := range portableNetIPSubnetPattern.FindAllStringSubmatchIndex(text, -1) {
		budget.step()

		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		field := normalizePortableField(text[match[6]:match[7]])
		if shouldSkipPortableField(field) {
			continue
		}
		value := normalizePortableValue(text[match[8]:match[9]])
		if value == "" {
			continue
		}
		budget.outputSize(len(matches) + 1)
		matches = append(matches, portableConditionMatch{
			start: match[0],
			end:   match[1],
			cond: Condition{
				Field:     field,
				Operator:  "cidrmatch",
				Value:     value,
				Negated:   portableNegatedAt(text, match[0], match[4] >= 0),
				PipeStage: stage,
			},
		})
		seenSpans = append(seenSpans, [2]int{match[0], match[1]})
	}
	for _, match := range portableCidrMatchPattern.FindAllStringSubmatchIndex(text, -1) {
		budget.step()

		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		value := normalizePortableValue(text[match[6]:match[7]])
		field := normalizePortableField(text[match[8]:match[9]])
		if shouldSkipPortableField(field) || value == "" {
			continue
		}
		budget.outputSize(len(matches) + 1)
		matches = append(matches, portableConditionMatch{
			start: match[0],
			end:   match[1],
			cond: Condition{
				Field:     field,
				Operator:  "cidrmatch",
				Value:     value,
				Negated:   portableNegatedAt(text, match[0], match[4] >= 0),
				PipeStage: stage,
			},
		})
		seenSpans = append(seenSpans, [2]int{match[0], match[1]})
	}
	for _, match := range portableNullFunctionPattern.FindAllStringSubmatchIndex(text, -1) {
		budget.step()

		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		field := normalizePortableField(text[match[8]:match[9]])
		if shouldSkipPortableField(field) {
			continue
		}
		budget.outputSize(len(matches) + 1)
		matches = append(matches, portableConditionMatch{
			start: match[0],
			end:   match[1],
			cond: Condition{
				Field:     field,
				Operator:  strings.ToLower(text[match[6]:match[7]]),
				Negated:   portableNegatedAt(text, match[0], match[4] >= 0),
				PipeStage: stage,
			},
		})
		seenSpans = append(seenSpans, [2]int{match[0], match[1]})
	}
	for _, match := range portableComparisonPattern.FindAllStringSubmatchIndex(text, -1) {
		budget.step()

		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		portableMatch := portableComparisonMatch(text, match, stage)
		budget.outputSize(len(matches) + 1)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}
	for _, match := range portableFunctionComparisonPattern.FindAllStringSubmatchIndex(text, -1) {
		budget.step()

		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		portableMatch := portableComparisonMatch(text, match, stage)
		budget.outputSize(len(matches) + 1)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].start < matches[j].start
	})

	budget.outputSize(len(matches))
	conditions := make([]Condition, 0, len(matches))
	for i, match := range matches {
		budget.step()

		if match.cond.Field == "" || (match.cond.Value == "" && match.cond.Operator != "isnotnull" && match.cond.Operator != "isnull") {
			continue
		}
		if i == 0 {
			match.cond.LogicalOp = "AND"
		} else {
			match.cond.LogicalOp = portableLogicalOpBefore(text, match.start)
		}
		conditions = budget.appendConditions(conditions, match.cond)
	}
	return conditions
}

func stripBracketedSubsearches(text string) string {
	out := []byte(text)
	depth := 0
	inString := false
	stringChar := byte(0)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if depth == 0 && !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if depth == 0 && inString {
			if c == stringChar && !isEscapedByte(text, i) {
				inString = false
			}
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '[':
			depth++
			out[i] = ' '
		case ']':
			if depth > 0 {
				depth--
				out[i] = ' '
			}
		default:
			if depth > 0 {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

func scanBalancedParenthesesEnd(text string, start int) int {
	const open, close byte = '(', ')'
	if start < 0 || start >= len(text) || text[start] != open {
		return -1
	}
	depth := 0
	inString := false
	stringChar := byte(0)
	for i := start; i < len(text); i++ {
		c := text[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if inString {
			if c == stringChar && !isEscapedByte(text, i) {
				inString = false
			}
			continue
		}
		switch c {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

type portableConditionMatch struct {
	start int
	end   int
	cond  Condition
}

func portableInMatch(text string, match []int, stage int) portableConditionMatch {
	field := normalizePortableField(text[match[6]:match[7]])
	if shouldSkipPortableField(field) {
		return portableConditionMatch{start: match[0], end: match[1]}
	}
	listEnd := match[9]
	matchEnd := match[1]
	if open := match[8] - 1; open >= 0 && text[open] == '(' {
		if balancedEnd := scanBalancedParenthesesEnd(text, open); balancedEnd > listEnd {
			listEnd = balancedEnd - 1
			matchEnd = balancedEnd
		}
	}
	values := splitPortableValueList(text[match[8]:listEnd])
	if len(values) == 0 {
		return portableConditionMatch{start: match[0], end: match[1]}
	}
	return portableConditionMatch{
		start: match[0],
		end:   matchEnd,
		cond: Condition{
			Field:        field,
			Operator:     "in",
			Value:        values[0],
			Alternatives: values,
			Negated:      portableNegatedAt(text, match[0], match[4] >= 0),
			PipeStage:    stage,
		},
	}
}

func portableComparisonMatch(text string, match []int, stage int) portableConditionMatch {
	field := normalizePortableField(text[match[6]:match[7]])
	if shouldSkipPortableField(field) {
		return portableConditionMatch{start: match[0], end: match[1]}
	}
	op := strings.ToLower(text[match[8]:match[9]])
	if match[10] >= 0 && match[10] <= len(text) && strings.Contains(text[match[8]:match[10]], "==") {
		op = "=="
	}
	valueEnd := match[11]
	matchEnd := match[1]
	if match[10] >= 0 && match[10] < len(text) && text[match[10]] == '(' {
		if balancedEnd := scanBalancedParenthesesEnd(text, match[10]); balancedEnd > valueEnd {
			valueEnd = balancedEnd
			matchEnd = balancedEnd
		}
	}
	rawValue := strings.TrimSpace(text[match[10]:valueEnd])
	values := []string{normalizePortableValue(rawValue)}
	if strings.HasPrefix(rawValue, "(") && strings.HasSuffix(rawValue, ")") {
		values = splitPortableValueList(strings.TrimSpace(rawValue[1 : len(rawValue)-1]))
	}
	if len(values) == 0 || values[0] == "" {
		return portableConditionMatch{start: match[0], end: match[1]}
	}
	return portableConditionMatch{
		start: match[0],
		end:   matchEnd,
		cond: Condition{
			Field:        field,
			Operator:     op,
			Value:        values[0],
			Alternatives: values,
			Negated:      portableNegatedAt(text, match[0], match[4] >= 0),
			PipeStage:    stage,
		},
	}
}

func spanOverlaps(start, end int, spans [][2]int) bool {
	for _, span := range spans {
		if start < span[1] && end > span[0] {
			return true
		}
	}
	return false
}

func portableLogicalOpBefore(text string, start int) string {
	if start > len(text) {
		start = len(text)
	}
	prefix := strings.TrimSpace(text[:start])
	prefix = strings.TrimRight(prefix, "(")
	prefix = strings.TrimSpace(prefix)
	if strings.HasSuffix(strings.ToUpper(prefix), " OR") || strings.EqualFold(prefix, "OR") {
		return "OR"
	}
	return "AND"
}

func portableNegatedAt(text string, start int, explicit bool) bool {
	if start > len(text) {
		start = len(text)
	}
	scanEnd := start
	if scanEnd < len(text) && text[scanEnd] == '(' {
		scanEnd++
	}
	currentNegated := false
	pendingNegation := false
	stack := make([]bool, 0)
	inString := false
	stringChar := byte(0)

	for i := 0; i < scanEnd; i++ {
		c := text[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if inString {
			if c == stringChar && !isEscapedByte(text, i) {
				inString = false
			}
			continue
		}

		if c == '!' && (i+1 >= len(text) || text[i+1] != '=') {
			pendingNegation = true
			continue
		}
		if hasWordAt(text, i, "NOT") {
			pendingNegation = true
			i += len("NOT") - 1
			continue
		}
		switch c {
		case '(':
			groupNegated := currentNegated
			if pendingNegation {
				groupNegated = !groupNegated
			}
			stack = append(stack, groupNegated)
			currentNegated = groupNegated
			pendingNegation = false
		case ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if len(stack) > 0 {
				currentNegated = stack[len(stack)-1]
			} else {
				currentNegated = false
			}
			pendingNegation = false
		default:
			if !unicode.IsSpace(rune(c)) {
				pendingNegation = false
			}
		}
	}

	if explicit {
		return !currentNegated
	}
	return currentNegated
}

func hasWordAt(text string, pos int, word string) bool {
	if pos < 0 || pos+len(word) > len(text) {
		return false
	}
	if !strings.EqualFold(text[pos:pos+len(word)], word) {
		return false
	}
	if pos > 0 && isPortableWordByte(text[pos-1]) {
		return false
	}
	if pos+len(word) < len(text) && isPortableWordByte(text[pos+len(word)]) {
		return false
	}
	return true
}

func isPortableWordByte(c byte) bool {
	return c == '_' || c == '.' || c == '-' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

func extractQuotedSearchOptionConditions(query string, depth int, budget *parseBudget) []Condition {
	budget.step()

	var conditions []Condition
	for _, literal := range extractQuotedSearchValues(query) {
		budget.step()

		value := decodeSPLStringLiteral(literal)
		trimmed := strings.TrimSpace(value)
		if !looksLikeEmbeddedSearch(trimmed) {
			continue
		}
		conditions = budget.appendConditions(conditions, extractPortableSPLConditionsDepth(normalizeSPLQuery(trimmed), depth+1, budget)...)
	}
	return conditions
}

func looksLikeEmbeddedSearch(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "|") ||
		strings.HasPrefix(lower, "search ") ||
		strings.HasPrefix(lower, "ldapsearch ") ||
		strings.HasPrefix(lower, "dataset ") ||
		strings.Contains(lower, "| dataset ") ||
		strings.Contains(lower, " method=powerquery ")
}

func extractDatasetPowerQueryConditions(query string, budget *parseBudget) []Condition {
	budget.step()

	var conditions []Condition
	for stage, segment := range splitSPLPipelineSegments(query) {
		budget.step()

		lower := strings.ToLower(segment)
		if !strings.Contains(lower, "dataset") || !strings.Contains(lower, "method=powerquery") {
			continue
		}
		for _, literal := range extractQuotedSearchValues(segment) {
			budget.step()

			value := normalizeEmbeddedPredicateText(decodeSPLStringLiteral(literal))
			conditions = budget.appendConditions(conditions, extractEmbeddedPredicateConditions(value, stage, budget)...)
		}
	}
	return conditions
}

func extractEmbeddedPredicateConditions(text string, stage int, budget *parseBudget) []Condition {
	defer budget.enterInterpretation(text)()
	budget.step()

	var conditions []Condition
	segments := splitSPLPipelineSegments(text)
	leadingPipe := strings.HasPrefix(strings.TrimSpace(text), "|")
	for idx, segment := range segments {
		budget.step()

		if predicateText, ok := embeddedPredicateSegment(segment, idx == 0 && !leadingPipe); ok {
			conditions = budget.appendConditions(conditions, extractPortablePredicatesFromText(predicateText, stage, budget)...)
		}
	}
	if len(conditions) == 0 {
		conditions = budget.appendConditions(conditions, extractParenthesizedPredicateBlockConditions(text, stage, budget)...)
	}
	if len(conditions) == 0 && looksLikePortablePredicate(text) {
		conditions = budget.appendConditions(conditions, extractPortablePredicatesFromText(text, stage, budget)...)
	}
	return deduplicateConditions(conditions, budget)
}

func extractParenthesizedPredicateBlockConditions(text string, stage int, budget *parseBudget) []Condition {
	budget.step()

	var conditions []Condition
	for _, block := range splitParenthesizedBlocks(text) {
		budget.step()

		if !looksLikePortablePredicate(block) {
			continue
		}
		conditions = budget.appendConditions(conditions, extractEmbeddedPredicateConditions(block, stage, budget)...)
	}
	return deduplicateConditions(conditions, budget)
}

func splitParenthesizedBlocks(text string) []string {
	var blocks []string
	start := -1
	depth := 0
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(text); i++ {
		c := text[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if inString {
			if c == stringChar && !isEscapedByte(text, i) {
				inString = false
			}
			continue
		}

		switch c {
		case '(':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ')':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				blocks = append(blocks, strings.TrimSpace(text[start:i]))
				start = -1
			}
		}
	}
	return blocks
}

func embeddedPredicateSegment(segment string, first bool) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimRight(segment, ","))
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"search ", "where ", "filter "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):]), true
		}
	}
	if strings.HasPrefix(lower, "union") {
		rest := strings.TrimSpace(trimmed[len("union"):])
		if rest != "" {
			return rest, true
		}
	}
	if strings.HasPrefix(lower, "and ") || strings.HasPrefix(lower, "or ") {
		return trimmed, true
	}
	if strings.HasPrefix(trimmed, "(") || strings.HasPrefix(trimmed, "!") || strings.HasPrefix(lower, "not ") {
		return trimmed, true
	}
	if first && looksLikePortablePredicate(trimmed) {
		return trimmed, true
	}
	if looksLikePortablePredicate(trimmed) && !startsWithEmbeddedTransformCommand(lower) {
		return trimmed, true
	}
	return "", false
}

func looksLikePortablePredicate(text string) bool {
	return portableComparisonPattern.MatchString(text) ||
		portableFunctionComparisonPattern.MatchString(text) ||
		portableInConditionPattern.MatchString(text) ||
		portableFunctionInConditionPattern.MatchString(text) ||
		portableNetIPSubnetPattern.MatchString(text) ||
		portableCidrMatchPattern.MatchString(text) ||
		portableNullFunctionPattern.MatchString(text)
}

func startsWithEmbeddedTransformCommand(lower string) bool {
	for _, prefix := range []string{
		"columns ", "column ", "sort ", "group ", "let ", "sql ", "join ",
		"left join ", "right join ", "inner join ", "outer join ", "on ",
		"format ", "limit ", "dedup ", "return ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func normalizeEmbeddedPredicateText(text string) string {
	text = decodeEscapedQuotes(text)
	text = strings.ReplaceAll(text, "\r\n", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")
	return strings.Join(strings.Fields(text), " ")
}

func extractSubsearchConditions(query string, depth int, budget *parseBudget) []Condition {
	budget.step()

	if budget != nil && depth > budget.limits.maxNestingDepth {
		budget.reject("embedded nesting depth")
	}
	var conditions []Condition
	for _, subsearch := range splitBracketedSubsearches(query) {
		budget.step()

		conditions = budget.appendConditions(conditions, extractPortableSPLConditionsDepth(normalizeSPLQuery(subsearch), depth+1, budget)...)
	}
	return conditions
}

func splitBracketedSubsearches(query string) []string {
	var subsearches []string
	start := -1
	depth := 0
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(query); i++ {
		c := query[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if inString {
			if c == stringChar && !isEscapedByte(query, i) {
				inString = false
			}
			continue
		}
		switch c {
		case '[':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ']':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				subsearches = append(subsearches, strings.TrimSpace(query[start:i]))
				start = -1
			}
		}
	}
	return subsearches
}

func extractLDAPSearchConditions(query string, budget *parseBudget) []Condition {
	budget.step()

	var conditions []Condition
	for stage, segment := range splitSPLPipelineSegments(query) {
		budget.step()

		if !strings.Contains(strings.ToLower(segment), "ldapsearch") {
			continue
		}
		for _, literal := range extractQuotedSearchValues(segment) {
			budget.step()

			filter := strings.TrimSpace(decodeSPLStringLiteral(literal))
			if !strings.HasPrefix(filter, "(") {
				continue
			}
			conditions = budget.appendConditions(conditions, parseLDAPFilterConditions(filter, stage, budget)...)
		}
	}
	return conditions
}

func extractQuotedSearchValues(text string) []string {
	pattern := regexp.MustCompile(`(?is)\bsearch\s*=\s*(` + quotedOptionValuePattern + `)`)
	matches := pattern.FindAllStringSubmatchIndex(text, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		values = append(values, text[match[2]:match[3]])
	}
	return values
}

func decodeSPLStringLiteral(literal string) string {
	literal = strings.TrimSpace(literal)
	if len(literal) >= 2 {
		first := literal[0]
		last := literal[len(literal)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			literal = literal[1 : len(literal)-1]
		}
	}
	return decodeEscapedQuotes(literal)
}

func decodeEscapedQuotes(value string) string {
	for i := 0; i < 8; i++ {
		next := strings.ReplaceAll(value, `\"`, `"`)
		next = strings.ReplaceAll(next, `\'`, `'`)
		if next == value {
			return next
		}
		value = next
	}
	return value
}

func parseLDAPFilterConditions(filter string, stage int, budget *parseBudget) []Condition {
	defer budget.enterInterpretation(filter)()
	budget.step()

	parser := ldapFilterParser{text: filter, stage: stage, budget: budget}
	return deduplicateConditions(parser.parse(), budget)
}

type ldapFilterParser struct {
	budget *parseBudget
	depth  int
	text   string
	pos    int
	stage  int
}

func (p *ldapFilterParser) parse() []Condition {
	p.skipSpace()
	conditions := p.parseFilter(false)
	return conditions
}

func (p *ldapFilterParser) parseFilter(negated bool) []Condition {
	p.budget.step()
	if p.budget != nil && p.depth >= p.budget.limits.maxNestingDepth {
		p.budget.reject("LDAP nesting depth")
	}
	p.depth++
	defer func() { p.depth-- }()
	p.skipSpace()
	if !p.consume('(') {
		return nil
	}
	p.skipSpace()
	if p.done() {
		return nil
	}

	switch p.peek() {
	case '&', '|':
		op := p.peek()
		p.pos++
		var conditions []Condition
		first := true
		for {
			p.skipSpace()
			if p.done() || p.peek() == ')' {
				break
			}
			child := p.parseFilter(negated)
			if len(child) == 0 {
				break
			}
			if op == '|' && !first {
				child[0].LogicalOp = "OR"
			} else if child[0].LogicalOp == "" {
				child[0].LogicalOp = "AND"
			}
			conditions = p.budget.appendConditions(conditions, child...)
			first = false
		}
		p.consume(')')
		return conditions
	case '!':
		p.pos++
		conditions := p.parseFilter(!negated)
		p.consume(')')
		return conditions
	default:
		bodyStart := p.pos
		for !p.done() && p.peek() != ')' {
			p.pos++
		}
		body := strings.TrimSpace(p.text[bodyStart:p.pos])
		p.consume(')')
		condition, ok := ldapAssertionCondition(body, negated, p.stage)
		if !ok {
			return nil
		}
		return p.budget.appendConditions(nil, condition)
	}
}

func (p *ldapFilterParser) skipSpace() {
	for !p.done() && unicode.IsSpace(rune(p.peek())) {
		p.pos++
	}
}

func (p *ldapFilterParser) consume(ch byte) bool {
	if p.done() || p.peek() != ch {
		return false
	}
	p.pos++
	return true
}

func (p *ldapFilterParser) peek() byte {
	return p.text[p.pos]
}

func (p *ldapFilterParser) done() bool {
	p.budget.step()
	return p.pos >= len(p.text)
}

func ldapAssertionCondition(body string, negated bool, stage int) (Condition, bool) {
	field, op, value, ok := splitLDAPAssertion(body)
	if !ok {
		return Condition{}, false
	}
	field = normalizePortableField(field)
	value = normalizePortableValue(value)
	if shouldSkipPortableField(field) || value == "" {
		return Condition{}, false
	}

	switch op {
	case "=":
		if value == "*" {
			op = "isnotnull"
			value = ""
		} else if strings.Contains(value, "*") {
			op = "like"
		}
	case "~=":
		op = "matches"
	}

	return Condition{
		Field:     field,
		Operator:  op,
		Value:     value,
		Negated:   negated,
		PipeStage: stage,
		LogicalOp: "AND",
	}, true
}

func splitLDAPAssertion(body string) (field, op, value string, ok bool) {
	for _, candidate := range []string{">=", "<=", "~=", "="} {
		if idx := strings.Index(body, candidate); idx >= 0 {
			return strings.TrimSpace(body[:idx]), candidate, strings.TrimSpace(body[idx+len(candidate):]), true
		}
	}
	return "", "", "", false
}

func extractCommandResourceConditions(query string, budget *parseBudget) []Condition {
	budget.step()

	var conditions []Condition
	for stage, segment := range splitSPLPipelineSegments(query) {
		budget.step()

		trimmed := strings.TrimSpace(segment)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "inputlookup "):
			if value := firstCommandResourceArgSource(trimmed, "inputlookup"); value != "" {
				conditions = budget.appendConditions(conditions, resourceCondition("_spl.inputlookup", value, stage))
			}
		case strings.HasPrefix(lower, "outputlookup "):
			if value := firstCommandResourceArgSource(trimmed, "outputlookup"); value != "" {
				conditions = budget.appendConditions(conditions, resourceCondition("_spl.outputlookup", value, stage))
			}
		case strings.HasPrefix(lower, "collect "):
			if value := extractOptionValue(trimmed, "index"); value != "" {
				conditions = budget.appendConditions(conditions, resourceCondition("_spl.collect.index", value, stage))
			}
			if value := extractOptionValue(trimmed, "source"); value != "" {
				conditions = budget.appendConditions(conditions, resourceCondition("_spl.collect.source", value, stage))
			}
		case strings.HasPrefix(lower, "tstats "):
			if value := extractTstatsDatamodelValue(trimmed); value != "" {
				conditions = budget.appendConditions(conditions, resourceCondition("_spl.datamodel", value, stage))
			}
		}
	}
	return deduplicateConditions(conditions, budget)
}

func resourceCondition(field, value string, stage int) Condition {
	kind := OperandBare
	if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'") {
		kind = OperandString
	}
	decoded := normalizePortableValue(value)
	return Condition{
		Field:     field,
		Context:   PredicateResource,
		LHS:       &Operand{Kind: OperandField, Value: field},
		RHS:       &Operand{Kind: kind, Source: value, Value: decoded},
		Operator:  "=",
		Value:     decoded,
		PipeStage: stage,
		LogicalOp: "AND",
	}
}

func firstCommandResourceArg(segment, command string) string {
	return normalizePortableValue(firstCommandResourceArgSource(segment, command))
}

func firstCommandResourceArgSource(segment, command string) string {
	rest := strings.TrimSpace(segment[len(command):])
	for _, arg := range splitSPLArgs(rest) {
		if strings.Contains(arg, "=") {
			continue
		}
		return arg
	}
	return ""
}

func splitSPLArgs(text string) []string {
	var args []string
	start := -1
	inString := false
	stringChar := byte(0)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if start < 0 && !unicode.IsSpace(rune(c)) {
			start = i
		}
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if inString {
			if c == stringChar && !isEscapedByte(text, i) {
				inString = false
			}
			continue
		}
		if unicode.IsSpace(rune(c)) && start >= 0 {
			args = append(args, text[start:i])
			start = -1
		}
	}
	if start >= 0 {
		args = append(args, text[start:])
	}
	return args
}

func extractOptionValue(text, optionName string) string {
	pattern := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(optionName) + `\s*=\s*(` + quotedOptionValuePattern + `|[^\s\]]+)`)
	match := pattern.FindStringSubmatchIndex(text)
	if match == nil {
		return ""
	}
	return normalizePortableValue(text[match[2]:match[3]])
}

func extractTstatsDatamodelValue(segment string) string {
	pattern := regexp.MustCompile(`(?is)\bfrom\s+datamodel(?:=|:)([A-Za-z0-9_.:-]+)`)
	match := pattern.FindStringSubmatch(segment)
	if len(match) < 2 {
		return ""
	}
	return normalizePortableValue(match[1])
}

func splitPortableValueList(valuesText string) []string {
	var values []string
	start := 0
	inString := false
	stringChar := byte(0)
	depth := 0
	for i := 0; i < len(valuesText); i++ {
		c := valuesText[i]
		if !inString && (c == '"' || c == '\'') {
			inString = true
			stringChar = c
			continue
		}
		if inString {
			if c == stringChar && !isEscapedByte(valuesText, i) {
				inString = false
			}
			continue
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth != 0 {
				continue
			}
			if value := normalizePortableValue(valuesText[start:i]); value != "" {
				values = append(values, value)
			}
			start = i + 1
		}
	}
	if value := normalizePortableValue(valuesText[start:]); value != "" {
		values = append(values, value)
	}
	return values
}

func normalizePortableField(field string) string {
	field = strings.TrimSpace(field)
	field = trimMatchingQuotes(field)
	return field
}

func normalizePortableValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ";")
	value = trimMatchingQuotes(value)
	value = decodeEscapedQuotes(value)
	value = trimMatchingQuotes(value)
	return value
}

func shouldSkipPortableField(field string) bool {
	if field == "" {
		return true
	}
	if strings.Contains(field, "<<") || strings.Contains(field, ">>") ||
		strings.ContainsAny(field, "<> \t\r\n") ||
		strings.HasPrefix(field, ".") || strings.HasSuffix(field, ".") {
		return true
	}
	lower := strings.ToLower(field)
	if isExcludedField(lower) {
		return true
	}
	switch lower {
	case "and", "or", "not", "true", "false", "null":
		return true
	}
	return false
}

func trimMatchingQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	first := value[0]
	last := value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

type conditionIdentity struct {
	stageID, inputScopeID, fieldBinding    string
	field, operator, value, valueReference string
	logicalOp, sourceField, alternatives   string
	context                                PredicateContext
	lhs, rhs                               string
	negated, isComputed                    bool
	pipeStage                              int
}

func identityOfCondition(condition Condition, budget *parseBudget) conditionIdentity {
	var alternatives strings.Builder
	for _, value := range condition.Alternatives {
		budget.step()
		alternatives.WriteString(strconv.Itoa(len(value)))
		alternatives.WriteByte(':')
		alternatives.WriteString(value)
	}
	return conditionIdentity{
		stageID: condition.StageID, inputScopeID: condition.InputScopeID,
		fieldBinding: effectiveFieldBindingIdentity(condition.FieldBinding, condition.pendingFieldBinding),
		field:        condition.Field, operator: condition.Operator, value: condition.Value,
		context: condition.Context, lhs: operandIdentity(condition.LHS, budget), rhs: operandIdentity(condition.RHS, budget),
		valueReference: condition.ValueReference, logicalOp: condition.LogicalOp,
		sourceField: condition.SourceField, alternatives: alternatives.String(),
		negated: condition.Negated, isComputed: condition.IsComputed, pipeStage: condition.PipeStage,
	}
}

func fieldBindingIdentity(ref *FieldBindingRef) string {
	if ref == nil {
		return "0"
	}
	var builder strings.Builder
	builder.WriteByte('1')
	appendIdentityPart(&builder, ref.ScopeID)
	appendIdentityPart(&builder, ref.Kind)
	appendIdentityPart(&builder, ref.BindingID)
	return builder.String()
}

func deduplicateConditions(conditions []Condition, budgets ...*parseBudget) []Condition {
	var budget *parseBudget
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	out, _ := deduplicateConditionsWithIndexMap(conditions, budget)
	return out
}

func deduplicateConditionsWithIndexMap(conditions []Condition, budget *parseBudget) ([]Condition, []int) {
	seen := make(map[conditionIdentity]int, len(conditions))
	out := make([]Condition, 0, len(conditions))
	indexMap := make([]int, len(conditions))
	for i, condition := range conditions {
		budget.step()
		key := identityOfCondition(condition, budget)
		if index, ok := seen[key]; ok {
			indexMap[i] = index
			continue
		}
		indexMap[i] = len(out)
		seen[key] = len(out)
		condition.Alternatives = cloneConditionAlternatives(condition.Alternatives, budget)
		out = append(out, condition)
	}
	return out, indexMap
}

func cloneConditionAlternatives(values []string, budget *parseBudget) []string {
	if values == nil {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		budget.step()
		result = append(result, value)
	}
	return result
}

// ExtractConditions parses an SPL query and extracts all field conditions.
// Applies bounded input, syntax nesting and cooperative work limits.
func ExtractConditions(query string) *ParseResult {
	return ExtractConditionsContext(context.Background(), query)
}

// ExtractConditionsContext applies caller cancellation and MaxParseTime at
// cooperative boundaries. No parsing goroutine outlives this call.
func ExtractConditionsContext(ctx context.Context, query string) *ParseResult {
	return extractConditionsContext(ctx, query, defaultParseLimits())
}

func extractConditionsContext(ctx context.Context, query string, limits parseLimits) (result *ParseResult) {
	defer func() {
		if value := recover(); value != nil {
			result = &ParseResult{Conditions: []Condition{}, Commands: []string{}, Errors: []string{parseFailure(value).Error()}}
		}
	}()
	budget, cancel := newParseBudget(ctx, limits)
	defer cancel()
	return extractConditionsInternal(query, budget)
}

func extractConditionsInternal(query string, budget *parseBudget) (result *ParseResult) {
	budget.input(query)
	preflight(query, budget, false, nil)
	normalizedQuery, sourceMap := normalizeSPLQueryMapped(query, budget)
	budget.input(normalizedQuery)
	lexerErrors := &errorListener{budget: budget}
	stream := preflight(normalizedQuery, budget, true, lexerErrors)
	parser := NewSPLParser(&budgetTokenStream{CommonTokenStream: stream, budget: budget})

	// Remove default error listener and add our own
	parser.RemoveErrorListeners()
	parserErrors := &errorListener{budget: budget}
	parser.AddErrorListener(parserErrors)

	// Parse the query
	root := parser.Root()
	tree := root.Query()
	unparsed := hasUnparsedTail(root)
	if parserErrors.truncatedNativeWherePredicate {
		budget.check()
		allErrors := append(lexerErrors.errors, parserErrors.errors...)
		return &ParseResult{
			Conditions:           []Condition{},
			Commands:             []string{},
			ExpressionIncomplete: true,
			Errors:               allErrors,
		}
	}

	// Walk the tree to extract conditions
	budget.step() // source-binding map allocation
	extractor := &conditionExtractor{
		budget:            budget,
		conditions:        make([]Condition, 0),
		conditionSources:  make(conditionSourceBindings),
		scalarRoots:       make(map[antlr.Tree]bool),
		computedFields:    make(map[string]string), // computed field -> source field
		fieldAliases:      make(map[string]string), // rename mappings: new name -> original name
		commands:          make([]string, 0),
		joins:             make([]JoinInfo, 0),
		dataflowOwners:    []dataflowOwnerFrame{{OwnerID: 0, Selected: true}},
		ownerByContext:    make(map[antlr.ParserRuleContext]int),
		canonicalChildren: make(map[antlr.ParserRuleContext]string),
		subsearches:       make([]*ParseResult, 0),
		lastLogicalOp:     "AND", // default
		tokenStream:       stream,
		originalQuery:     query,
		sourceMap:         sourceMap,
		stageNumbers:      make(map[*PipelineStageContext]int),
		subsearchIndices:  make(map[*SubsearchContext]int),
	}
	antlr.ParseTreeWalkerDefault.Walk(extractor, tree)
	budget.check()
	extractor.checkOutput()

	// Combine errors
	allErrors := append(lexerErrors.errors, parserErrors.errors...)
	allErrors = append(allErrors, extractor.errors...)
	var dataflow *QueryDataflow
	var dataflowWarnings []string
	if len(allErrors) == 0 {
		dataflow, dataflowWarnings = newSPLDataflowBuilder(extractor).build(tree)
		if dataflow == nil {
			return &ParseResult{Conditions: []Condition{}, Commands: []string{}, Errors: []string{"dataflow source association failed"}}
		}
	}

	// The raw tree proves which OR operands are atomic siblings. A flat
	// LogicalOp sequence cannot distinguish a sibling from an AND subtree.
	rawConditions := make([]Condition, 0, len(extractor.conditions))
	for _, condition := range extractor.conditions {
		budget.step()
		condition.Alternatives = cloneConditionAlternatives(condition.Alternatives, budget)
		rawConditions = append(rawConditions, condition)
	}
	unsupportedSearchOperand := hasUnsupportedSearchOperand(tree, budget)
	incomplete := unsupportedSearchOperand || unparsed
	var rawExpression *ConditionExpression
	if !incomplete {
		var missingAtom bool
		rawExpression, missingAtom = buildConditionExpressionWithCompleteness(tree, rawConditions, budget, extractor.conditionSources)
		incomplete = missingAtom
	}
	conditions, conditionIndexMap := groupORConditionsByExpression(rawConditions, rawExpression, budget)
	expression := remapConditionExpression(rawExpression, conditionIndexMap, budget)
	recoveredWithPortableExtraction := false
	if len(allErrors) > 0 && containsScalarExpression(expression) {
		incomplete = true
	} else if len(allErrors) > 0 {
		if portableConditions := extractPortableSPLConditions(query, budget); len(portableConditions) > 0 {
			conditions = portableConditions
			expression = nil
			recoveredWithPortableExtraction = true
		}
	} else if len(conditions) == 0 && !containsScalarExpression(expression) {
		if portableConditions := extractPortableSPLConditions(query, budget); len(portableConditions) > 0 {
			conditions = portableConditions
			expression = nil
		}
	}
	if resourceConditions := extractCommandResourceConditions(normalizedQuery, budget); len(resourceConditions) > 0 {
		if len(conditions) == 0 && expression == nil {
			expression = nil
			recoveredWithPortableExtraction = len(allErrors) > 0
		}
		combined := make([]Condition, 0, len(conditions)+len(resourceConditions))
		for _, condition := range conditions {
			budget.step()
			combined = append(combined, condition)
		}
		for _, condition := range resourceConditions {
			budget.step()
			combined = append(combined, condition)
		}
		var indexMap []int
		conditions, indexMap = deduplicateConditionsWithIndexMap(combined, budget)
		expression = remapConditionExpression(expression, indexMap, budget)
	}
	if recoveredWithPortableExtraction && !containsScalarExpression(expression) {
		allErrors = nil
		// The regex recovery cannot vouch for the syntax the grammar rejected.
		incomplete = true
	}

	for i := range conditions {
		budget.step()
		budget.reserveOutput(operandNodeCount(conditions[i].LHS, budget) + operandNodeCount(conditions[i].RHS, budget))
	}
	budget.outputSize(len(conditions))
	budget.check()
	materializeConditionBindings(conditions, budget)

	var warnings []string
	if incomplete {
		expression = nil
		budget.reserveOutput(1)
		warning := "boolean expression unavailable: query contains an unsupported or unrepresented boolean operand"
		if unsupportedSearchOperand {
			warning = "boolean expression unavailable: search contains an unsupported macro or subsearch operand"
		}
		if unparsed {
			warning = "boolean expression unavailable: the parser stopped before the end of the query"
		}
		warnings = append(warnings, warning)
	}
	warnings = append(warnings, dataflowWarnings...)
	result = &ParseResult{
		Dataflow:             dataflow,
		ExpressionIncomplete: incomplete,
		Warnings:             warnings,
		Conditions:           conditions,
		Expression:           expression,
		GroupByFields:        extractor.groupByFields,
		Aggregations:         extractor.aggregations,
		ComputedFields:       extractor.computedFields,
		FieldAliases:         extractor.fieldAliases,
		Commands:             extractor.commands,
		Joins:                extractor.joins,
		Subsearches:          extractor.subsearches,
		Errors:               allErrors,
	}
	return finalizeSPLDataflowResult(result, budget)
}

func (e *conditionExtractor) EnterPipelineStage(ctx *PipelineStageContext) {
	if e.ownsDataflow(ctx) {
		e.currentStage = e.nextStage
		e.stageNumbers[ctx] = e.currentStage
	}
}

// ExitPipelineStage increments the stage counter after processing each stage.
// A subsearch's stages advance it too, but leave currentStage to their owner.
func (e *conditionExtractor) ExitPipelineStage(ctx *PipelineStageContext) {
	e.nextStage++
}

// EnterSubsearch tracks when we enter a subsearch
func (e *conditionExtractor) EnterSubsearch(ctx *SubsearchContext) {
	if e.inSubsearch == 0 {
		subText := strings.TrimSpace(e.extractSubsearchText(ctx))
		if subText != "" {
			e.subsearchIndices[ctx] = len(e.subsearches)
			if _, canonical := e.canonicalChildren[ctx]; !canonical {
				e.canonicalChildren[ctx] = "subsearches/" + strconv.Itoa(len(e.subsearches))
			}
			e.subsearches = append(e.subsearches, extractConditionsInternal(subText, e.budget))
		}
	}
	e.enterDataflowOwner(ctx)
	e.inSubsearch++
}

// ExitSubsearch tracks when we exit a subsearch
func (e *conditionExtractor) ExitSubsearch(ctx *SubsearchContext) {
	e.inSubsearch--
	e.leaveDataflowOwner()
}

// extractSubsearchText extracts the raw query text inside a subsearch's brackets
// using character positions from the original query string. We use the original
// query rather than GetTextFromTokens because the latter strips whitespace
// (WS tokens are on the HIDDEN channel).
func (e *conditionExtractor) extractSubsearchText(ctx *SubsearchContext) string {
	if ctx == nil || ctx.Query() == nil {
		return ""
	}
	if len(e.sourceMap.spans) == 0 {
		return ctx.Query().GetText()
	}
	return e.sourceOf(ctx.Query())
}

// EnterJoinCommand extracts join metadata and recursively parses the subsearch
func (e *conditionExtractor) EnterJoinCommand(ctx *JoinCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
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
				e.budget.outputSize(len(info.JoinFields) + 1)
				info.JoinFields = append(info.JoinFields, foq.FieldName().GetText())
			} else if foq.QUOTED_STRING() != nil {
				field := trimMatchingQuotes(foq.QUOTED_STRING().GetText())
				// Reserve decoding work before allocating the field identity.
				for range len(field) {
					e.budget.step()
				}
				field = decodeSPLQuotedOperand(field)
				e.budget.outputSize(len(info.JoinFields) + 1)
				info.JoinFields = append(info.JoinFields, field)
			}
		}
	}

	// Recursively parse the subsearch
	if ctx.Subsearch() != nil {
		e.canonicalChildren[ctx.Subsearch().(*SubsearchContext)] = "joins/" + strconv.Itoa(len(e.joins))
		subText := e.extractSubsearchText(ctx.Subsearch().(*SubsearchContext))
		if subText != "" {
			info.Subsearch = extractConditionsInternal(subText, e.budget)
			info.ExposedFields = deriveExposedFields(info.Subsearch, info.JoinFields)
		}
	}

	e.joins = append(e.joins, info)
}

// EnterAppendCommand extracts append subsearch info
func (e *conditionExtractor) EnterAppendCommand(ctx *AppendCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "append")

	info := JoinInfo{
		Type:      "append",
		IsAppend:  true,
		PipeStage: e.currentStage,
	}

	if ctx.Subsearch() != nil {
		e.canonicalChildren[ctx.Subsearch().(*SubsearchContext)] = "joins/" + strconv.Itoa(len(e.joins))
		subText := e.extractSubsearchText(ctx.Subsearch().(*SubsearchContext))
		if subText != "" {
			info.Subsearch = extractConditionsInternal(subText, e.budget)
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
	// Map iteration order varies between calls.
	sort.Strings(result)
	return result
}

// ClassifyFieldProvenance determines where a field originates relative to
// the first join in the query.
// Returns ProvenanceAmbiguous if no joins exist or provenance can't be determined.
func ClassifyFieldProvenance(result *ParseResult, field string) FieldProvenance {
	if result == nil || len(result.Joins) == 0 {
		return ProvenanceAmbiguous
	}

	// Check join keys first (they exist on both sides)
	for _, j := range result.Joins {
		for _, jf := range j.JoinFields {
			if jf == field {
				return ProvenanceJoinKey
			}
		}
	}

	// Check if field is in exposed fields from any join's subsearch
	for _, j := range result.Joins {
		for _, ef := range j.ExposedFields {
			if ef == field {
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
		if c.Field == field && c.PipeStage < firstJoinStage {
			return ProvenanceMain
		}
	}

	// Field exists in conditions but after join — check if it's from main query scope
	// by seeing if it appears in pre-join computed fields
	if _, ok := result.ComputedFields[field]; ok {
		return ProvenanceMain
	}

	return ProvenanceAmbiguous
}

// EnterFunctionCall extracts only argument shapes that the Condition model can
// represent. Other complete calls are retained by the scalar expression path.
func (e *conditionExtractor) EnterFunctionCall(ctx *FunctionCallContext) {
	e.inFunctionCall++
	if e.scalarDepth > 0 || !e.ownsDataflow(ctx) || e.inEvalCommand > 0 || e.inStatsFunction > 0 || e.inFunctionCall > 1 ||
		!representedBareFunction(ctx, e.budget) {
		return
	}
	args := ctx.ArgumentList().AllExpression()
	syntax := predicateContextFor(ctx, e.budget)
	var lhs, rhs *Operand
	operator := ""
	switch {
	case ctx.CIDRMATCH() != nil:
		rhs = directExpressionOperand(args[0], e.budget)
		lhs = directExpressionOperand(args[1], e.budget)
		operator = "cidrmatch"
		rhs.Source, lhs.Source = e.sourceOf(args[0]), e.sourceOf(args[1])
	case ctx.MATCH() != nil, ctx.LIKE() != nil:
		lhs = directExpressionOperand(args[0], e.budget)
		rhs = directExpressionOperand(args[1], e.budget)
		lhs.Source, rhs.Source = e.sourceOf(args[0]), e.sourceOf(args[1])
		if ctx.MATCH() != nil {
			operator = "matches"
		} else {
			operator = "like"
		}
	case ctx.ISNULL() != nil, ctx.ISNOTNULL() != nil:
		lhs = directExpressionOperand(args[0], e.budget)
		lhs.Source = e.sourceOf(args[0])
		if ctx.ISNULL() != nil {
			operator = "isnull"
		} else {
			operator = "isnotnull"
		}
	}
	if lhs == nil || lhs.Kind != OperandField || operator == "" {
		return
	}
	value := ""
	if rhs != nil {
		value = rhs.Value
		if rhs.Kind == OperandString {
			value = trimMatchingQuotes(rhs.Source)
		}
	}
	if operator == "like" {
		value = strings.ReplaceAll(value, "%", "*")
	}
	condition := Condition{Field: lhs.Value, Context: syntax, LHS: lhs, RHS: rhs, Operator: operator,
		Value: value, Negated: e.negated, PipeStage: e.currentStage, LogicalOp: e.lastLogicalOp}
	e.appendSourcedCondition(condition, ctx)
	e.lastLogicalOp = "AND"
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

// ExitStatsFunction tracks when we exit a stats function.
// It also registers any "AS alias" as a computed field so that post-aggregation
// filters (e.g. | where events > 5) are recognized as operating on computed fields.
func (e *conditionExtractor) ExitStatsFunction(ctx *StatsFunctionContext) {
	e.inStatsFunction--
	if !e.ownsDataflow(ctx) {
		return
	}

	// If this stats function has an AS alias, register it as a computed field
	if ctx.AS() != nil && (ctx.FieldName() != nil || ctx.QUOTED_STRING() != nil) {
		alias := splFieldName(ctx.FieldName())
		if ctx.QUOTED_STRING() != nil {
			alias = decodeSPLQuotedOperand(trimMatchingQuotes(ctx.QUOTED_STRING().GetText()))
		}
		// The source is the expression inside the function, or the function name itself
		sourceField := ""
		if ctx.Expression() != nil {
			sourceField = extractFirstFieldFromExpression(ctx.Expression())
		}
		if sourceField == "" {
			// For functions like count (no expression), use the function name as source marker
			sourceField = strings.ToLower(ctx.IDENTIFIER().GetText())
		}
		if !unresolvedSPLFieldName(alias) {
			e.computedFields[alias] = sourceField
		}
	}

	if aggregation, ok := newAggregation(ctx, e.currentStage); ok {
		e.aggregations = append(e.aggregations, aggregation)
	}
}

// newAggregation describes a stats-like function and the column it outputs.
// Without an alias, Splunk names the column after the function as written,
// e.g. count or dc(src).
func newAggregation(ctx *StatsFunctionContext, stage int) (Aggregation, bool) {
	aggregation := Aggregation{PipeStage: stage}
	switch ctx.GetParent().(type) {
	case *StatsCommandContext:
		aggregation.Command = "stats"
	case *EventstatsCommandContext:
		aggregation.Command = "eventstats"
	case *StreamstatsCommandContext:
		aggregation.Command = "streamstats"
	case *ChartCommandContext:
		aggregation.Command = "chart"
	case *TimechartCommandContext:
		aggregation.Command = "timechart"
	case *TstatsCommandContext:
		aggregation.Command = "tstats"
	case *MstatsCommandContext:
		aggregation.Command = "mstats"
	default:
		return Aggregation{}, false
	}
	if ctx.IDENTIFIER() == nil {
		return Aggregation{}, false
	}
	function := ctx.IDENTIFIER().GetText()
	aggregation.Function = strings.ToLower(function)

	column := function
	if ctx.LPAREN() != nil {
		argument := ""
		if ctx.Expression() != nil {
			argument = ctx.Expression().GetText()
			if extractFirstFieldFromExpression(ctx.Expression()) == argument {
				aggregation.Field = argument
			}
		}
		column += "(" + argument + ")"
	}
	switch {
	case ctx.FieldName() != nil:
		column = splFieldName(ctx.FieldName())
	case ctx.QUOTED_STRING() != nil:
		column = decodeSPLQuotedOperand(trimMatchingQuotes(ctx.QUOTED_STRING().GetText()))
	}
	aggregation.Column = column
	return aggregation, true
}

// EnterTstatsCommand extracts group-by fields, datamodel reference, and commands from tstats
func (e *conditionExtractor) EnterTstatsCommand(ctx *TstatsCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "tstats")

	// Extract BY/GROUPBY fields from fieldOrQuoted elements
	for _, foq := range ctx.AllFieldOrQuoted() {
		if foq.FieldName() != nil {
			field := foq.FieldName().GetText()
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.groupByFields = append(e.groupByFields, field)
			}
		} else if foq.QUOTED_STRING() != nil {
			field := strings.Trim(foq.QUOTED_STRING().GetText(), `"'`)
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.groupByFields = append(e.groupByFields, field)
			}
		}
	}

	// Extract datamodel reference
	if ctx.TstatsDatamodel() != nil {
		dm := ctx.TstatsDatamodel()
		ids := dm.AllIDENTIFIER()
		if dm.EQ() != nil && len(ids) >= 2 {
			// "datamodel=Endpoint.Processes" — skip "datamodel" keyword
			parts := make([]string, 0, len(ids)-1)
			for _, id := range ids[1:] {
				parts = append(parts, id.GetText())
			}
			e.computedFields["_datamodel"] = strings.Join(parts, ".")
		} else {
			// Plain "Endpoint.Processes"
			parts := make([]string, 0, len(ids))
			for _, id := range ids {
				parts = append(parts, id.GetText())
			}
			e.computedFields["_datamodel"] = strings.Join(parts, ".")
		}
	}
}

// EnterMstatsCommand extracts group-by fields from mstats commands (metrics store)
func (e *conditionExtractor) EnterMstatsCommand(ctx *MstatsCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "mstats")

	// Extract BY/GROUPBY fields from fieldOrQuoted elements
	for _, foq := range ctx.AllFieldOrQuoted() {
		if foq.FieldName() != nil {
			field := foq.FieldName().GetText()
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.groupByFields = append(e.groupByFields, field)
			}
		} else if foq.QUOTED_STRING() != nil {
			field := strings.Trim(foq.QUOTED_STRING().GetText(), `"'`)
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.groupByFields = append(e.groupByFields, field)
			}
		}
	}
}

// EnterInputlookupCommand extracts the command name for inputlookup
func (e *conditionExtractor) EnterInputlookupCommand(ctx *InputlookupCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "inputlookup")
}

// EnterStatsCommand extracts group-by fields from stats commands
func (e *conditionExtractor) EnterStatsCommand(ctx *StatsCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "stats")
	e.extractByFields(ctx.FieldList())
}

// EnterEventstatsCommand extracts group-by fields from eventstats commands
func (e *conditionExtractor) EnterEventstatsCommand(ctx *EventstatsCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "eventstats")
	e.extractByFields(ctx.FieldList())
}

// EnterStreamstatsCommand extracts group-by fields from streamstats commands
func (e *conditionExtractor) EnterStreamstatsCommand(ctx *StreamstatsCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "streamstats")
	e.extractByFields(ctx.FieldList())
}

// EnterTimechartCommand extracts group-by fields from timechart commands
func (e *conditionExtractor) EnterTimechartCommand(ctx *TimechartCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
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
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "chart")
	for _, fieldList := range ctx.AllFieldList() {
		e.extractByFields(fieldList)
	}
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
				e.budget.outputSize(len(e.groupByFields) + 1)
				e.groupByFields = append(e.groupByFields, field)
			}
		} else if fieldOrQuoted.QUOTED_STRING() != nil {
			field := trimMatchingQuotes(fieldOrQuoted.QUOTED_STRING().GetText())
			// Charge the decode buffer before allocation, using the shared request budget.
			for range len(field) {
				e.budget.step()
			}
			field = decodeSPLQuotedOperand(field)
			fieldLower := strings.ToLower(field)
			if !isExcludedField(fieldLower) {
				e.budget.outputSize(len(e.groupByFields) + 1)
				e.groupByFields = append(e.groupByFields, field)
			}
		}
	}
}

// EnterDedupCommand extracts fields from dedup commands
func (e *conditionExtractor) EnterDedupCommand(ctx *DedupCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.extractByFields(ctx.FieldList())
}

// EnterFieldsCommand extracts fields from fields commands (field selection)
func (e *conditionExtractor) EnterFieldsCommand(ctx *FieldsCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "fields")
	// `fields - x` removes x; only a kept list names fields the stage outputs.
	if ctx.MINUS() == nil {
		e.extractByFields(ctx.FieldList())
	}
}

// EnterTableCommand extracts fields from table commands (display fields)
func (e *conditionExtractor) EnterTableCommand(ctx *TableCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "table")
	e.extractByFields(ctx.FieldList())
}

// EnterTopCommand extracts fields from top commands
func (e *conditionExtractor) EnterTopCommand(ctx *TopCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	for _, fieldList := range ctx.AllFieldList() {
		e.extractByFields(fieldList)
	}
	e.addValueCountAggregations("top")
}

// EnterRareCommand extracts fields from rare commands
func (e *conditionExtractor) EnterRareCommand(ctx *RareCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	for _, fieldList := range ctx.AllFieldList() {
		e.extractByFields(fieldList)
	}
	e.addValueCountAggregations("rare")
}

// addValueCountAggregations records the count and percent columns that top
// and rare output for each value.
func (e *conditionExtractor) addValueCountAggregations(command string) {
	for _, column := range []string{"count", "percent"} {
		e.aggregations = append(e.aggregations, Aggregation{Command: command, Function: column, Column: column, PipeStage: e.currentStage})
	}
}

// EnterSortCommand extracts fields from sort commands
func (e *conditionExtractor) EnterSortCommand(ctx *SortCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
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
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "eval")
	e.inEvalCommand++
}

// ExitEvalCommand leaves eval command scope.
func (e *conditionExtractor) ExitEvalCommand(ctx *EvalCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	if e.inEvalCommand > 0 {
		e.inEvalCommand--
	}
}

// EnterWhereCommand tracks where commands
func (e *conditionExtractor) EnterWhereCommand(ctx *WhereCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "where")
	e.inWhereCommand++
}

// ExitWhereCommand leaves where command scope.
func (e *conditionExtractor) ExitWhereCommand(ctx *WhereCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	if e.inWhereCommand > 0 {
		e.inWhereCommand--
	}
}

// EnterTransactionCommand tracks transaction commands and marks computed fields.
// The transaction command computes several fields that don't exist in raw events:
// - duration: seconds between first and last event in the transaction
// - eventcount: number of events in the transaction
// - closed_txn: 1 if the transaction was properly closed, 0 otherwise
// These should not be expected in test data as they're computed by the command.
func (e *conditionExtractor) EnterTransactionCommand(ctx *TransactionCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "transaction")

	// Mark transaction-computed fields
	// The source marker "_transaction" indicates these are computed by the transaction command
	e.computedFields["duration"] = "_transaction"
	e.computedFields["eventcount"] = "_transaction"
	e.computedFields["closed_txn"] = "_transaction"

	// Extract the grouping fields from transaction (these are the fields used to group events)
	if ctx.FieldList() != nil {
		e.extractByFields(ctx.FieldList())
	}
}

// EnterRexCommand tracks rex commands and extracts computed fields from named capture groups
// rex field=CommandLine "(?<script>[^\s]+\.ps1)" creates computed field "script" from "CommandLine"
func (e *conditionExtractor) EnterRexCommand(ctx *RexCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "rex")

	// Skip rex in subsearches
	if !e.ownsDataflow(ctx) {
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
			e.computedFields[captured] = sourceField
		}
	}
}

// EnterRenameCommand tracks rename commands: | rename OldField AS NewField
// Records field aliases so downstream consumers can resolve renamed fields.
func (e *conditionExtractor) EnterRenameCommand(ctx *RenameCommandContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	e.commands = append(e.commands, "rename")

	if !e.ownsDataflow(ctx) {
		return
	}

	for _, spec := range ctx.AllRenameSpec() {
		fields := spec.AllFieldName()
		// renameSpec: fieldName AS (fieldName | QUOTED_STRING)
		if len(fields) >= 1 {
			oldName := fields[0].GetText()
			var newName string
			if len(fields) >= 2 {
				newName = fields[1].GetText()
			} else if spec.QUOTED_STRING() != nil {
				newName = decodeSPLQuotedOperand(trimMatchingQuotes(spec.QUOTED_STRING().GetText()))
			}
			if newName != "" && oldName != "" {
				e.fieldAliases[newName] = oldName
				// Also track as computed field so downstream conditions
				// on the renamed field are marked as computed.
				e.computedFields[newName] = oldName
			}
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
	if !e.ownsDataflow(ctx) {
		return
	}

	// Extract the exact decoded output name; quoted names are ordinary fields.
	computedField := splFieldName(ctx.FieldName())
	if ctx.QUOTED_STRING() != nil {
		computedField = decodeSPLQuotedOperand(trimMatchingQuotes(ctx.QUOTED_STRING().GetText()))
	}
	if !unresolvedSPLFieldName(computedField) {
		sourceField := ""

		// Try to extract the source field from the expression
		// Expression is typically: function(sourceField) or function(sourceField, ...)
		if ctx.Expression() != nil {
			sourceField = extractFirstFieldFromExpression(ctx.Expression())
		}

		e.computedFields[computedField] = sourceField
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

// EnterBareWord extracts fulltext search terms, preserving their authored case
// and wildcard patterns in the same representation as quoted keywords.
func (e *conditionExtractor) EnterBareWord(ctx *BareWordContext) {
	// Skip inside subsearches
	if !e.ownsDataflow(ctx) || !bareWordIsSearchPredicate(ctx, e.budget) {
		return
	}

	// A direct NOT applies only to the first whitespace-delimited term,
	// while negation inherited from a parenthesized group applies to every term.
	directNot := false
	for parent := ctx.GetParent(); parent != nil; parent = parent.GetParent() {
		e.budget.step()
		term, ok := parent.(ISearchTermContext)
		if !ok {
			break
		}
		if term.NOT() != nil {
			directNot = !directNot
		}
	}
	parts := searchBareWordParts(ctx, e.budget)
	for i, part := range parts {
		negated := e.negated
		if i > 0 && directNot {
			negated = !negated
		}
		cond := Condition{
			Field: "_raw", Context: PredicateSearch, LHS: &Operand{Kind: OperandField, Value: "_raw"}, Operator: "contains", Value: part.value,
			Negated: negated, PipeStage: e.currentStage, LogicalOp: e.lastLogicalOp,
		}
		kind := OperandBare
		source := e.sourceMap.source(part.start, part.stop)
		if ctx.QUOTED_STRING() != nil {
			kind = OperandString
		}
		cond.RHS = &Operand{Kind: kind, Source: source, Value: part.value}
		e.appendSourcedCondition(cond, ctx)
		e.lastLogicalOp = "AND"
	}

}

// EnterCondition extracts field conditions
func (e *conditionExtractor) EnterCondition(ctx *ConditionContext) {
	if e.scalarDepth > 0 {
		return
	}
	// Skip conditions inside subsearches (like join)
	if !e.ownsDataflow(ctx) {
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

	// Eval assignments compute or transform fields; they are not predicates.
	if e.inEvalCommand > 0 {
		return
	}

	// Check for field comparison: field op value
	if ctx.FieldName() != nil && ctx.ComparisonOp() != nil && ctx.Value() != nil {
		syntax := predicateContextFor(ctx, e.budget)
		if syntax == "" {
			return
		}
		lhs, rhs, scalar := directConditionOperands(ctx, syntax, e.budget)
		if scalar {
			return
		}
		lhs.Source = e.sourceOf(ctx.FieldName())
		rhs.Source = e.sourceOf(ctx.Value())
		field := lhs.Value
		fieldLower := strings.ToLower(field)

		// Skip SPL keywords (metadata fields like index, sourcetype, etc.).
		// Stats function names stay: they are also the columns stats outputs
		// (stats count → count), so "where count > 5" filters on a field.
		if isExcludedField(fieldLower) && !statsColumnNames[fieldLower] {
			return
		}

		op := e.extractComparisonOperator(ctx)
		value := extractValue(ctx.Value())
		valueReference := ""
		if syntax == PredicateEval && rhs != nil {
			if lhs.Kind != OperandField && rhs.Kind == OperandField {
				lhs, rhs = rhs, lhs
				field = lhs.Value
				op = invertComparison(op)
			}
			switch rhs.Kind {
			case OperandField:
				valueReference, value = rhs.Value, ""
			case OperandString:
				value = trimMatchingQuotes(rhs.Source)
			default:
				value = rhs.Value
			}
		}

		// Check if this is a computed field and get its source field
		sourceField, isComputed := e.computedFields[field]

		cond := Condition{
			Field:          field,
			Context:        syntax,
			LHS:            lhs,
			RHS:            rhs,
			Operator:       op,
			Value:          value,
			ValueReference: valueReference,
			Negated:        e.negated,
			PipeStage:      e.currentStage,
			LogicalOp:      e.lastLogicalOp,
			IsComputed:     isComputed,
			SourceField:    sourceField,
		}
		e.appendSourcedCondition(cond, ctx)
		e.lastLogicalOp = "AND" // reset to default
	}

	// Check for IN operator: field IN (value1, value2, ...)
	// Create a single "in" condition with all values rather than expanding to multiple "=" conditions.
	// This preserves correct semantics for NOT field IN (...) which requires AND logic (not match ANY),
	// unlike expanded form which would be OR logic.
	if ctx.FieldName() != nil && ctx.IN() != nil && ctx.ValueList() != nil {
		syntax := predicateContextFor(ctx, e.budget)
		if syntax == "" {
			return
		}
		lhs, _, scalar := directConditionOperands(ctx, syntax, e.budget)
		if scalar {
			return
		}
		lhs.Source = e.sourceOf(ctx.FieldName())
		field := lhs.Value
		fieldLower := strings.ToLower(field)

		// Skip SPL keywords
		if isExcludedField(fieldLower) {
			return
		}

		// Check if this is a computed field and get its source field
		sourceField, isComputed := e.computedFields[field]

		values := extractValueList(ctx.ValueList())
		items := make([]Operand, 0, len(ctx.ValueList().AllValue()))
		for _, item := range ctx.ValueList().AllValue() {
			e.budget.step()
			operand := operandFromValue(item, syntax)
			if operand == nil {
				return
			}
			operand.Source = e.sourceOf(item)
			items = append(items, *operand)
		}

		// Create a single IN condition with all values
		cond := Condition{
			Field:        field,
			Context:      syntax,
			LHS:          lhs,
			RHS:          &Operand{Kind: OperandList, Source: e.sourceOf(ctx.ValueList()), Items: items},
			Operator:     "in",
			Value:        values[0], // Primary value for backward compatibility
			Negated:      e.negated,
			PipeStage:    e.currentStage,
			LogicalOp:    e.lastLogicalOp,
			Alternatives: values, // All values in the IN list
			IsComputed:   isComputed,
			SourceField:  sourceField,
		}
		e.appendSourcedCondition(cond, ctx)
		e.lastLogicalOp = "AND"
	}
}

func (e *conditionExtractor) EnterComparisonExpression(ctx *ComparisonExpressionContext) {
	if !e.ownsDataflow(ctx) || e.inEvalCommand > 0 || e.inStatsFunction > 0 || e.inFunctionCall > 0 || e.scalarDepth > 0 ||
		predicateContextFor(ctx, e.budget) != PredicateEval {
		return
	}
	shape := classifyComparison(ctx, e.budget)
	if shape.kind == predicateAtom && ctx.Condition() == nil && ctx.ComparisonOp() != nil {
		operands := ctx.AllAdditiveExpression()
		if len(operands) != 2 {
			return
		}
		lhs := directAdditiveOperand(operands[0], e.budget)
		rhs := directAdditiveOperand(operands[1], e.budget)
		if lhs == nil || rhs == nil {
			return
		}
		lhs.Source, rhs.Source = e.sourceOf(operands[0]), e.sourceOf(operands[1])
		op := e.authoredComparisonOperator(ctx.ComparisonOp().GetText(), ctx.ComparisonOp().GetStart().GetStart(), operands[0].GetStop().GetStop(), operands[1].GetStart().GetStart())
		if lhs.Kind != OperandField {
			lhs, rhs, op = rhs, lhs, invertComparison(op)
		}
		if lhs.Kind != OperandField {
			return
		}
		field := lhs.Value
		if isExcludedField(strings.ToLower(field)) && !strings.HasPrefix(lhs.Source, "'") && !statsColumnNames[strings.ToLower(field)] {
			return
		}
		value, reference := rhs.Value, ""
		if rhs.Kind == OperandField {
			value, reference = "", rhs.Value
		}
		sourceField, computed := e.computedFields[field]
		e.appendSourcedCondition(Condition{Field: field, Context: PredicateEval, LHS: lhs, RHS: rhs,
			Operator: op, Value: value, ValueReference: reference, Negated: e.negated,
			PipeStage: e.currentStage, LogicalOp: e.lastLogicalOp, IsComputed: computed, SourceField: sourceField}, ctx)
		e.lastLogicalOp = "AND"
		return
	}
	if shape.kind != predicateScalar {
		return
	}
	source := e.sourceOf(ctx)
	if source == "" {
		return
	}
	e.budget.reserveOutput(1)
	scalar := &ScalarPredicate{Expression: source, Context: PredicateEval, PipeStage: e.currentStage}
	e.budget.step()
	e.conditionSources[ctx] = conditionSourceBinding{scalar: scalar}
	e.scalarRoots[ctx] = true
	e.scalarDepth++
}

func (e *conditionExtractor) ExitComparisonExpression(ctx *ComparisonExpressionContext) {
	if e.scalarRoots[ctx] {
		e.scalarDepth--
	}
}

func (e *conditionExtractor) sourceOf(ctx antlr.ParserRuleContext) string {
	if ctx == nil || ctx.GetStart() == nil || ctx.GetStop() == nil {
		return ""
	}
	start, stop := ctx.GetStart().GetStart(), ctx.GetStop().GetStop()
	e.budget.step()
	return e.sourceMap.source(start, stop)
}

func (e *conditionExtractor) extractComparisonOperator(ctx *ConditionContext) string {
	op := ctx.ComparisonOp().GetText()
	if op != "=" || ctx.FieldName() == nil || ctx.Value() == nil ||
		ctx.FieldName().GetStop() == nil || ctx.Value().GetStart() == nil {
		return op
	}
	left, right := ctx.FieldName().GetStop().GetStop(), ctx.Value().GetStart().GetStart()
	return e.authoredComparisonOperator(op, ctx.ComparisonOp().GetStart().GetStart(), left, right)
}

func (e *conditionExtractor) authoredComparisonOperator(op string, operatorStart, left, right int) string {
	if op != "=" {
		return op
	}
	if len(e.sourceMap.spans) > 0 {
		if e.sourceMap.source(operatorStart, operatorStart) == "==" {
			return "=="
		}
		return op
	}
	if len(e.sourceMap.spans) == 0 && left >= 0 && right > left && right <= len(e.originalQuery) {
		end := right
		if end < len(e.originalQuery) && e.originalQuery[end] == '=' {
			end++
		}
		if strings.Contains(e.originalQuery[left+1:end], "==") {
			return "=="
		}
	}
	return op
}

// EnterNotExpression tracks negation
func (e *conditionExtractor) EnterNotExpression(ctx *NotExpressionContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// ExitNotExpression resets negation
func (e *conditionExtractor) ExitNotExpression(ctx *NotExpressionContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// EnterSearchTerm handles NOT in search terms
func (e *conditionExtractor) EnterSearchTerm(ctx *SearchTermContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// ExitSearchTerm resets negation for search terms
func (e *conditionExtractor) ExitSearchTerm(ctx *SearchTermContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	if ctx.NOT() != nil {
		e.negated = !e.negated
	}
}

// EnterLogicalOp tracks the logical operator
func (e *conditionExtractor) EnterLogicalOp(ctx *LogicalOpContext) {
	if !e.ownsDataflow(ctx) {
		return
	}
	if ctx.OR() != nil {
		e.lastLogicalOp = "OR"
	} else {
		e.lastLogicalOp = "AND"
	}
}

// EnterOrExpression handles OR in where clauses
func (e *conditionExtractor) EnterOrExpression(_ *OrExpressionContext) {}

// extractValue gets the string value from a value context
func extractValue(ctx IValueContext) string {
	if ctx == nil {
		return ""
	}

	text := ctx.GetText()

	// Remove quotes if present
	if ctx.QUOTED_STRING() != nil {
		text = trimMatchingQuotes(text)
	}

	return text
}

func splValueIsFieldReference(ctx IValueContext, value string) bool {
	if ctx == nil || ctx.IDENTIFIER() == nil {
		return false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "true", "false", "null":
		return false
	}
	return true
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

// groupORConditionsByExpression groups only direct atomic siblings of an OR
// node. The index map always refers to the original raw condition indexes.
func groupORConditionsByExpression(conditions []Condition, expression *ConditionExpression, budget *parseBudget) ([]Condition, []int) {
	groupStart := make([]int, len(conditions))
	for i := range groupStart {
		budget.step()
		groupStart[i] = i
	}
	var visit func(*ConditionExpression)
	visit = func(node *ConditionExpression) {
		budget.step()
		if node == nil {
			return
		}
		if node.Operator == BooleanOr {
			start, previous := -1, -1
			for _, child := range node.Children {
				budget.step()
				if child == nil || child.Operator != BooleanCondition || child.ConditionIndex < 0 || child.ConditionIndex >= len(conditions) {
					start, previous = -1, -1
					continue
				}
				index := child.ConditionIndex
				if start >= 0 && index == previous+1 && sameConditionGroup(conditions[start], conditions[index]) {
					groupStart[index] = start
				} else {
					start = index
				}
				previous = index
			}
		}
		for _, child := range node.Children {
			budget.step()
			visit(child)
		}
	}
	visit(expression)

	// Accumulate each proven group once. Re-copying and de-duplicating the
	// growing prefix for every sibling makes ordinary long OR lists quadratic.
	type alternativesGroup struct {
		values []string
		items  []Operand
		typed  bool
		seen   map[string]struct{}
	}
	groups := make(map[int]*alternativesGroup)
	addValues := func(group *alternativesGroup, condition Condition) {
		values := condition.Alternatives
		if len(values) == 0 {
			values = []string{condition.Value}
		}
		for i, value := range values {
			budget.step()
			var item *Operand
			if condition.RHS != nil {
				if condition.RHS.Kind == OperandList && i < len(condition.RHS.Items) {
					item = &condition.RHS.Items[i]
				}
				if condition.RHS.Kind != OperandList && i == 0 {
					item = condition.RHS
				}
			}
			key := value
			if item != nil {
				key = operandIdentity(item, budget)
			}
			if _, exists := group.seen[key]; exists {
				continue
			}
			group.seen[key] = struct{}{}
			group.values = append(group.values, value)
			if item != nil {
				group.typed = true
				group.items = append(group.items, *cloneOperand(item, budget))
			}
		}
	}
	result := make([]Condition, 0, len(conditions))
	indexMap := make([]int, len(conditions))
	for i, condition := range conditions {
		budget.step()
		if start := groupStart[i]; start != i {
			group := groups[start]
			if group == nil {
				budget.step()
				group = &alternativesGroup{seen: make(map[string]struct{})}
				groups[start] = group
				addValues(group, result[indexMap[start]])
			}
			addValues(group, condition)
			indexMap[i] = indexMap[start]
			continue
		}
		indexMap[i] = len(result)
		condition.Alternatives = cloneConditionAlternatives(condition.Alternatives, budget)
		result = append(result, condition)
	}
	for start, group := range groups {
		budget.step()
		result[indexMap[start]].Alternatives = group.values
		if group.typed {
			budget.reserveOutput(1)
			result[indexMap[start]].RHS = &Operand{Kind: OperandList, Items: group.items}
		}
	}
	return result, indexMap
}

func sameConditionGroup(a, b Condition) bool {
	if a.Negated || b.Negated || a.IsComputed || b.IsComputed ||
		a.ValueReference != "" || b.ValueReference != "" || a.Field == "" || a.Operator == "" {
		return false
	}
	if a.Field != b.Field || a.Operator != b.Operator || a.PipeStage != b.PipeStage || a.SourceField != b.SourceField ||
		a.StageID != b.StageID || a.InputScopeID != b.InputScopeID || effectiveFieldBindingIdentity(a.FieldBinding, a.pendingFieldBinding) != effectiveFieldBindingIdentity(b.FieldBinding, b.pendingFieldBinding) {
		return false
	}
	if a.Context != b.Context || operandIdentity(a.LHS, nil) != operandIdentity(b.LHS, nil) ||
		!eligibleGroupedRHS(a.RHS) || !eligibleGroupedRHS(b.RHS) {
		return false
	}
	if strings.EqualFold(a.Operator, "like") {
		return likePatternKind(a.Value) == likePatternKind(b.Value)
	}
	return true
}

func eligibleGroupedRHS(rhs *Operand) bool {
	if rhs == nil {
		return true
	} // caller-created legacy conditions retain unknown provenance
	switch rhs.Kind {
	case OperandString, OperandNumber, OperandBare:
		return true
	case OperandList:
		for _, item := range rhs.Items {
			if !eligibleGroupedRHS(&item) || item.Kind == OperandList {
				return false
			}
		}
		return true
	}
	return false
}

func likePatternKind(pattern string) string {
	pattern = strings.ReplaceAll(pattern, "%", "*")
	start := strings.HasPrefix(pattern, "*")
	end := strings.HasSuffix(pattern, "*")
	switch {
	case start && end:
		return "contains"
	case start:
		return "endswith"
	case end:
		return "startswith"
	default:
		return "matches"
	}
}

func conditionDedupKey(cond Condition) string {
	var builder strings.Builder
	for _, part := range []string{cond.Field, cond.Operator, cond.Value, cond.ValueReference,
		string(cond.Context), operandIdentity(cond.LHS, nil), operandIdentity(cond.RHS, nil),
		boolKey(cond.Negated), boolKey(cond.IsComputed), cond.SourceField, cond.StageID, cond.InputScopeID, effectiveFieldBindingIdentity(cond.FieldBinding, cond.pendingFieldBinding)} {
		appendIdentityPart(&builder, part)
	}
	for _, value := range cond.Alternatives {
		appendIdentityPart(&builder, value)
	}
	return builder.String()
}

func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// DeduplicateConditions removes duplicate conditions, keeping the latest pipe stage
func DeduplicateConditions(conditions []Condition) []Condition {
	if len(conditions) == 0 {
		return conditions
	}

	// Keep exact field identity when choosing the latest compatibility stage.
	fieldConditions := make(map[string][]Condition)
	for _, cond := range conditions {
		// Skip pure wildcards
		if cond.Value == "*" && cond.Context == PredicateSearch && cond.RHS != nil && cond.RHS.Kind == OperandBare {
			continue
		}
		var fieldKey strings.Builder
		appendIdentityPart(&fieldKey, cond.Field)
		appendIdentityPart(&fieldKey, string(cond.Context))
		key := fieldKey.String()
		fieldConditions[key] = append(fieldConditions[key], cond)
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
				key := conditionDedupKey(cond)
				if !seen[key] {
					seen[key] = true
					cond.LHS = cloneOperand(cond.LHS, nil)
					cond.RHS = cloneOperand(cond.RHS, nil)
					cond.FieldBinding = cloneFieldBindingRef(cond.FieldBinding, nil)
					cond.Alternatives = append([]string(nil), cond.Alternatives...)
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
	if result == nil {
		return false
	}
	statisticalCommands := map[string]bool{
		"stats":       true,
		"eventstats":  true,
		"streamstats": true,
		"chart":       true,
		"timechart":   true,
		"tstats":      true,
		"mstats":      true,
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
	if result == nil {
		return false
	}
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
	if result == nil {
		return false
	}
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
	"chart": true, "timechart": true, "tstats": true, "mstats": true,
	"transaction": true, "dedup": true, "top": true, "rare": true,
}

// ClassifyPipelineStages parses a SPL query and returns metadata about each
// pipeline stage. This allows callers to make decisions based on stage type
// (e.g. stopping at aggregation stages) without brittle string splitting.
// Returns nil if parsing fails.
func ClassifyPipelineStages(query string) []PipelineStageInfo {
	result, _ := ClassifyPipelineStagesContext(context.Background(), query)
	return result
}

// ClassifyPipelineStagesContext reports cancellation and limit rejection.
// Like ClassifyPipelineStages, it returns nil stages and no error when the
// grammar cannot parse the whole query.
func ClassifyPipelineStagesContext(ctx context.Context, query string) ([]PipelineStageInfo, error) {
	return classifyPipelineStagesContext(ctx, query, defaultParseLimits())
}

func classifyPipelineStagesContext(ctx context.Context, query string, limits parseLimits) (result []PipelineStageInfo, err error) {
	defer func() {
		if value := recover(); value != nil {
			result = nil
			err = parseFailure(value)
		}
	}()
	budget, cancel := newParseBudget(ctx, limits)
	defer cancel()
	return classifyPipelineStagesInternal(query, budget), nil
}

func classifyPipelineStagesInternal(query string, budget *parseBudget) []PipelineStageInfo {
	budget.input(query)
	preflight(query, budget, false, nil)
	normalizedQuery, sourceMap := normalizeSPLQueryMapped(query, budget)
	budget.input(normalizedQuery)
	lexerErrors := &errorListener{budget: budget}
	stream := preflight(normalizedQuery, budget, true, lexerErrors)
	parser := NewSPLParser(&budgetTokenStream{CommonTokenStream: stream, budget: budget})
	parser.RemoveErrorListeners()
	parserErrors := &errorListener{budget: budget}
	parser.AddErrorListener(parserErrors)

	root := parser.Root()

	budget.check()
	if len(lexerErrors.errors) > 0 || len(parserErrors.errors) > 0 || hasUnparsedTail(root) {
		return nil
	}
	stages := root.Query().AllPipelineStage()
	budget.outputSize(len(stages))
	infos := make([]PipelineStageInfo, len(stages))

	for i, stage := range stages {
		budget.step()
		cmdType := classifyStage(stage)

		// Extract original text with whitespace from the query string using
		// ANTLR token positions, since GetText() strips whitespace. The
		// positions are runes of the normalized query; the source map
		// returns the authored bytes.
		originalText := stage.GetText()
		startToken := stage.GetStart()
		stopToken := stage.GetStop()
		if startToken != nil && stopToken != nil {
			if source := sourceMap.source(startToken.GetStart(), stopToken.GetStop()); source != "" {
				originalText = source
			}
		}

		infos[i] = PipelineStageInfo{
			Index:         i,
			CommandType:   cmdType,
			IsAggregation: aggregationCommands[cmdType],
			OriginalText:  originalText,
		}
	}

	budget.check()
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
	if search := stage.SearchCommand(); search != nil {
		// Search is implied only in the first stage; a later command
		// without a rule of its own parses as a search but is generic.
		if implicitPipelineCommand(search, nil) {
			return "generic"
		}
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
	if stage.TstatsCommand() != nil {
		return "tstats"
	}
	if stage.MstatsCommand() != nil {
		return "mstats"
	}
	if stage.InputlookupCommand() != nil {
		return "inputlookup"
	}
	if stage.GenericCommand() != nil {
		return "generic"
	}
	return "unknown"
}

// GetEventTypeFromConditions detects Windows Event types based on EventCode/EventID conditions
// Returns event type strings like "windows_4688", "sysmon_1", etc.
// Only positive equalities (=, ==, IN) count: negated conditions, != and range
// comparisons do not select events. When several EventCode or EventID
// conditions qualify, the first one wins.
func GetEventTypeFromConditions(result *ParseResult) string {
	if result == nil {
		return ""
	}
	var eventCode string
	var hasSysmon bool

	for _, cond := range result.Conditions {
		if cond.Negated || (cond.Operator != "=" && cond.Operator != "==" && cond.Operator != "in") {
			continue
		}
		fieldLower := strings.ToLower(cond.Field)

		// Check for EventCode or EventID
		if (fieldLower == "eventcode" || fieldLower == "eventid") && eventCode == "" {
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
