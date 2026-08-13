package spl

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	LogicalOp    string   `json:"logical_op"`             // "AND" or "OR" connecting to previous condition
	Alternatives []string `json:"alternatives,omitempty"` // For OR conditions on same field
	IsComputed   bool     `json:"is_computed,omitempty"`  // True if field was created by eval/rex
	SourceField  string   `json:"source_field,omitempty"` // Original field before transformation (for computed fields)
}

// ParseResult contains all conditions extracted from the query
type ParseResult struct {
	Conditions     []Condition       `json:"conditions"`
	GroupByFields  []string          `json:"group_by_fields,omitempty"` // Fields from stats/eventstats/streamstats BY clauses
	ComputedFields map[string]string `json:"computed_fields,omitempty"` // Map of computed field name -> source field (from eval/rex)
	FieldAliases   map[string]string `json:"field_aliases,omitempty"`   // Map of new name -> original name (from rename)
	Commands       []string          `json:"commands,omitempty"`        // List of commands used in the query (stats, eventstats, etc.)
	Joins          []JoinInfo        `json:"joins,omitempty"`           // Extracted join/append info
	Subsearches    []*ParseResult    `json:"subsearches,omitempty"`     // Recursively parsed direct subsearches
	Errors         []string          `json:"errors,omitempty"`
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
	ExposedFields []string          `json:"exposed_fields,omitempty"` // Fields the subsearch makes available
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
	fieldAliases    map[string]string // Rename mappings: new name -> original name
	commands        []string          // Commands used in the query (stats, eventstats, etc.)
	joins           []JoinInfo        // Extracted join info
	subsearches     []*ParseResult    // Direct subsearch parse results
	currentStage    int
	inSubsearch     int // depth of subsearch nesting
	inFunctionCall  int // depth of function call nesting (eval, count, etc.)
	inStatsFunction int // depth of stats function nesting (count(), sum(), etc.)
	inEvalCommand   int // depth of eval command expressions; assignments compute fields, they do not filter events
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

func normalizeSPLQuery(query string) string {
	normalized := query
	normalized = normalizeSPLUnicodeWhitespace(normalized)
	normalized = stripSPLLineContinuations(normalized)
	normalized = stripSPLBacktickComments(normalized)
	normalized = normalizeSPLDoubleEquals(normalized)
	normalized = normalizeSPLBangFunctions(normalized)
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
	portableInConditionPattern         = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT|!)\s*)?((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s+IN(?::[A-Za-z]+)?\s*\(([^)]*)\)`)
	portableFunctionInConditionPattern = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT|!)\s*)?(?:lower|upper)\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)\s+IN(?::[A-Za-z]+)?\s*\(([^)]*)\)`)
	portableComparisonPattern          = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT|!)\s*)?((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*(contains|starts_with|matches|!=|>=|<=|=|>|<)\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:\([^)]*\))|(?:[^\s),\]]+))`)
	portableFunctionComparisonPattern  = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT|!)\s*)?(?:lower|upper)\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)\s*(contains|starts_with|matches|!=|>=|<=|=|>|<)\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:\([^)]*\))|(?:[^\s),\]]+))`)
	portableNetIPSubnetPattern         = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT|!)\s*)?net_ipsubnet\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*,\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:[^\s),\]]+))\s*\)`)
	portableCidrMatchPattern           = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT|!)\s*)?cidrmatch\s*\(\s*((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')|(?:[^\s),\]]+))\s*,\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)`)
	portableNullFunctionPattern        = regexp.MustCompile(`(?is)(^|[\s(])(?:(NOT|!)\s*)?(isnotnull|isnull)\s*\(\s*((?:[A-Za-z_][A-Za-z0-9_.{}\[\]-]*)|(?:[0-9]+))\s*\)`)
	quotedOptionValuePattern           = `(?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*')`
)

func extractPortableSPLConditions(query string) []Condition {
	normalized := normalizeSPLQuery(query)
	conditions := extractPortableSPLConditionsDepth(normalized, 0)
	if len(conditions) > 0 {
		return conditions
	}
	return nil
}

func extractPortableSPLConditionsDepth(normalized string, depth int) []Condition {
	if depth > 6 {
		return nil
	}

	var conditions []Condition
	for stage, segment := range splitSPLPipelineSegments(normalized) {
		text, ok := portablePredicateSegment(segment, stage == 0)
		if !ok {
			continue
		}
		conditions = append(conditions, extractPortablePredicatesFromText(text, stage)...)
	}
	conditions = append(conditions, extractLDAPSearchConditions(normalized)...)
	conditions = append(conditions, extractDatasetPowerQueryConditions(normalized)...)
	conditions = append(conditions, extractQuotedSearchOptionConditions(normalized, depth)...)
	conditions = append(conditions, extractSubsearchConditions(normalized, depth)...)
	return deduplicateConditions(conditions)
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
				segments = append(segments, query[start:i])
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

func extractPortablePredicatesFromText(text string, stage int) []Condition {
	text = stripBracketedSubsearches(text)
	var matches []portableConditionMatch
	seenSpans := make([][2]int, 0)
	for _, match := range portableInConditionPattern.FindAllStringSubmatchIndex(text, -1) {
		portableMatch := portableInMatch(text, match, stage)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}
	for _, match := range portableFunctionInConditionPattern.FindAllStringSubmatchIndex(text, -1) {
		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		portableMatch := portableInMatch(text, match, stage)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}
	for _, match := range portableNetIPSubnetPattern.FindAllStringSubmatchIndex(text, -1) {
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
		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		value := normalizePortableValue(text[match[6]:match[7]])
		field := normalizePortableField(text[match[8]:match[9]])
		if shouldSkipPortableField(field) || value == "" {
			continue
		}
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
		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		field := normalizePortableField(text[match[8]:match[9]])
		if shouldSkipPortableField(field) {
			continue
		}
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
		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		portableMatch := portableComparisonMatch(text, match, stage)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}
	for _, match := range portableFunctionComparisonPattern.FindAllStringSubmatchIndex(text, -1) {
		if spanOverlaps(match[0], match[1], seenSpans) {
			continue
		}
		portableMatch := portableComparisonMatch(text, match, stage)
		matches = append(matches, portableMatch)
		seenSpans = append(seenSpans, [2]int{portableMatch.start, portableMatch.end})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].start < matches[j].start
	})

	conditions := make([]Condition, 0, len(matches))
	for i, match := range matches {
		if match.cond.Field == "" || (match.cond.Value == "" && match.cond.Operator != "isnotnull" && match.cond.Operator != "isnull") {
			continue
		}
		if i == 0 {
			match.cond.LogicalOp = "AND"
		} else {
			match.cond.LogicalOp = portableLogicalOpBefore(text, match.start)
		}
		conditions = append(conditions, match.cond)
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

func scanBalancedGroupEnd(text string, start int, open, close byte) int {
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
		if balancedEnd := scanBalancedGroupEnd(text, open, '(', ')'); balancedEnd > listEnd {
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
	valueEnd := match[11]
	matchEnd := match[1]
	if match[10] >= 0 && match[10] < len(text) && text[match[10]] == '(' {
		if balancedEnd := scanBalancedGroupEnd(text, match[10], '(', ')'); balancedEnd > valueEnd {
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

func extractQuotedSearchOptionConditions(query string, depth int) []Condition {
	var conditions []Condition
	for _, literal := range extractQuotedOptionValues(query, "search") {
		value := decodeSPLStringLiteral(literal)
		trimmed := strings.TrimSpace(value)
		if !looksLikeEmbeddedSearch(trimmed) {
			continue
		}
		conditions = append(conditions, extractPortableSPLConditionsDepth(normalizeSPLQuery(trimmed), depth+1)...)
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

func extractDatasetPowerQueryConditions(query string) []Condition {
	var conditions []Condition
	for stage, segment := range splitSPLPipelineSegments(query) {
		lower := strings.ToLower(segment)
		if !strings.Contains(lower, "dataset") || !strings.Contains(lower, "method=powerquery") {
			continue
		}
		for _, literal := range extractQuotedOptionValues(segment, "search") {
			value := normalizeEmbeddedPredicateText(decodeSPLStringLiteral(literal))
			conditions = append(conditions, extractEmbeddedPredicateConditions(value, stage)...)
		}
	}
	return conditions
}

func extractEmbeddedPredicateConditions(text string, stage int) []Condition {
	var conditions []Condition
	segments := splitSPLPipelineSegments(text)
	for idx, segment := range segments {
		if predicateText, ok := embeddedPredicateSegment(segment, idx == 0); ok {
			conditions = append(conditions, extractPortablePredicatesFromText(predicateText, stage)...)
		}
	}
	if len(conditions) == 0 {
		conditions = append(conditions, extractParenthesizedPredicateBlockConditions(text, stage)...)
	}
	if len(conditions) == 0 && looksLikePortablePredicate(text) {
		conditions = append(conditions, extractPortablePredicatesFromText(text, stage)...)
	}
	return deduplicateConditions(conditions)
}

func extractParenthesizedPredicateBlockConditions(text string, stage int) []Condition {
	var conditions []Condition
	for _, block := range splitParenthesizedBlocks(text) {
		if !looksLikePortablePredicate(block) {
			continue
		}
		conditions = append(conditions, extractEmbeddedPredicateConditions(block, stage)...)
	}
	return deduplicateConditions(conditions)
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

func extractSubsearchConditions(query string, depth int) []Condition {
	if depth > 6 {
		return nil
	}
	var conditions []Condition
	for _, subsearch := range splitBracketedSubsearches(query) {
		conditions = append(conditions, extractPortableSPLConditionsDepth(normalizeSPLQuery(subsearch), depth+1)...)
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

func extractLDAPSearchConditions(query string) []Condition {
	var conditions []Condition
	for stage, segment := range splitSPLPipelineSegments(query) {
		if !strings.Contains(strings.ToLower(segment), "ldapsearch") {
			continue
		}
		for _, literal := range extractQuotedOptionValues(segment, "search") {
			filter := strings.TrimSpace(decodeSPLStringLiteral(literal))
			if !strings.HasPrefix(filter, "(") {
				continue
			}
			conditions = append(conditions, parseLDAPFilterConditions(filter, stage)...)
		}
	}
	return conditions
}

func extractQuotedOptionValues(text, optionName string) []string {
	pattern := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(optionName) + `\s*=\s*(` + quotedOptionValuePattern + `)`)
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

func parseLDAPFilterConditions(filter string, stage int) []Condition {
	parser := ldapFilterParser{text: filter, stage: stage}
	return deduplicateConditions(parser.parse())
}

type ldapFilterParser struct {
	text  string
	pos   int
	stage int
}

func (p *ldapFilterParser) parse() []Condition {
	p.skipSpace()
	conditions := p.parseFilter(false)
	return conditions
}

func (p *ldapFilterParser) parseFilter(negated bool) []Condition {
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
			conditions = append(conditions, child...)
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
		return []Condition{condition}
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

func extractCommandResourceConditions(query string) []Condition {
	var conditions []Condition
	for stage, segment := range splitSPLPipelineSegments(query) {
		trimmed := strings.TrimSpace(segment)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "inputlookup "):
			if value := firstCommandResourceArg(trimmed, "inputlookup"); value != "" {
				conditions = append(conditions, resourceCondition("_spl.inputlookup", value, stage))
			}
		case strings.HasPrefix(lower, "outputlookup "):
			if value := firstCommandResourceArg(trimmed, "outputlookup"); value != "" {
				conditions = append(conditions, resourceCondition("_spl.outputlookup", value, stage))
			}
		case strings.HasPrefix(lower, "collect "):
			if value := extractOptionValue(trimmed, "index"); value != "" {
				conditions = append(conditions, resourceCondition("_spl.collect.index", value, stage))
			}
			if value := extractOptionValue(trimmed, "source"); value != "" {
				conditions = append(conditions, resourceCondition("_spl.collect.source", value, stage))
			}
		case strings.HasPrefix(lower, "tstats "):
			if value := extractTstatsDatamodelValue(trimmed); value != "" {
				conditions = append(conditions, resourceCondition("_spl.datamodel", value, stage))
			}
		}
	}
	return deduplicateConditions(conditions)
}

func resourceCondition(field, value string, stage int) Condition {
	return Condition{
		Field:     field,
		Operator:  "=",
		Value:     normalizePortableValue(value),
		PipeStage: stage,
		LogicalOp: "AND",
	}
}

func firstCommandResourceArg(segment, command string) string {
	rest := strings.TrimSpace(segment[len(command):])
	for _, arg := range splitSPLArgs(rest) {
		if strings.Contains(arg, "=") {
			continue
		}
		return normalizePortableValue(arg)
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
		if c == ',' {
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

func deduplicateConditions(conditions []Condition) []Condition {
	seen := make(map[string]bool, len(conditions))
	out := make([]Condition, 0, len(conditions))
	for _, condition := range conditions {
		key := strings.ToLower(condition.Field) + "\x00" +
			strings.ToLower(condition.Operator) + "\x00" +
			condition.Value + "\x00" +
			fmt.Sprint(condition.Negated)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, condition)
	}
	return out
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

	normalizedQuery := normalizeSPLQuery(query)
	input := antlr.NewInputStream(normalizedQuery)
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
		fieldAliases:   make(map[string]string), // rename mappings: new name -> original name
		commands:       make([]string, 0),
		joins:          make([]JoinInfo, 0),
		subsearches:    make([]*ParseResult, 0),
		lastLogicalOp:  "AND", // default
		tokenStream:    stream,
		originalQuery:  normalizedQuery,
	}
	antlr.ParseTreeWalkerDefault.Walk(extractor, tree)

	// Combine errors
	allErrors := append(lexerErrors.errors, parserErrors.errors...)
	allErrors = append(allErrors, extractor.errors...)

	// Post-process to group OR conditions on same field
	conditions := groupORConditions(extractor.conditions)
	recoveredWithPortableExtraction := false
	if len(allErrors) > 0 {
		if portableConditions := extractPortableSPLConditions(query); len(portableConditions) > 0 {
			conditions = groupORConditions(portableConditions)
			recoveredWithPortableExtraction = true
		}
	} else if len(conditions) == 0 {
		if portableConditions := extractPortableSPLConditions(query); len(portableConditions) > 0 {
			conditions = groupORConditions(portableConditions)
		}
	}
	if len(conditions) == 0 {
		if resourceConditions := extractCommandResourceConditions(normalizedQuery); len(resourceConditions) > 0 {
			conditions = groupORConditions(resourceConditions)
			recoveredWithPortableExtraction = len(allErrors) > 0
		}
	}
	if recoveredWithPortableExtraction {
		allErrors = nil
	}

	return &ParseResult{
		Conditions:     conditions,
		GroupByFields:  extractor.groupByFields,
		ComputedFields: extractor.computedFields,
		FieldAliases:   extractor.fieldAliases,
		Commands:       extractor.commands,
		Joins:          extractor.joins,
		Subsearches:    extractor.subsearches,
		Errors:         allErrors,
	}
}

// ExitPipelineStage increments the stage counter after processing each stage
func (e *conditionExtractor) ExitPipelineStage(ctx *PipelineStageContext) {
	e.currentStage++
}

// EnterSubsearch tracks when we enter a subsearch
func (e *conditionExtractor) EnterSubsearch(ctx *SubsearchContext) {
	if e.inSubsearch == 0 {
		subText := strings.TrimSpace(e.extractSubsearchText(ctx))
		if subText != "" {
			e.subsearches = append(e.subsearches, ExtractConditions(subText))
		}
	}
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
	if e.inEvalCommand > 0 {
		e.inFunctionCall++
		return
	}
	if e.inStatsFunction > 0 {
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

	// Check for isnotnull(field) - extracts an exists condition
	if ctx.ISNOTNULL() != nil {
		args := ctx.ArgumentList()
		if args != nil {
			allArgs := args.AllExpression()
			if len(allArgs) >= 1 {
				field := allArgs[0].GetText()
				cond := Condition{
					Field:     field,
					Operator:  "isnotnull",
					Value:     "",
					Negated:   e.negated,
					PipeStage: e.currentStage,
					LogicalOp: e.lastLogicalOp,
				}
				e.conditions = append(e.conditions, cond)
				e.lastLogicalOp = "AND"
			}
		}
		return
	}

	// Check for isnull(field) - extracts a null check condition
	if ctx.ISNULL() != nil {
		args := ctx.ArgumentList()
		if args != nil {
			allArgs := args.AllExpression()
			if len(allArgs) >= 1 {
				field := allArgs[0].GetText()
				cond := Condition{
					Field:     field,
					Operator:  "isnull",
					Value:     "",
					Negated:   e.negated,
					PipeStage: e.currentStage,
					LogicalOp: e.lastLogicalOp,
				}
				e.conditions = append(e.conditions, cond)
				e.lastLogicalOp = "AND"
			}
		}
		return
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

// ExitStatsFunction tracks when we exit a stats function.
// It also registers any "AS alias" as a computed field so that post-aggregation
// filters (e.g. | where events > 5) are recognized as operating on computed fields.
func (e *conditionExtractor) ExitStatsFunction(ctx *StatsFunctionContext) {
	e.inStatsFunction--

	// If this stats function has an AS alias, register it as a computed field
	if ctx.AS() != nil && ctx.FieldName() != nil {
		alias := strings.ToLower(ctx.FieldName().GetText())
		// The source is the expression inside the function, or the function name itself
		sourceField := ""
		if ctx.Expression() != nil {
			sourceField = extractFirstFieldFromExpression(ctx.Expression())
		}
		if sourceField == "" {
			// For functions like count (no expression), use the function name as source marker
			sourceField = strings.ToLower(ctx.IDENTIFIER().GetText())
		}
		e.computedFields[alias] = sourceField
	}
}

// EnterTstatsCommand extracts group-by fields, datamodel reference, and commands from tstats
func (e *conditionExtractor) EnterTstatsCommand(ctx *TstatsCommandContext) {
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
	e.commands = append(e.commands, "inputlookup")
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
	e.inEvalCommand++
}

// ExitEvalCommand leaves eval command scope.
func (e *conditionExtractor) ExitEvalCommand(ctx *EvalCommandContext) {
	if e.inEvalCommand > 0 {
		e.inEvalCommand--
	}
}

// EnterWhereCommand tracks where commands
func (e *conditionExtractor) EnterWhereCommand(ctx *WhereCommandContext) {
	e.commands = append(e.commands, "where")
}

// EnterTransactionCommand tracks transaction commands and marks computed fields.
// The transaction command computes several fields that don't exist in raw events:
// - duration: seconds between first and last event in the transaction
// - eventcount: number of events in the transaction
// - closed_txn: 1 if the transaction was properly closed, 0 otherwise
// These should not be expected in test data as they're computed by the command.
func (e *conditionExtractor) EnterTransactionCommand(ctx *TransactionCommandContext) {
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

// EnterRenameCommand tracks rename commands: | rename OldField AS NewField
// Records field aliases so downstream consumers can resolve renamed fields.
func (e *conditionExtractor) EnterRenameCommand(ctx *RenameCommandContext) {
	e.commands = append(e.commands, "rename")

	if e.inSubsearch > 0 {
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
				newName = strings.Trim(spec.QUOTED_STRING().GetText(), "\"'")
			}
			if newName != "" && oldName != "" {
				e.fieldAliases[strings.ToLower(newName)] = oldName
				// Also track as computed field so downstream conditions
				// on the renamed field are marked as computed.
				e.computedFields[strings.ToLower(newName)] = oldName
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
		value = trimMatchingQuotes(value)

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

	// Eval assignments compute or transform fields; they are not predicates.
	if e.inEvalCommand > 0 {
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
		text = trimMatchingQuotes(text)
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
		if i+1 < len(conditions) && conditions[i+1].LogicalOp == "OR" && sameConditionGroup(cond, conditions[i+1]) {
			alternatives := conditionAlternatives(cond)

			j := i + 1
			for j < len(conditions) {
				next := conditions[j]
				if next.LogicalOp == "OR" && sameConditionGroup(cond, next) {
					alternatives = append(alternatives, conditionAlternatives(next)...)
					j++
				} else {
					break
				}
			}

			if len(alternatives) > 1 {
				cond.Alternatives = deduplicateConditionValues(alternatives)
				result = append(result, cond)
				i = j - 1 // skip the grouped conditions
				continue
			}
		}

		result = append(result, cond)
	}

	return result
}

func sameConditionGroup(a, b Condition) bool {
	if !strings.EqualFold(a.Field, b.Field) ||
		!strings.EqualFold(a.Operator, b.Operator) ||
		a.Negated != b.Negated ||
		a.IsComputed != b.IsComputed ||
		!strings.EqualFold(a.SourceField, b.SourceField) {
		return false
	}
	if strings.EqualFold(a.Operator, "like") {
		return likePatternKind(a.Value) == likePatternKind(b.Value)
	}
	return true
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

func conditionAlternatives(cond Condition) []string {
	if len(cond.Alternatives) > 0 {
		return append([]string(nil), cond.Alternatives...)
	}
	return []string{cond.Value}
}

func deduplicateConditionValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func conditionDedupKey(cond Condition) string {
	return strings.ToLower(cond.Field) + "|" +
		cond.Operator + "|" +
		cond.Value + "|" +
		boolKey(cond.Negated) + "|" +
		boolKey(cond.IsComputed) + "|" +
		strings.ToLower(cond.SourceField) + "|" +
		strings.Join(cond.Alternatives, "\x00")
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
				key := conditionDedupKey(cond)
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
	"chart": true, "timechart": true, "tstats": true, "mstats": true,
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
