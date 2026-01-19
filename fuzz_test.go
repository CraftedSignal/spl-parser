package spl

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// FuzzSPLParser tests the parser with random inputs
func FuzzSPLParser(f *testing.F) {
	// Seed corpus with valid SPL patterns
	seeds := []string{
		`index=main`,
		`index=main status=200`,
		`index=main | stats count by host`,
		`index=main | where status>200`,
		`index=main (status=200 OR status=404)`,
		`index=main NOT status=500`,
		`EventCode=4624 Logon_Type=10`,
		`index=sysmon EventCode=1 | table _time host process`,
		`index=main | eval x=len(field) | where x>10`,
		`index=main | rex field=_raw "(?<ip>\d+\.\d+\.\d+\.\d+)"`,
		`| tstats count where index=* by sourcetype`,
		`index=main | join type=left user [search index=users]`,
		`index=main | transaction user maxspan=30m`,
		`index=main status IN (200, 201, 204)`,
		`source="/var/log/*.log" error`,
		`index=main earliest=-24h latest=now`,
		`index=main | stats count avg(duration) max(bytes) by src_ip`,
		`index=main | timechart span=1h count by status`,
		`index=main | dedup 3 user sortby -_time`,
		`index=main | fillnull value=0 count`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, query string) {
		// Skip empty queries
		if len(query) == 0 {
			return
		}

		// Skip very long queries to avoid timeouts
		if len(query) > 10000 {
			return
		}

		// The parser should not panic on any input
		result := ExtractConditions(query)

		// Basic sanity checks - these should never fail
		if result == nil {
			t.Error("ExtractConditions returned nil")
		}

		// Conditions should be a valid slice (even if empty)
		if result.Conditions == nil {
			t.Error("Conditions slice is nil")
		}

		// Each condition should have required fields
		for i, cond := range result.Conditions {
			if cond.Field == "" {
				t.Errorf("Condition %d has empty field", i)
			}
			if cond.Operator == "" {
				t.Errorf("Condition %d has empty operator", i)
			}
		}
	})
}

// TestRandomSPLQueries generates and tests random SPL-like queries
func TestRandomSPLQueries(t *testing.T) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var passed, failed, panicked int
	var failures []string

	for i := 0; i < 10000; i++ {
		query := generateRandomQuery(rng)

		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked++
					failures = append(failures, "PANIC: "+query)
				}
			}()

			result := ExtractConditions(query)
			if len(result.Errors) > 0 {
				failed++
			} else {
				passed++
			}
		}()
	}

	t.Logf("=== Random Query Fuzzing Summary ===")
	t.Logf("Passed: %d", passed)
	t.Logf("Failed (parse errors): %d", failed)
	t.Logf("Panicked: %d", panicked)

	if panicked > 0 {
		t.Errorf("Parser panicked on %d queries", panicked)
		for _, f := range failures[:min(10, len(failures))] {
			t.Logf("  %s", f)
		}
	}
}

// TestMalformedQueries tests the parser with intentionally malformed inputs
func TestMalformedQueries(t *testing.T) {
	malformed := []string{
		// Empty and whitespace
		``,
		`   `,
		"\t\n\r",

		// Incomplete operators
		`index=`,
		`index`,
		`=value`,
		`field>`,
		`<value`,

		// Unbalanced brackets/parens
		`index=main (status=200`,
		`index=main status=200)`,
		`index=main [search`,
		`index=main | join [`,
		`((((`,
		`))))`,
		`[[[[`,
		`]]]]`,

		// Invalid characters
		`index=main @#$%`,
		`index=main 你好`,
		`index=main \x00\x01`,

		// Very long field names
		strings.Repeat("a", 1000) + `=value`,

		// Very long values
		`field="` + strings.Repeat("x", 10000) + `"`,

		// Deeply nested
		`((((((((((status=200))))))))))`,
		strings.Repeat("(", 100) + "x=1" + strings.Repeat(")", 100),

		// Many pipes
		`index=main` + strings.Repeat(" | stats count", 100),

		// SQL injection attempts (should parse as SPL, not execute)
		`index=main; DROP TABLE users;--`,
		`index=main' OR '1'='1`,
		`index=main" OR "1"="1`,

		// Regex edge cases
		`| rex "(?<field>.*)"`,
		`| rex "(((((((((("`,
		`| rex "[[[[[["`,

		// Numeric edge cases
		`field=999999999999999999999999999999`,
		`field=-999999999999999999999999999999`,
		`field=1e999`,
		`field=0.000000000000001`,

		// Time span edge cases
		`earliest=-999999999d`,
		`span=0s`,
		`span=-1h`,

		// Wildcard edge cases
		`field=*****`,
		`field=*.*.*`,
		`*=*`,

		// Quote edge cases
		`field="unclosed`,
		`field='unclosed`,
		`field="nested "quotes" here"`,
		`field='nested 'quotes' here'`,

		// Pipe edge cases
		`|`,
		`| |`,
		`|| |`,
		`index=main |`,
		`| | | |`,

		// Comment-like strings (SPL doesn't have comments)
		`index=main // comment`,
		`index=main /* comment */`,
		`index=main # comment`,

		// Boolean edge cases
		`AND`,
		`OR`,
		`NOT`,
		`AND OR NOT`,
		`index=main AND AND status=200`,
		`index=main OR OR status=200`,
		`NOT NOT NOT status=200`,

		// Function edge cases
		`| eval x=func()`,
		`| eval x=func(a,b,c,d,e,f,g,h,i,j)`,
		`| eval x=func(func(func(func())))`,
		`| stats count() count() count()`,

		// Field name edge cases
		`_=value`,
		`_time=value`,
		`123=value`,
		`field.with.dots=value`,
		`field-with-dashes=value`,
		`field:with:colons=value`,

		// IN operator edge cases
		`field IN ()`,
		`field IN (,,,)`,
		`field IN (1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20)`,

		// Subsearch edge cases
		`[[[search index=main]]]`,
		`index=main [search [search [search index=main]]]`,
	}

	var passed, failed, panicked int

	for _, query := range malformed {
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked++
					t.Errorf("PANIC on query: %q - %v", truncate(query, 100), r)
				}
			}()

			result := ExtractConditions(query)
			if result != nil {
				if len(result.Errors) > 0 {
					failed++
				} else {
					passed++
				}
			}
		}()
	}

	t.Logf("=== Malformed Query Test Summary ===")
	t.Logf("Parsed OK: %d", passed)
	t.Logf("Parse errors (expected): %d", failed)
	t.Logf("Panicked (bugs): %d", panicked)

	if panicked > 0 {
		t.Fail()
	}
}

// Helper functions

func generateRandomQuery(rng *rand.Rand) string {
	var parts []string

	// Random index clause
	if rng.Float32() < 0.8 {
		parts = append(parts, "index="+randomIdentifier(rng))
	}

	// Random field conditions
	numConditions := rng.Intn(5)
	for i := 0; i < numConditions; i++ {
		parts = append(parts, generateRandomCondition(rng))
	}

	query := strings.Join(parts, " ")

	// Random pipes
	numPipes := rng.Intn(4)
	for i := 0; i < numPipes; i++ {
		query += " | " + generateRandomCommand(rng)
	}

	return query
}

func generateRandomCondition(rng *rand.Rand) string {
	field := fuzzRandomField(rng)
	op := fuzzRandomOperator(rng)
	value := fuzzRandomValue(rng)

	// Sometimes wrap in NOT
	if rng.Float32() < 0.1 {
		return "NOT " + field + op + value
	}

	// Sometimes use IN
	if rng.Float32() < 0.1 {
		values := []string{fuzzRandomValue(rng), fuzzRandomValue(rng), fuzzRandomValue(rng)}
		return field + " IN (" + strings.Join(values, ", ") + ")"
	}

	// Sometimes wrap in parens with OR
	if rng.Float32() < 0.2 {
		return "(" + field + op + value + " OR " + field + op + fuzzRandomValue(rng) + ")"
	}

	return field + op + value
}

func generateRandomCommand(rng *rand.Rand) string {
	commands := []string{
		"stats count by " + fuzzRandomField(rng),
		"table " + fuzzRandomField(rng) + " " + fuzzRandomField(rng),
		"where " + fuzzRandomField(rng) + ">" + randomNumber(rng),
		"eval " + fuzzRandomField(rng) + "=len(" + fuzzRandomField(rng) + ")",
		"dedup " + fuzzRandomField(rng),
		"sort -" + fuzzRandomField(rng),
		"head 10",
		"tail 10",
		"rex field=" + fuzzRandomField(rng) + ` "(?<x>.*)"`,
		"rename " + fuzzRandomField(rng) + " AS " + fuzzRandomField(rng),
		"fields " + fuzzRandomField(rng),
		"fillnull value=0",
		"timechart span=1h count",
		"top " + fuzzRandomField(rng),
	}
	return commands[rng.Intn(len(commands))]
}

func randomIdentifier(rng *rand.Rand) string {
	identifiers := []string{
		"main", "sysmon", "windows", "linux", "security", "firewall",
		"web", "dns", "proxy", "auth", "endpoint", "network",
	}
	return identifiers[rng.Intn(len(identifiers))]
}

func fuzzRandomField(rng *rand.Rand) string {
	fields := []string{
		"status", "host", "src_ip", "dest_ip", "user", "action",
		"EventCode", "process", "CommandLine", "bytes", "duration",
		"src_port", "dest_port", "method", "uri", "response_time",
		"_time", "_raw", "sourcetype", "source", "index",
	}
	return fields[rng.Intn(len(fields))]
}

func fuzzRandomOperator(rng *rand.Rand) string {
	ops := []string{"=", "!=", ">", "<", ">=", "<="}
	return ops[rng.Intn(len(ops))]
}

func fuzzRandomValue(rng *rand.Rand) string {
	if rng.Float32() < 0.5 {
		return randomNumber(rng)
	}
	values := []string{
		`"success"`, `"failure"`, `"error"`, `"blocked"`, `"allowed"`,
		`"admin"`, `"root"`, `"system"`, `"*"`, `"*test*"`,
		"200", "404", "500", "1", "0",
	}
	return values[rng.Intn(len(values))]
}

func randomNumber(rng *rand.Rand) string {
	return string(rune('0'+rng.Intn(10))) + string(rune('0'+rng.Intn(10))) + string(rune('0'+rng.Intn(10)))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
