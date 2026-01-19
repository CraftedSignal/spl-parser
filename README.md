# SPL Parser

[![Go Reference](https://pkg.go.dev/badge/github.com/craftedsignal/spl-parser.svg)](https://pkg.go.dev/github.com/craftedsignal/spl-parser)
[![Go Report Card](https://goreportcard.com/badge/github.com/craftedsignal/spl-parser)](https://goreportcard.com/report/github.com/craftedsignal/spl-parser)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A production-ready Go parser for Splunk Processing Language (SPL), built with ANTLR4. This parser extracts conditions, fields, and search terms from SPL queries used in Splunk Enterprise, Splunk Cloud, and Splunk SOAR.

## Features

- **Full SPL Grammar Support**: Parses complex SPL queries including subsearches, macros, and piped commands
- **Condition Extraction**: Extracts filter conditions with field names, operators, and values
- **Field Discovery**: Identifies all fields referenced in queries
- **Command Analysis**: Recognizes SPL commands like `search`, `where`, `eval`, `stats`, etc.
- **Error Recovery**: Graceful handling of malformed queries with detailed error reporting
- **High Performance**: Optimized for processing large volumes of queries

## Installation

```bash
go get github.com/craftedsignal/spl-parser
```

## Usage

### Basic Condition Extraction

```go
package main

import (
    "fmt"
    spl "github.com/craftedsignal/spl-parser"
)

func main() {
    query := `
        index=windows sourcetype=WinEventLog:Security EventCode=4624
        | where Logon_Type IN (2, 10)
        | stats count by user, src_ip
    `

    result := spl.ExtractConditions(query)

    fmt.Printf("Found %d conditions:\n", len(result.Conditions))
    for _, cond := range result.Conditions {
        fmt.Printf("  Field: %s, Operator: %s, Value: %s\n",
            cond.Field, cond.Operator, cond.Value)
    }

    if len(result.Errors) > 0 {
        fmt.Printf("Warnings: %v\n", result.Errors)
    }
}
```

### Output

```
Found 4 conditions:
  Field: index, Operator: =, Value: windows
  Field: sourcetype, Operator: =, Value: WinEventLog:Security
  Field: EventCode, Operator: =, Value: 4624
  Field: Logon_Type, Operator: IN, Value: (2, 10)
```

### Advanced Usage

```go
// Extract with full context
result := spl.ExtractConditions(query)

// Access extracted data
for _, cond := range result.Conditions {
    fmt.Printf("Condition: %s %s %s (negated: %v)\n",
        cond.Field, cond.Operator, cond.Value, cond.Negated)
}

// Get all fields
for _, field := range result.Fields {
    fmt.Printf("Field: %s\n", field)
}
```

## Supported SPL Features

| Feature | Status |
|---------|--------|
| search command | Supported |
| where clause | Supported |
| eval command | Supported |
| stats/chart/timechart | Supported |
| rex (regex extraction) | Supported |
| lookup | Supported |
| join/append | Supported |
| subsearch | Supported |
| Boolean operators | Supported |
| Comparison operators | Supported |
| Wildcards | Supported |
| Time modifiers | Supported |
| Field extraction | Supported |

## API Reference

### Types

```go
// ExtractionResult contains all extracted information from an SPL query
type ExtractionResult struct {
    Conditions []Condition  // Extracted filter conditions
    Fields     []string     // All field references
    Errors     []string     // Non-fatal parsing warnings
}

// Condition represents a single filter condition
type Condition struct {
    Field      string   // Field name being filtered
    Operator   string   // Comparison operator (=, !=, >, <, IN, etc.)
    Value      string   // Filter value
    Values     []string // Multiple values for 'IN' operator
    Negated    bool     // Whether condition is negated (NOT)
}
```

### Functions

```go
// ExtractConditions parses an SPL query and extracts all conditions
func ExtractConditions(query string) *ExtractionResult
```

## Performance

Benchmarks on a corpus of 195 real-world SPL queries:

| Metric | Value |
|--------|-------|
| Success Rate | 87% |
| Avg Parse Time | <1ms |
| Queries/Second | >10,000 |

## Grammar

This parser uses ANTLR4 with a comprehensive SPL grammar. The grammar files are included:

- `SPLLexer.g4` - Lexer rules
- `SPLParser.g4` - Parser rules

To regenerate the parser after grammar changes:

```bash
make generate
```

## Contributing

Contributions are welcome! Please ensure:

1. All tests pass: `make test`
2. Code is formatted: `make fmt`
3. Linter passes: `make lint`

## License

MIT License - see [LICENSE](LICENSE) for details.

## Related Projects

- [kql-parser](https://github.com/craftedsignal/kql-parser) - Kusto Query Language parser
- [CraftedSignal](https://craftedsignal.com) - Detection engineering platform
