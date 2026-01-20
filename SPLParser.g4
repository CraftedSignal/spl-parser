parser grammar SPLParser;

options { tokenVocab=SPLLexer; }

// Entry point
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
    : STATS statsFunction (COMMA statsFunction)* (BY fieldList)?
    ;

statsFunction
    : IDENTIFIER LPAREN expression? RPAREN (AS fieldName)?  // count(), sum(bytes)
    | IDENTIFIER (AS fieldName)?                             // count, dc(distinct count) without parens
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

// Sort command
sortCommand
    : SORT NUMBER? sortField (COMMA sortField)*
    ;

sortField
    : (PLUS | MINUS)? fieldName
    ;

// Head command
headCommand
    : HEAD NUMBER?
    ;

// Tail command
tailCommand
    : TAIL NUMBER?
    ;

// Top command
topCommand
    : TOP NUMBER? fieldList (BY fieldList)?
    ;

// Rare command
rareCommand
    : RARE NUMBER? fieldList (BY fieldList)?
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
    : EVENTSTATS statsFunction (COMMA statsFunction)* (BY fieldList)?
    ;

// Streamstats command
streamstatsCommand
    : STREAMSTATS statsFunction (COMMA statsFunction)* (BY fieldList)?
    ;

// Timechart command
timechartCommand
    : TIMECHART (timechartOption)* statsFunction (BY fieldName)?
    ;

timechartOption
    : IDENTIFIER EQ (QUOTED_STRING | fieldName | NUMBER | TIME_SPAN)
    ;

// Chart command
chartCommand
    : CHART statsFunction (BY fieldList)? (OVER fieldName)?
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

// Bucket/bin command
bucketCommand
    : (BUCKET | BIN) fieldName (bucketOption)*
    ;

bucketOption
    : IDENTIFIER EQ (QUOTED_STRING | NUMBER | TIME_SPAN | IDENTIFIER)
    ;

// Generic command for unrecognized commands
genericCommand
    : IDENTIFIER genericArg*
    ;

genericArg
    : IDENTIFIER (EQ MINUS? (value | IDENTIFIER))?
    | MINUS? value
    | LPAREN genericArg* RPAREN
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
    | fieldName IN LPAREN valueList RPAREN
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

// Expression (for where clause and eval)
expression
    : orExpression
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
// NOTE: condition must come first to properly parse field=value in where clauses
comparisonExpression
    : condition  // Field conditions like field=value - must be first!
    | additiveExpression (comparisonOp additiveExpression)?
    ;

// Additive expressions: + -
additiveExpression
    : multiplicativeExpression ((PLUS | MINUS) multiplicativeExpression)*
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
    | functionCall
    | value
    | fieldName
    ;

// Function call
functionCall
    : IDENTIFIER LPAREN argumentList? RPAREN
    | MATCH LPAREN argumentList RPAREN
    | LIKE LPAREN argumentList RPAREN
    | CIDRMATCH LPAREN argumentList RPAREN
    ;

argumentList
    : expression (COMMA expression)*
    ;

// Values
value
    : QUOTED_STRING
    | NUMBER
    | TIME_SPAN       // Time values like -24h, 5m, 1d@d
    | wildcardValue   // Must come before others to match access*
    | colonValue      // Handle colon-separated values like o365:management:activity
    | IDENTIFIER
    ;

// Colon-separated values (common in SPL for sourcetypes, eventtypes, etc.)
colonValue
    : IDENTIFIER (COLON IDENTIFIER)+
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

// Bare word (search term - can be identifier, number, or quoted string)
bareWord
    : IDENTIFIER
    | NUMBER
    | QUOTED_STRING
    ;

// Field name
fieldName
    : IDENTIFIER
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
