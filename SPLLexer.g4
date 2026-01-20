lexer grammar SPLLexer;

// Keywords (case-insensitive)
AND         : [Aa][Nn][Dd] ;
OR          : [Oo][Rr] ;
NOT         : [Nn][Oo][Tt] ;
BY          : [Bb][Yy] ;
AS          : [Aa][Ss] ;
IN          : [Ii][Nn] ;
WHERE       : [Ww][Hh][Ee][Rr][Ee] ;
SEARCH      : [Ss][Ee][Aa][Rr][Cc][Hh] ;
EVAL        : [Ee][Vv][Aa][Ll] ;
STATS       : [Ss][Tt][Aa][Tt][Ss] ;
TABLE       : [Tt][Aa][Bb][Ll][Ee] ;
FIELDS      : [Ff][Ii][Ee][Ll][Dd][Ss] ;
RENAME      : [Rr][Ee][Nn][Aa][Mm][Ee] ;
REX         : [Rr][Ee][Xx] ;
DEDUP       : [Dd][Ee][Dd][Uu][Pp] ;
SORT        : [Ss][Oo][Rr][Tt] ;
HEAD        : [Hh][Ee][Aa][Dd] ;
TAIL        : [Tt][Aa][Ii][Ll] ;
TOP         : [Tt][Oo][Pp] ;
RARE        : [Rr][Aa][Rr][Ee] ;
LOOKUP      : [Ll][Oo][Oo][Kk][Uu][Pp] ;
JOIN        : [Jj][Oo][Ii][Nn] ;
APPEND      : [Aa][Pp][Pp][Ee][Nn][Dd] ;
TRANSACTION : [Tt][Rr][Aa][Nn][Ss][Aa][Cc][Tt][Ii][Oo][Nn] ;
SPATH       : [Ss][Pp][Aa][Tt][Hh] ;
EVENTSTATS  : [Ee][Vv][Ee][Nn][Tt][Ss][Tt][Aa][Tt][Ss] ;
STREAMSTATS : [Ss][Tt][Rr][Ee][Aa][Mm][Ss][Tt][Aa][Tt][Ss] ;
TIMECHART   : [Tt][Ii][Mm][Ee][Cc][Hh][Aa][Rr][Tt] ;
CHART       : [Cc][Hh][Aa][Rr][Tt] ;
FILLNULL    : [Ff][Ii][Ll][Ll][Nn][Uu][Ll][Ll] ;
MAKEMV      : [Mm][Aa][Kk][Ee][Mm][Vv] ;
MVEXPAND    : [Mm][Vv][Ee][Xx][Pp][Aa][Nn][Dd] ;
FORMAT      : [Ff][Oo][Rr][Mm][Aa][Tt] ;
CONVERT     : [Cc][Oo][Nn][Vv][Ee][Rr][Tt] ;
BUCKET      : [Bb][Uu][Cc][Kk][Ee][Tt] ;
BIN         : [Bb][Ii][Nn] ;
OVER        : [Oo][Vv][Ee][Rr] ;

// Comparison operators
EQ          : '=' ;
NEQ         : '!=' ;
LT          : '<' ;
GT          : '>' ;
LTE         : '<=' ;
GTE         : '>=' ;
LIKE        : [Ll][Ii][Kk][Ee] ;
MATCH       : [Mm][Aa][Tt][Cc][Hh] ;
CIDRMATCH   : [Cc][Ii][Dd][Rr][Mm][Aa][Tt][Cc][Hh] ;

// Delimiters
PIPE        : '|' ;
LPAREN      : '(' ;
RPAREN      : ')' ;
LBRACKET    : '[' ;
RBRACKET    : ']' ;
COMMA       : ',' ;
COLON       : ':' ;
DQUOTE      : '"' ;
PLUS        : '+' ;
MINUS       : '-' ;
SLASH       : '/' ;
PERCENT     : '%' ;

// String literals
QUOTED_STRING
    : '"' (~["\\\r\n] | '\\' .)* '"'
    | '\'' (~['\\\r\n] | '\\' .)* '\''
    ;

// Time span values (must be before NUMBER to match span=1h, -24h, etc.)
TIME_SPAN
    : '-'? [0-9]+ [smhdwMy]
    | '-'? [0-9]+ [smhdwMy] '@' [a-zA-Z]+
    ;

// Numbers (negative numbers handled in parser with MINUS token)
NUMBER
    : DIGIT+ ('.' DIGIT+)?
    | '.' DIGIT+
    ;

fragment DIGIT : [0-9] ;

// Wildcards and patterns
WILDCARD    : '*' ;

// Special pattern suffix (for Windows account patterns like *$)
DOLLAR      : '$' ;

// Field names and identifiers (allowing dots for nested fields)
IDENTIFIER
    : [a-zA-Z_] [a-zA-Z0-9_]*
    | [a-zA-Z_] [a-zA-Z0-9_]* ('.' [a-zA-Z_] [a-zA-Z0-9_]*)+
    ;

// Dot operator (must be after NUMBER and IDENTIFIER to not interfere with decimals and nested fields)
DOT         : '.' ;

// Backtick macros
MACRO
    : '`' (~[`])+ '`'
    ;

// Time alignment modifier (e.g., @d, @h)
TIME_MODIFIER
    : '@' [a-zA-Z]+
    ;

// Whitespace
WS          : [ \t\r\n]+ -> skip ;

// Line comments (useful for formatted queries)
LINE_COMMENT
    : '```' ~[\r\n]* -> skip
    ;
