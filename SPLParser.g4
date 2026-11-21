parser grammar SPLParser;

options { tokenVocab=SPLLexer; }

// Entry point: a query must span the whole input. subsearch reuses query
// inside brackets, so the end-of-input check lives in its own rule.
root
    : query EOF
    ;

// Queries can optionally start with | for generating commands like tstats, inputlookup
query
    : PIPE? pipelineStage (PIPE pipelineStage)*
    ;

// A single stage in the pipeline
pipelineStage
    : searchCommand
    | whereCommand
    | evalCommand
    | statsCommand
    | tableCommand
    | fieldsCommand
    | renameCommand
    | rexCommand
    | dedupCommand
    | sortCommand
    | headCommand
    | tailCommand
    | topCommand
    | rareCommand
    | lookupCommand
    | joinCommand
    | appendCommand
    | transactionCommand
    | spathCommand
    | eventstatsCommand
    | streamstatsCommand
    | timechartCommand
    | chartCommand
    | fillnullCommand
    | makemvCommand
    | mvexpandCommand
    | formatCommand
    | convertCommand
    | bucketCommand
    | restCommand
    | tstatsCommand
    | mstatsCommand
    | inputlookupCommand
    | genericCommand
    ;

// Search command (implicit or explicit)
searchCommand
    : SEARCH? searchExpression
    ;

// Where command
whereCommand
    : WHERE expression
    ;

// Eval command
evalCommand
    : EVAL evalAssignment (COMMA evalAssignment)*
    ;

evalAssignment
    : (fieldName | QUOTED_STRING) EQ expression
    ;

// Stats command
statsCommand
    : STATS statsFunction (COMMA? statsFunction)* (BY fieldList)?
    ;

statsFunction
    : IDENTIFIER LPAREN expression? RPAREN (AS (fieldName | QUOTED_STRING))?  // count(), sum(bytes)
    | IDENTIFIER (AS (fieldName | QUOTED_STRING))?                             // count, dc(distinct count) without parens
    ;

// Table command
tableCommand
    : TABLE fieldList
    ;

// Fields command
fieldsCommand
    : FIELDS (PLUS | MINUS)? fieldList
    ;

// Rename command
renameCommand
    : RENAME renameSpec (COMMA renameSpec)*
    ;

renameSpec
    : fieldName AS (fieldName | QUOTED_STRING)
    ;

// Rex command
rexCommand
    : REX (rexOption)* (QUOTED_STRING | fieldName EQ QUOTED_STRING)?
    ;

rexOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER)
    ;

// Dedup command
dedupCommand
    : DEDUP NUMBER? fieldList (dedupOption)*
    ;

dedupOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER)
    ;

// Sort command: sort [<count> | limit=<int>] <sort-by-clause>, ...
sortCommand
    : SORT (NUMBER | IDENTIFIER EQ NUMBER)? sortField (COMMA sortField)*
    ;

sortField
    : (PLUS | MINUS)? fieldName
    ;

// Head command: head [<N>] [limit=<int>] [null=<bool>] [keeplast=<bool>]
headCommand
    : HEAD NUMBER? headOption*
    ;

headOption
    : IDENTIFIER EQ (NUMBER | IDENTIFIER)
    ;

// Tail command
tailCommand
    : TAIL NUMBER?
    ;

// Top command: options such as limit=20 come before the fields, and may
// also follow the BY clause (top 1 productName by categoryId showperc=f)
topCommand
    : TOP NUMBER? topOption* fieldList (BY fieldList)? topOption*
    ;

topOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER)
    ;

// Rare command (same options as top, e.g. rare url limit=5)
rareCommand
    : RARE NUMBER? topOption* fieldList (BY fieldList)? topOption*
    ;

// Lookup command
lookupCommand
    : LOOKUP lookupOption* IDENTIFIER fieldList
    ;

lookupOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER)
    ;

// Join command with subsearch
joinCommand
    : JOIN joinOption* subsearch
    | JOIN joinOption* fieldList subsearch
    ;

joinOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER)
    ;

// Append command with subsearch
appendCommand
    : APPEND subsearch
    ;

// Transaction command
transactionCommand
    : TRANSACTION fieldList (transactionOption)*
    ;

transactionOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER | TIME_SPAN)
    ;

// Spath command
spathCommand
    : SPATH (spathOption)*
    ;

spathOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName)
    ;

// Eventstats command
eventstatsCommand
    : EVENTSTATS statsFunction (COMMA? statsFunction)* (BY fieldList)?
    ;

// Streamstats command: options such as window=10 or current=f come before the
// functions and may also follow them or the BY clause
streamstatsCommand
    : STREAMSTATS streamstatsOption* statsFunction (COMMA? statsFunction | streamstatsOption)* (BY fieldList streamstatsOption*)?
    ;

streamstatsOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER | TIME_SPAN)
    ;

// Timechart command: options such as span=1h before the functions, split-by
// options such as usenull=f after the BY field
timechartCommand
    : TIMECHART (timechartOption)* statsFunction (COMMA? statsFunction)* (BY fieldName timechartOption*)?
    ;

timechartOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER | TIME_SPAN)
    ;

// Chart command: chart <functions> [BY <row-split> <column-split>] or
// [OVER <row-split>] [BY <column-split>]
chartCommand
    : CHART statsFunction (COMMA? statsFunction)* (BY fieldList)? (OVER fieldName (BY fieldList)?)?
    ;

// Fillnull command
fillnullCommand
    : FILLNULL (fillnullOption)* fieldList?
    ;

fillnullOption
    : IDENTIFIER EQ (QUOTED_STRING | NUMBER)
    ;

// Makemv command
makemvCommand
    : MAKEMV (makemvOption)* fieldName
    ;

makemvOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER)
    ;

// Mvexpand command
mvexpandCommand
    : MVEXPAND fieldName
    ;

// Format command
formatCommand
    : FORMAT (formatOption)*
    ;

formatOption
    : IDENTIFIER EQ (QUOTED_STRING | NUMBER)
    ;

// Convert command
// Supports: convert timeformat="%Y-%m-%d" ctime(_time) AS date
convertCommand
    : CONVERT convertOption* convertFunction (COMMA convertFunction)*
    ;

convertOption
    : IDENTIFIER EQ (QUOTED_STRING | NUMBER | IDENTIFIER)
    ;

convertFunction
    : IDENTIFIER LPAREN fieldName RPAREN (AS (fieldName | QUOTED_STRING))?
    ;

// Bucket/bin command: bin [<bin-options>...] <field> [AS <newfield>]; the
// options may also follow the field (bin _time span=5m)
bucketCommand
    : (BUCKET | BIN) bucketOption* fieldName bucketOption* (AS fieldName bucketOption*)?
    ;

bucketOption
    : IDENTIFIER EQ (QUOTED_STRING | NUMBER | TIME_SPAN | IDENTIFIER)
    ;

// Rest command for Splunk REST API queries
// Syntax: | rest [options] <endpoint> [options]
// Example: | rest timeout=600 splunk_server=local /servicesNS/-/-/saved/searches count=0
restCommand
    : REST restArg*
    ;

restArg
    : IDENTIFIER EQ MINUS? (value | IDENTIFIER)   // option=value
    | REST_PATH                                    // REST API path like /servicesNS/-/-/saved/searches
    | IDENTIFIER                                   // endpoint without leading slash
    ;

// Tstats command for accelerated datamodel searches
tstatsCommand
    : TSTATS tstatsPreOption* (statsFunction (COMMA? statsFunction)*)?
      (FROM tstatsDatamodel)?
      (WHERE searchExpression)?
      ((BY | GROUPBY) (tstatsPostOption | fieldOrQuoted) (COMMA? (tstatsPostOption | fieldOrQuoted))*)?
    ;

tstatsPreOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER | TIME_SPAN)
    | MACRO
    ;

tstatsDatamodel
    : IDENTIFIER EQ IDENTIFIER (DOT IDENTIFIER)*
    | IDENTIFIER (COLON IDENTIFIER)? (DOT IDENTIFIER)*
    ;

tstatsPostOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER | TIME_SPAN)
    ;

// Mstats command for metrics-store queries
// Syntax: | mstats avg(_value) count(_value) WHERE metric_name="*.cpu.percent" by metric_name span=30s
mstatsCommand
    : MSTATS tstatsPreOption* (statsFunction (COMMA? statsFunction)*)?
      (WHERE searchExpression)?
      ((BY | GROUPBY) (tstatsPostOption | fieldOrQuoted)+)?
    ;

// Inputlookup command with optional where clause
// Syntax: | inputlookup [options] filename.csv [where condition]
inputlookupCommand
    : INPUTLOOKUP inputlookupOption* (IDENTIFIER | QUOTED_STRING) (WHERE expression)?
    ;

inputlookupOption
    : (IDENTIFIER | APPEND) EQ (QUOTED_STRING | fieldName | NUMBER)
    ;

// Generic command for unrecognized commands
genericCommand
    : IDENTIFIER genericArg*
    ;

genericArg
    : genericOptionName (EQ MINUS? (value | genericOptionName))?
    | MINUS? value
    | LPAREN genericArg* RPAREN
    ;

genericOptionName
    : IDENTIFIER
    | SEARCH
    ;

// Subsearch
subsearch
    : LBRACKET query RBRACKET
    ;

// Search expression (the filtering part)
// Terms can be connected by explicit AND/OR or implicitly (adjacent = AND)
searchExpression
    : searchTerm (logicalOp? searchTerm)*
    ;

searchTerm
    : NOT searchTerm
    | LPAREN searchExpression RPAREN
    | condition
    | subsearch
    | MACRO
    | bareWord
    ;

// Field conditions
condition
    : fieldName comparisonOp value
    | fieldName IN LPAREN valueList RPAREN      // field IN ("val1", "val2")
    | fieldName IN subsearch                     // field IN [search ...]
    | functionCall
    ;

// Comparison operators
comparisonOp
    : EQ
    | NEQ
    | LT
    | GT
    | LTE
    | GTE
    ;

// Logical operators (AND is implicit between terms)
logicalOp
    : AND
    | OR
    ;

// Expression (for where clause and eval); eval applies XOR after OR
expression
    : orExpression (XOR expression)?
    ;

orExpression
    : andExpression (OR andExpression)*
    ;

andExpression
    : notExpression (AND? notExpression)*
    ;

notExpression
    : NOT notExpression
    | comparisonExpression
    ;

// Comparison expression with arithmetic on both sides
// NOTE: condition must come before the arithmetic form to parse field=value in where clauses
comparisonExpression
    : fieldName comparisonOp functionCall  // user=lower(src_user), ahead of condition: not user=lower AND (src_user)
    | condition  // Field conditions like field=value
    | additiveExpression (comparisonOp additiveExpression)?
    ;

// Additive expressions: + - . (DOT is string concatenation in SPL)
additiveExpression
    : multiplicativeExpression ((PLUS | MINUS | DOT) multiplicativeExpression)*
    ;

// Multiplicative expressions: * / %
multiplicativeExpression
    : unaryExpression ((WILDCARD | SLASH | PERCENT) unaryExpression)*
    ;

// Unary expressions: - (negative)
unaryExpression
    : MINUS unaryExpression
    | primaryExpression
    ;

primaryExpression
    : LPAREN expression RPAREN
    | subsearch                   // Allow subsearch in expressions (e.g., where NOT [search ...])
    | functionCall
    | value
    | fieldName
    ;

// Function call
// Note: EVAL keyword is included because it can be used as a function inside stats (e.g., count(eval(status="200")))
functionCall
    : IDENTIFIER LPAREN argumentList? RPAREN
    | EVAL LPAREN argumentList? RPAREN
    | MATCH LPAREN argumentList RPAREN
    | LIKE LPAREN argumentList RPAREN
    | CIDRMATCH LPAREN argumentList RPAREN
    | ISNOTNULL LPAREN argumentList RPAREN
    | ISNULL LPAREN argumentList RPAREN
    ;

argumentList
    : expression (COMMA expression)*
    ;

// Values
value
    : QUOTED_STRING
    | NUMBER
    | DOTTED_NUMBER   // 10.0.0.1, 10.0.0.0/8
    | DIGIT_WORD      // 7z.exe, 0x1F
    | TIME_SPAN       // Time values like -24h, 5m, 1d@d
    | wildcardValue   // Must come before others to match access*
    | colonValue      // Handle colon-separated values like o365:management:activity
    | IDENTIFIER
    | keywordName     // action=search
    | bareValue       // Must stay last: os-linux, web-01, admin$
    ;

// Unquoted search value that runs on through - . $ and @, e.g. os-linux, web-01,
// john.doe@example.com, admin$, prod-*. The parser skips whitespace, so these
// parts also join across it. Being the last value alternative, it applies only
// where a shorter value cannot continue, so where/eval x=a-b keeps its parse.
bareValue
    : bareValuePart ((MINUS | DOT) bareValuePart | DOLLAR | TIME_MODIFIER)+
    ;

bareValuePart
    : IDENTIFIER
    | NUMBER
    | DOTTED_NUMBER
    | DIGIT_WORD
    | WILDCARD
    | keywordName
    ;

// Command and function names are plain words as a field name or a value
// (index=_audit action=search, | table title search). Operators and clause
// keywords (AND, OR, NOT, XOR, IN, AS, BY, OVER, FROM, GROUPBY) are not: Splunk
// requires quotation marks to search for them as values.
keywordName
    : SEARCH | WHERE | EVAL | STATS | TABLE | FIELDS | RENAME | REX | DEDUP | SORT
    | HEAD | TAIL | TOP | RARE | LOOKUP | JOIN | APPEND | TRANSACTION | SPATH
    | EVENTSTATS | STREAMSTATS | TIMECHART | CHART | FILLNULL | MAKEMV | MVEXPAND
    | FORMAT | CONVERT | BUCKET | BIN | REST | TSTATS | MSTATS | INPUTLOOKUP
    | LIKE | MATCH | CIDRMATCH | ISNOTNULL | ISNULL
    ;

// Colon-separated values (common in SPL for sourcetypes, eventtypes, etc.)
// Allows hyphens and slashes within parts, e.g., WinEventLog:Microsoft-Windows-PowerShell/Operational
colonValue
    : extendedIdentifier (COLON extendedIdentifier)+
    ;

// Extended identifier allows hyphens and slashes (common in Windows event sourcetypes)
extendedIdentifier
    : IDENTIFIER ((MINUS | SLASH) IDENTIFIER)*
    ;

// Wildcard values like *, access*, *access, *.php, *$
// NOTE: Does NOT support middle wildcards like access*log - those must be quoted.
// This prevents greedy matching that consumes the next field name.
wildcardValue
    : IDENTIFIER WILDCARD DOLLAR                                           // user*$ (Windows patterns)
    | IDENTIFIER WILDCARD                                                  // access* (trailing wildcard)
    | WILDCARD IDENTIFIER WILDCARD                                         // *access* (surrounded)
    | WILDCARD IDENTIFIER                                                  // *access (leading wildcard)
    | WILDCARD DOT IDENTIFIER                                              // *.php, *.log
    | WILDCARD DOLLAR                                                      // *$ (Windows service accounts)
    | WILDCARD                                                             // just *
    ;

// Bare word (search term - can be identifier, number, quoted string, or wildcard)
bareWord
    : IDENTIFIER
    | NUMBER
    | DOTTED_NUMBER
    | DIGIT_WORD
    | QUOTED_STRING
    | REST_PATH      // Unquoted fulltext path/glob, e.g. /var/log/*
    | wildcardValue  // Allows bare * or *value patterns as search terms
    ;

// Field name (NUMBER included for Sysmon-style numeric field names like 3=3)
// Supports hyphenated names (c-uri, cs-user-agent) via IDENTIFIER (MINUS IDENTIFIER)*
// Supports curly-brace fields (ModifiedProperties{}.NewValue) via LBRACE RBRACE
// Supports template variables (<<FIELD>>) from foreach command
fieldName
    : fieldNameBase (fieldNameSuffix (DOT fieldNameBase fieldNameSuffix?)*)?
    | NUMBER
    | TEMPLATE_VAR IDENTIFIER?     // <<FIELD>> or <<FIELD>>_pct
    ;

// Optional suffix on a field path segment: {} (curly brace), [*] (array wildcard), or [N] (array index)
fieldNameSuffix
    : LBRACE RBRACE                                // ModifiedProperties{}.NewValue
    | LBRACKET WILDCARD RBRACKET                    // targetResources[*].displayName
    | LBRACKET NUMBER RBRACKET                      // items[0].value
    ;

fieldNameBase
    : IDENTIFIER (MINUS IDENTIFIER)*
    | FROM
    | keywordName
    ;

// Field list (SPL allows both space-separated and comma-separated)
// Also allows quoted strings for field names with spaces
fieldList
    : fieldOrQuoted (COMMA? fieldOrQuoted)*
    ;

fieldOrQuoted
    : fieldName
    | QUOTED_STRING
    ;

// Value list (for IN operator)
valueList
    : value (COMMA value)*
    ;
