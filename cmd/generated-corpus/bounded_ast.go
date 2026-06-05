package main

import (
	crand "crypto/rand"
	"fmt"
	"math/big"
	"math/rand"
	"sort"
	"strconv"
	"strings"
)

const semanticMaxDepth = 3

var (
	semanticSPLFields = []string{
		"EventCode", "EventID", "Image", "ParentImage", "CommandLine", "TargetFilename",
		"DestinationIp", "SourceIp", "DestinationPort", "SourcePort", "QueryName",
		"ProcessId", "TargetUserName", "Account_Name", "user", "host", "src_ip",
		"dest_ip", "src_port", "dest_port", "status", "action", "method", "uri",
		"uri_path", "bytes", "duration", "response_time", "message", "level", "severity",
	}
	semanticSPLNumericFields = []string{
		"EventCode", "EventID", "DestinationPort", "SourcePort", "ProcessId",
		"src_port", "dest_port", "status", "bytes", "duration", "response_time",
	}
)

type semanticPicker interface {
	Intn(int) int
	Int63() int64
}

type seededSemanticPicker struct {
	rng *rand.Rand
}

func (p seededSemanticPicker) Intn(n int) int {
	return p.rng.Intn(n)
}

func (p seededSemanticPicker) Int63() int64 {
	return p.rng.Int63()
}

type cryptoSemanticPicker struct{}

func (cryptoSemanticPicker) Intn(n int) int {
	if n <= 0 {
		panic("semantic picker called with non-positive bound")
	}
	value, err := crand.Int(crand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(fmt.Sprintf("crypto random read failed: %v", err))
	}
	return int(value.Int64())
}

func (p cryptoSemanticPicker) Int63() int64 {
	const maxInt63 = int64(1<<63 - 1)
	value, err := crand.Int(crand.Reader, big.NewInt(maxInt63))
	if err != nil {
		panic(fmt.Sprintf("crypto random read failed: %v", err))
	}
	return value.Int64()
}

type semanticExpr struct {
	kind         string
	condition    expectedCondition
	left         *semanticExpr
	right        *semanticExpr
	child        *semanticExpr
	parenthesize bool
}

func generateBoundedSemanticCase(seed, id int64) generatedCase {
	var picker semanticPicker
	caseSeed := seedForCase(seed, id)
	if seed < 0 {
		picker = cryptoSemanticPicker{}
		caseSeed = picker.Int63()
	} else {
		picker = seededSemanticPicker{rng: rand.New(rand.NewSource(caseSeed))}
	}

	query, expected := generateSPLSemanticQuery(picker)
	return generatedCase{
		ID:       id,
		Seed:     caseSeed,
		Query:    query,
		Expected: expected,
	}
}

func generateSPLSemanticQuery(p semanticPicker) (string, []expectedCondition) {
	expr := generateSemanticExpr(p, 1+p.Intn(semanticMaxDepth))
	rendered := renderSPLSemanticExpr(p, expr)
	expected := semanticExpected(expr, false)

	switch p.Intn(7) {
	case 0:
		return "search " + rendered, expected
	case 1:
		return fmt.Sprintf("%s | table _time %s | sort - _time | head %d",
			rendered, strings.Join(semanticProjectionFields(expected), " "), 1+p.Intn(500)), expected
	case 2:
		groupField := semanticPickString(p, []string{"host", "user", "src_ip", "dest_ip", "Image"})
		return fmt.Sprintf("search %s | eval risk_score=bytes+duration | stats count as event_count by %s | sort - event_count | head %d",
			rendered, groupField, 1+p.Intn(250)), expected
	case 3:
		second := generateSemanticExpr(p, 1+p.Intn(semanticMaxDepth))
		query := fmt.Sprintf("search %s | where %s | table _time %s | dedup %d %s",
			rendered,
			renderSPLSemanticExpr(p, second),
			strings.Join(semanticProjectionFields(append(expected, semanticExpected(second, false)...)), " "),
			1+p.Intn(5),
			semanticPickString(p, []string{"host", "user", "src_ip", "dest_ip"}),
		)
		return query, append(expected, semanticExpected(second, false)...)
	case 4:
		sub := generateSemanticExpr(p, 1+p.Intn(semanticMaxDepth))
		joinField := semanticPickString(p, []string{"user", "src_ip", "dest_ip", "host"})
		query := fmt.Sprintf("search %s | join type=%s %s [ search %s | fields %s ] | table _time %s",
			rendered,
			semanticPickString(p, []string{"inner", "left", "outer"}),
			joinField,
			renderSPLSemanticExpr(p, sub),
			joinField,
			strings.Join(semanticProjectionFields(expected), " "),
		)
		return query, append(expected, semanticExpected(sub, false)...)
	case 5:
		sub := generateSemanticExpr(p, 1+p.Intn(semanticMaxDepth))
		query := fmt.Sprintf("search %s | append [ search %s | table %s ] | fields _time %s",
			rendered,
			renderSPLSemanticExpr(p, sub),
			strings.Join(semanticProjectionFields(semanticExpected(sub, false)), " "),
			strings.Join(semanticProjectionFields(expected), " "),
		)
		return query, append(expected, semanticExpected(sub, false)...)
	default:
		query := fmt.Sprintf("search (%s)\n| table _time %s\n| sort - _time\n| head %d",
			rendered, strings.Join(semanticProjectionFields(expected), " "), 1+p.Intn(300))
		query = strings.ReplaceAll(query, " AND ", "\n    AND ")
		query = strings.ReplaceAll(query, " OR ", "\n    OR ")
		return query, expected
	}
}

func generateSemanticExpr(p semanticPicker, depth int) *semanticExpr {
	if depth <= 0 {
		return &semanticExpr{kind: "predicate", condition: randomSemanticCondition(p), parenthesize: p.Intn(2) == 0}
	}
	switch p.Intn(5) {
	case 0:
		return &semanticExpr{kind: "predicate", condition: randomSemanticCondition(p), parenthesize: p.Intn(2) == 0}
	case 1:
		return &semanticExpr{kind: "and", left: generateSemanticExpr(p, depth-1), right: generateSemanticExpr(p, depth-1), parenthesize: p.Intn(3) != 0}
	case 2:
		return &semanticExpr{kind: "or", left: generateSemanticExpr(p, depth-1), right: generateSemanticExpr(p, depth-1), parenthesize: p.Intn(3) != 0}
	case 3:
		return &semanticExpr{kind: "not", child: generateSemanticExpr(p, depth-1), parenthesize: true}
	default:
		leftDepth := p.Intn(depth + 1)
		rightDepth := p.Intn(depth + 1)
		return &semanticExpr{kind: "and", left: generateSemanticExpr(p, leftDepth), right: generateSemanticExpr(p, rightDepth), parenthesize: true}
	}
}

func randomSemanticCondition(p semanticPicker) expectedCondition {
	field := semanticPickString(p, semanticSPLFields)
	if contains(semanticSPLNumericFields, field) {
		return expectedCondition{
			Field:    field,
			Operator: semanticPickString(p, []string{"=", "!=", ">", "<", ">=", "<="}),
			Value:    strconv.Itoa(1 + p.Intn(9000)),
		}
	}

	switch p.Intn(5) {
	case 0:
		return expectedCondition{
			Field:    field,
			Operator: semanticPickString(p, []string{"=", "!="}),
			Value:    semanticRandomValueForField(p, field),
		}
	case 1:
		values := semanticUniqueValues(p, semanticValuesForField(field), 2+p.Intn(3))
		return expectedCondition{Field: field, Operator: "in", Value: values[0], Alternatives: values, Negated: p.Intn(4) == 0}
	case 2:
		return expectedCondition{
			Field:    field,
			Operator: "like",
			Value:    semanticWildcardValueForField(p, field),
		}
	case 3:
		return expectedCondition{
			Field:    field,
			Operator: "matches",
			Value:    semanticPickString(p, []string{"(?i)admin", "powershell", "cmd\\.exe", "error|failure", "svc_.*"}),
		}
	default:
		return expectedCondition{
			Field:    field,
			Operator: "=",
			Value:    semanticRandomValueForField(p, field),
			Negated:  p.Intn(3) == 0,
		}
	}
}

func renderSPLSemanticExpr(p semanticPicker, expr *semanticExpr) string {
	switch expr.kind {
	case "predicate":
		return maybeSemanticParens(renderCondition(expr.condition), expr.parenthesize)
	case "and":
		return maybeSemanticParens(renderSPLSemanticExpr(p, expr.left)+" AND "+renderSPLSemanticExpr(p, expr.right), expr.parenthesize)
	case "or":
		return maybeSemanticParens(renderSPLSemanticExpr(p, expr.left)+" OR "+renderSPLSemanticExpr(p, expr.right), expr.parenthesize)
	case "not":
		return "NOT (" + renderSPLSemanticExpr(p, expr.child) + ")"
	default:
		panic("unknown semantic expression kind")
	}
}

func semanticExpected(expr *semanticExpr, negated bool) []expectedCondition {
	switch expr.kind {
	case "predicate":
		cond := expr.condition
		if negated {
			cond.Negated = !cond.Negated
		}
		return []expectedCondition{cond}
	case "and", "or":
		out := semanticExpected(expr.left, negated)
		out = append(out, semanticExpected(expr.right, negated)...)
		return out
	case "not":
		return semanticExpected(expr.child, !negated)
	default:
		panic("unknown semantic expression kind")
	}
}

func semanticProjectionFields(expected []expectedCondition) []string {
	seen := make(map[string]bool, len(expected))
	out := make([]string, 0, 5)
	for _, cond := range expected {
		if cond.Field == "" || seen[cond.Field] {
			continue
		}
		seen[cond.Field] = true
		out = append(out, cond.Field)
		if len(out) == 5 {
			return out
		}
	}
	for _, field := range []string{"host", "user", "src_ip", "dest_ip", "Image"} {
		if !seen[field] {
			out = append(out, field)
		}
		if len(out) == 5 {
			return out
		}
	}
	return out
}

func maybeSemanticParens(value string, enabled bool) string {
	if enabled {
		return "(" + value + ")"
	}
	return value
}

func semanticRandomValueForField(p semanticPicker, field string) string {
	values := semanticValuesForField(field)
	return semanticPickString(p, values)
}

func semanticWildcardValueForField(p semanticPicker, field string) string {
	value := semanticPickString(p, []string{"admin", "root", "SYSTEM", "web01", "failure", "critical", "powershell", "cmd.exe"})
	switch p.Intn(3) {
	case 0:
		return "*" + strings.Trim(value, "*") + "*"
	case 1:
		return strings.Trim(value, "*") + "*"
	default:
		return "*" + strings.Trim(value, "*")
	}
}

func semanticValuesForField(field string) []string {
	switch field {
	case "Image", "ParentImage":
		return []string{`C:\Windows\System32\cmd.exe`, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, `C:\Windows\System32\rundll32.exe`, `C:\Windows\System32\svchost.exe`}
	case "CommandLine":
		return []string{"-enc", "downloadstring", "whoami", "net user", " /c ", "Invoke-WebRequest"}
	case "TargetFilename", "uri", "uri_path":
		return []string{`C:\Users\Public\dropper.exe`, `C:\ProgramData\payload.dll`, "/admin/login", "/api/v1/token", "/owa/auth/logon.aspx"}
	case "DestinationIp", "SourceIp", "src_ip", "dest_ip":
		return []string{"10.0.0.5", "192.168.1.10", "172.16.1.20", "8.8.8.8", "203.0.113.10"}
	case "action":
		return []string{"allowed", "blocked", "created", "deleted", "quarantined"}
	case "status", "level", "severity":
		return []string{"success", "failure", "low", "medium", "high", "critical"}
	case "method":
		return []string{"GET", "POST", "PUT", "DELETE"}
	case "host":
		return []string{"web01", "dc01", "srv-app-01", "workstation-22"}
	case "user", "TargetUserName", "Account_Name":
		return []string{"admin", "root", "SYSTEM", "svc_app", "john.doe"}
	case "QueryName":
		return []string{"example.com", "update.microsoft.com", "pastebin.com", "malware.test"}
	default:
		return []string{"admin", "root", "SYSTEM", "svc_app", "web01", "test"}
	}
}

func semanticUniqueValues(p semanticPicker, pool []string, count int) []string {
	if count > len(pool) {
		count = len(pool)
	}
	seen := make(map[string]bool, count)
	out := make([]string, 0, count)
	for len(out) < count {
		value := semanticPickString(p, pool)
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func semanticPickString(p semanticPicker, values []string) string {
	return values[p.Intn(len(values))]
}
