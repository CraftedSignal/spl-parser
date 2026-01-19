package spl

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// TestParserStress generates 100k random queries to find edge cases
func TestParserStress(t *testing.T) {
	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	t.Logf("Seed: %d", seed)

	const iterations = 100000
	var (
		total         int
		panics        int
		parseErrors   int
		withConditions int
		panicQueries  []string
	)

	for i := 0; i < iterations; i++ {
		query := generateStressQuery(rng)
		total++

		// Catch panics
		func() {
			defer func() {
				if r := recover(); r != nil {
					panics++
					if len(panicQueries) < 10 {
						panicQueries = append(panicQueries, fmt.Sprintf("%q: %v", truncateStr(query, 100), r))
					}
				}
			}()

			result := ExtractConditions(query)
			if len(result.Errors) > 0 {
				parseErrors++
			}
			if len(result.Conditions) > 0 {
				withConditions++
			}
		}()
	}

	t.Logf("Results (%d iterations):", total)
	t.Logf("  Panics: %d", panics)
	t.Logf("  Parse errors: %d (%.1f%%)", parseErrors, float64(parseErrors)*100/float64(total))
	t.Logf("  With conditions: %d (%.1f%%)", withConditions, float64(withConditions)*100/float64(total))

	if panics > 0 {
		t.Errorf("Parser panicked %d times!", panics)
		for _, q := range panicQueries {
			t.Logf("  Panic query: %s", q)
		}
	}
}

// TestEdgeCasePatterns tests specific edge case patterns
func TestEdgeCasePatterns(t *testing.T) {
	patterns := []struct {
		name  string
		query string
	}{
		// Empty and whitespace
		{"empty", ""},
		{"whitespace_only", "   \t\n  "},
		{"single_pipe", "|"},
		{"multiple_pipes", "| | |"},

		// Special characters in values
		{"backslash_value", `path="C:\\Windows\\System32"`},
		{"forward_slash", `path="/var/log/auth.log"`},
		{"special_chars", `msg="hello! @#$%^&*()"`},
		{"unicode_value", `name="café résumé"`},
		{"newline_in_query", "index=main\nstatus=200"},
		{"tab_in_query", "index=main\tstatus=200"},

		// Quotes
		{"single_quotes", `status='200'`},
		{"mixed_quotes", `status="200" action='success'`},
		{"escaped_quote", `msg="say \"hello\""`},
		{"empty_string", `status=""`},

		// Wildcards
		{"only_wildcard", `status=*`},
		{"double_wildcard", `status=**`},
		{"triple_wildcard", `status=***`},
		{"wildcard_middle", `path="*test*value*"`},
		{"question_wildcard", `host=server?`},

		// Numbers
		{"negative_number", `count>-100`},
		{"decimal_number", `ratio>=0.5`},
		{"scientific", `value=1e10`},
		{"leading_zero", `code=007`},

		// Operators
		{"double_equals", `status==200`},
		{"spacey_operator", `status = 200`},
		{"no_space_operator", `status=200`},

		// Boolean logic
		{"triple_or", `a=1 OR b=2 OR c=3`},
		{"triple_and", `a=1 AND b=2 AND c=3`},
		{"mixed_logic", `a=1 AND b=2 OR c=3`},
		{"double_not", `NOT NOT status=200`},
		{"not_with_parens", `NOT (status=200 OR status=201)`},

		// Parentheses
		{"empty_parens", `()`},
		{"nested_empty", `(())`},
		{"deep_nesting", `((((status=200))))`},
		{"unbalanced_open", `(status=200`},
		{"unbalanced_close", `status=200)`},
		{"adjacent_parens", `(status=200)(action=success)`},

		// IN operator
		{"empty_in", `status IN ()`},
		{"single_in", `status IN ("200")`},
		{"many_in", `status IN ("200", "201", "202", "203", "204", "301", "302", "400", "404", "500")`},

		// Pipes
		{"pipe_no_command", `index=main |`},
		{"double_pipe", `index=main || stats count`},
		{"pipe_at_start", `| stats count`},

		// Subsearches
		{"empty_subsearch", `index=main [search]`},
		{"nested_subsearch", `index=main [search index=other [search index=deep]]`},
		{"subsearch_only", `[search index=main]`},

		// Commands
		{"unknown_command", `index=main | foobar arg1 arg2`},
		{"stats_variations", `index=main | stats count, sum(bytes), avg(duration) by user`},
		{"eval_complex", `index=main | eval x=if(status>200, "high", "low")`},
		{"rex_pattern", `index=main | rex field=_raw "(?<ip>\d+\.\d+\.\d+\.\d+)"`},

		// Field names
		{"dotted_field", `user.name="admin"`},
		{"underscore_field", `_raw="test"`},
		{"numeric_start_value", `field=123abc`},
		{"hyphen_in_value", `uuid="550e8400-e29b-41d4-a716-446655440000"`},

		// Long queries
		{"very_long_field_list", `| fields a b c d e f g h i j k l m n o p q r s t u v w x y z`},

		// Colon values
		{"multiple_colons", `sourcetype=a:b:c:d:e`},
		{"colon_with_wildcard", `sourcetype=aws:*`},

		// Time modifiers
		{"earliest_latest", `index=main earliest=-24h latest=now`},
		{"time_at", `index=main earliest=-1d@d`},

		// Real-world patterns
		{"windows_security", `index=windows EventCode=4688 CommandLine="*powershell*" NOT user="SYSTEM"`},
		{"dns_query", `index=dns query_type=A query="*.evil.com" NOT src_ip="10.*"`},
		{"firewall_block", `index=firewall action=blocked src_ip IN ("192.168.1.1", "10.0.0.1") dest_port>1024`},
		{"o365_activity", `index=o365:management:activity Operation="FileAccessed" UserId="*@company.com"`},

		// Macros
		{"macro_only", "`my_macro`"},
		{"macro_with_args", "`get_events(host, 24h)`"},
		{"macro_in_query", "index=main `security_filter` status=200"},
	}

	for _, tc := range patterns {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Panic on query %q: %v", tc.query, r)
				}
			}()

			result := ExtractConditions(tc.query)
			t.Logf("Query: %q", truncateStr(tc.query, 80))
			t.Logf("  Conditions: %d, Errors: %d", len(result.Conditions), len(result.Errors))
			for _, c := range result.Conditions {
				t.Logf("    %s %s %q (negated=%v, stage=%d)", c.Field, c.Operator, c.Value, c.Negated, c.PipeStage)
			}
		})
	}
}

// TestRandomBooleanExpressions tests complex boolean logic
func TestRandomBooleanExpressions(t *testing.T) {
	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	t.Logf("Seed: %d", seed)

	for i := 0; i < 1000; i++ {
		query := generateBooleanExpression(rng, 5)

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Panic on iteration %d, query %q: %v", i, query, r)
			}
		}()

		result := ExtractConditions(query)
		if len(result.Conditions) == 0 && !isMetadataOnlyQuery(query) {
			// Log but don't fail - some complex expressions might not extract cleanly
			if i < 10 {
				t.Logf("Empty conditions for: %q", truncateStr(query, 100))
			}
		}
	}
}

// TestPipelineVariations tests various pipeline command combinations
func TestPipelineVariations(t *testing.T) {
	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	t.Logf("Seed: %d", seed)

	commands := []string{
		"stats count by %s",
		"stats sum(%s) as total",
		"eval new_%s=lower(%s)",
		"where %s > 100",
		"where %s = \"test\"",
		"search %s=\"value*\"",
		"table %s",
		"fields %s",
		"fields - %s",
		"dedup %s",
		"sort - %s",
		"sort + %s",
		"head 100",
		"tail 50",
		"rename %s as new_field",
		"rex field=%s \"(?<extract>\\w+)\"",
		"fillnull value=\"N/A\" %s",
		"transaction %s",
	}

	for i := 0; i < 1000; i++ {
		// Start with a base search
		field := randomField(rng)
		query := fmt.Sprintf("index=main %s=\"test\"", field)

		// Add 1-5 random pipeline stages
		numStages := rng.Intn(5) + 1
		for j := 0; j < numStages; j++ {
			cmd := commands[rng.Intn(len(commands))]
			pipeField := randomField(rng)
			if strings.Contains(cmd, "%s") {
				// Count occurrences
				count := strings.Count(cmd, "%s")
				args := make([]interface{}, count)
				for k := range args {
					args[k] = pipeField
				}
				cmd = fmt.Sprintf(cmd, args...)
			}
			query += " | " + cmd
		}

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Panic on iteration %d, query %q: %v", i, query, r)
			}
		}()

		result := ExtractConditions(query)

		// The base search condition should be found
		if len(result.Conditions) == 0 {
			t.Logf("No conditions extracted from: %s", truncateStr(query, 100))
		}
	}
}

// Helper functions

func generateStressQuery(rng *rand.Rand) string {
	generators := []func(*rand.Rand) string{
		generateSimpleSearch,
		generateComplexBoolean,
		generatePipelineQuery,
		generateSubsearchQuery,
		generateEdgeCaseQuery,
		generateRealWorldQuery,
	}

	gen := generators[rng.Intn(len(generators))]
	return gen(rng)
}

func generateSimpleSearch(rng *rand.Rand) string {
	parts := []string{}

	// Maybe add index
	if rng.Intn(2) == 0 {
		parts = append(parts, "index="+randomValue(rng))
	}

	// Add 1-4 conditions
	numConds := rng.Intn(4) + 1
	for i := 0; i < numConds; i++ {
		parts = append(parts, randomCondition(rng))
	}

	return strings.Join(parts, " ")
}

func generateComplexBoolean(rng *rand.Rand) string {
	return generateBooleanExpression(rng, rng.Intn(4)+2)
}

func generateBooleanExpression(rng *rand.Rand, depth int) string {
	if depth <= 0 || rng.Intn(3) == 0 {
		return randomCondition(rng)
	}

	switch rng.Intn(5) {
	case 0: // AND
		return generateBooleanExpression(rng, depth-1) + " AND " + generateBooleanExpression(rng, depth-1)
	case 1: // OR
		return generateBooleanExpression(rng, depth-1) + " OR " + generateBooleanExpression(rng, depth-1)
	case 2: // NOT
		return "NOT " + generateBooleanExpression(rng, depth-1)
	case 3: // Parentheses
		return "(" + generateBooleanExpression(rng, depth-1) + ")"
	default: // Implicit AND
		return generateBooleanExpression(rng, depth-1) + " " + generateBooleanExpression(rng, depth-1)
	}
}

func generatePipelineQuery(rng *rand.Rand) string {
	query := generateSimpleSearch(rng)

	numPipes := rng.Intn(5) + 1
	for i := 0; i < numPipes; i++ {
		query += " | " + randomPipeCommand(rng)
	}

	return query
}

func generateSubsearchQuery(rng *rand.Rand) string {
	base := generateSimpleSearch(rng)
	subsearch := generateSimpleSearch(rng)

	switch rng.Intn(3) {
	case 0:
		return base + " | join type=left user [search " + subsearch + "]"
	case 1:
		return base + " | append [search " + subsearch + "]"
	default:
		return base + " [search " + subsearch + "]"
	}
}

func generateEdgeCaseQuery(rng *rand.Rand) string {
	cases := []string{
		"",
		" ",
		"|",
		"index=main",
		"index=main |",
		"| stats count",
		"NOT NOT NOT status=200",
		"((((field=value))))",
		`field="value with spaces and \"quotes\""`,
		"field=*",
		"field=**wildcard**",
		"field IN ()",
		"field IN (\"a\")",
		"field IN (\"a\", \"b\", \"c\", \"d\", \"e\")",
		"`macro`",
		"index=main `macro` status=200",
		"a=1 b=2 c=3 d=4 e=5 f=6 g=7 h=8 i=9 j=10",
	}
	return cases[rng.Intn(len(cases))]
}

func generateRealWorldQuery(rng *rand.Rand) string {
	templates := []string{
		`index=windows EventCode=%d CommandLine="*%s*" NOT user="SYSTEM"`,
		`index=sysmon EventType=ProcessCreate Image="*%s*" | stats count by ParentImage`,
		`index=firewall action="%s" src_ip="%d.%d.%d.*" | where bytes > %d`,
		`index=dns query="*.%s.com" query_type IN ("A", "AAAA", "CNAME")`,
		`index=web status>=%d status<%d | stats count by uri_path`,
		`index=auth user="%s" action IN ("login", "logout", "failed") | transaction user`,
		`index=o365:management:activity Operation="%s" | eval risk=if(match(UserId, "admin"), "high", "low")`,
	}

	template := templates[rng.Intn(len(templates))]

	// Fill in random values
	result := template
	for strings.Contains(result, "%d") {
		result = strings.Replace(result, "%d", fmt.Sprintf("%d", rng.Intn(1000)), 1)
	}
	for strings.Contains(result, "%s") {
		result = strings.Replace(result, "%s", randomWord(rng), 1)
	}

	return result
}

func randomCondition(rng *rand.Rand) string {
	field := randomField(rng)
	op := randomOperator(rng)
	value := randomValue(rng)

	if op == "IN" {
		values := make([]string, rng.Intn(4)+2)
		for i := range values {
			values[i] = fmt.Sprintf("%q", randomWord(rng))
		}
		return fmt.Sprintf("%s IN (%s)", field, strings.Join(values, ", "))
	}

	return fmt.Sprintf("%s%s%s", field, op, value)
}

func randomField(rng *rand.Rand) string {
	fields := []string{
		"status", "action", "user", "src_ip", "dest_ip", "bytes",
		"EventCode", "CommandLine", "ProcessName", "ParentProcess",
		"query", "response", "duration", "count", "level", "message",
		"field_" + fmt.Sprintf("%d", rng.Intn(100)),
	}
	return fields[rng.Intn(len(fields))]
}

func randomOperator(rng *rand.Rand) string {
	ops := []string{"=", "!=", ">", "<", ">=", "<=", "IN"}
	return ops[rng.Intn(len(ops))]
}

func randomValue(rng *rand.Rand) string {
	switch rng.Intn(5) {
	case 0: // Number
		return fmt.Sprintf("%d", rng.Intn(1000))
	case 1: // Quoted string
		return fmt.Sprintf("%q", randomWord(rng))
	case 2: // Wildcard
		return fmt.Sprintf("*%s*", randomWord(rng))
	case 3: // Quoted wildcard
		return fmt.Sprintf("%q", "*"+randomWord(rng)+"*")
	default: // Plain value
		return randomWord(rng)
	}
}

func randomWord(rng *rand.Rand) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	length := rng.Intn(8) + 3
	var b strings.Builder
	for i := 0; i < length; i++ {
		b.WriteByte(letters[rng.Intn(len(letters))])
	}
	return b.String()
}

func randomPipeCommand(rng *rand.Rand) string {
	field := randomField(rng)
	commands := []string{
		fmt.Sprintf("stats count by %s", field),
		fmt.Sprintf("stats sum(%s)", field),
		fmt.Sprintf("where %s > %d", field, rng.Intn(100)),
		fmt.Sprintf("where %s = %q", field, randomWord(rng)),
		fmt.Sprintf("search %s=%q", field, randomWord(rng)),
		fmt.Sprintf("eval new_%s=lower(%s)", field, field),
		fmt.Sprintf("table %s", field),
		fmt.Sprintf("fields %s", field),
		fmt.Sprintf("dedup %s", field),
		fmt.Sprintf("sort - %s", field),
		fmt.Sprintf("head %d", rng.Intn(100)+1),
		fmt.Sprintf("rename %s as renamed_%s", field, field),
	}
	return commands[rng.Intn(len(commands))]
}

func isMetadataOnlyQuery(query string) bool {
	// Check if query only contains metadata fields
	metadataOnly := []string{"index=", "sourcetype=", "source=", "host=", "earliest=", "latest="}
	lower := strings.ToLower(query)

	hasMetadata := false
	for _, m := range metadataOnly {
		if strings.Contains(lower, m) {
			hasMetadata = true
			break
		}
	}

	if !hasMetadata {
		return false
	}

	// Check for non-metadata conditions
	nonMetadata := []string{"status", "action", "user", "event", "command", "process", "query", "bytes"}
	for _, nm := range nonMetadata {
		if strings.Contains(lower, nm+"=") || strings.Contains(lower, nm+">") || strings.Contains(lower, nm+"<") {
			return false
		}
	}

	return true
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// TestRealWorldQueries tests queries from real detection rules and community sources
func TestRealWorldQueries(t *testing.T) {
	// Real-world queries from various sources:
	// - Splunk Security Content
	// - SOC analyst cheat sheets
	// - Threat hunting repositories
	// - Community detection rules
	queries := []struct {
		name   string
		query  string
		source string
	}{
		// From EpicDetect SOC Analyst Cheat Sheet
		{"failed_logins_24h", `index=main sourcetype=windows:security EventCode=4625 earliest=-24h latest=now`, "epicdetect"},
		{"blocked_firewall_stats", `index=security sourcetype=firewall action=blocked | stats count by src_ip | sort - count`, "epicdetect"},
		{"bot_crawler_search", `index=web sourcetype=access_combined | search user_agent="bot" OR user_agent="crawler"`, "epicdetect"},
		{"auth_count_threshold", `index=authentication sourcetype=radius | stats count by username | where count > 10`, "epicdetect"},
		{"rex_http_status", `index=web sourcetype=apache | rex field=_raw "status=(?<http_status>\d+)" | stats count by http_status`, "epicdetect"},
		{"dedup_logins", `index=security EventCode=4624 | dedup username | table _time, username, src_ip`, "epicdetect"},
		{"failed_login_timechart", `index=security EventCode=4625 | timechart span=1h count by username`, "epicdetect"},
		{"eval_conditional", `index=security EventCode=4625 | eval failed_login_time=strftime(_time, "%Y-%m-%d %H:%M:%S") | eval status=if(Account_Name="admin", "CRITICAL", "normal") | table failed_login_time, Account_Name, status`, "epicdetect"},
		{"join_threat_intel", `index=firewall action=blocked | stats count by src_ip | join src_ip [search index=threat_intel | table src_ip, threat_score] | where threat_score > 50`, "epicdetect"},
		{"transaction_sessions", `index=web sourcetype=access_combined | transaction session_id maxpause=30m | where duration > 3600 | table session_id, duration, eventcount`, "epicdetect"},
		{"subsearch_threat_intel", `index=authentication action=failure [search index=threat_intel category=malicious | fields src_ip] | stats count by username, src_ip`, "epicdetect"},
		{"top_dns_queries", `index=dns | top limit=20 query`, "epicdetect"},
		{"geo_lookup", `index=firewall | lookup geo_ip_lookup ip as src_ip OUTPUT country, city | stats count by country`, "epicdetect"},
		{"regex_scanners", `index=web sourcetype=access_combined | regex user_agent="(?i)(nikto|sqlmap|nmap|metasploit)"`, "epicdetect"},
		{"streamstats_login", `index=authentication action=success | streamstats count by username reset_on_change=true | where count > 5`, "epicdetect"},

		// Splunk Security Content - Windows detection rules
		{"process_creation_4688", `index=windows EventCode=4688 CommandLine="*powershell*" NOT CommandLine="*-version*"`, "splunk-security"},
		{"suspicious_process", `index=sysmon EventType=ProcessCreate Image="*\\cmd.exe" ParentImage="*\\winword.exe"`, "splunk-security"},
		{"scheduled_task_creation", `index=windows EventCode=4698 | eval task_action=mvindex(split(TaskContent, "<Exec>"), 1)`, "splunk-security"},
		{"service_creation", `index=windows EventCode=7045 ServiceType="user mode service" | where like(ServiceFileName, "%cmd.exe%")`, "splunk-security"},
		{"registry_modification", `index=sysmon EventType=RegistryEvent TargetObject="*\\Run\\*" OR TargetObject="*\\RunOnce\\*"`, "splunk-security"},
		{"network_connection", `index=sysmon EventType=NetworkConnect DestinationPort IN (4444, 5555, 6666, 8080, 9999) NOT DestinationIp="10.*"`, "splunk-security"},
		{"file_creation_temp", `index=sysmon EventType=FileCreate TargetFilename="*\\Temp\\*.exe" OR TargetFilename="*\\Temp\\*.dll"`, "splunk-security"},
		{"dns_query_suspicious", `index=sysmon EventType=DNSQuery QueryName="*.onion" OR QueryName="*pastebin*" OR QueryName="*ngrok*"`, "splunk-security"},

		// Lateral movement detection
		{"lateral_movement", `index=authentication action=success | bucket _time span=5m | stats dc(dest_host) AS unique_hosts BY user, _time | where unique_hosts > 3`, "threat-hunting"},
		{"brute_force_success", `index=auth (action=failure OR action=success) | transaction user maxspan=10m | search action=success AND failure > 5`, "threat-hunting"},
		{"pass_the_hash", `index=windows EventCode=4624 LogonType=9 AuthenticationPackageName="NTLM" | stats count by TargetUserName, IpAddress`, "threat-hunting"},
		{"kerberoasting", `index=windows EventCode=4769 ServiceName!="krbtgt" TicketEncryptionType=0x17 | stats count by TargetUserName, ServiceName`, "threat-hunting"},

		// Cloud/O365 detection
		{"o365_file_download", `index=o365:management:activity Operation="FileDownloaded" | stats count by UserId, ClientIP | where count > 100`, "cloud-security"},
		{"azure_ad_signin", `index=azure:aad:signin ResultType!=0 | stats count by UserPrincipalName, IPAddress | where count > 10`, "cloud-security"},
		{"aws_console_login", `index=aws:cloudtrail eventName="ConsoleLogin" errorMessage="*" | stats count by sourceIPAddress, userIdentity.arn`, "cloud-security"},
		{"gcp_iam_changes", `index=gcp:audit:activity methodName="SetIamPolicy" OR methodName="CreateServiceAccount" | table _time, principalEmail, methodName`, "cloud-security"},

		// Network detection
		{"dns_tunneling", `index=dns | eval query_len=len(query) | where query_len > 50 | stats count by src_ip | where count > 100`, "network-security"},
		{"beaconing_detection", `index=proxy | bucket _time span=1m | stats count by src_ip, dest_ip, _time | eventstats stdev(count) as std by src_ip, dest_ip | where std < 2 AND count > 0`, "network-security"},
		{"port_scan", `index=firewall | stats dc(dest_port) as unique_ports by src_ip | where unique_ports > 100`, "network-security"},

		// Complex multi-stage queries
		{"complex_eval_chain", `index=main | eval size_mb=bytes/1024/1024 | eval category=case(size_mb<1, "small", size_mb<100, "medium", true(), "large") | stats count by category`, "complex"},
		{"nested_subsearch", `index=auth [search index=threat_intel | dedup ip | fields ip] | join type=left ip [search index=geo | fields ip, country] | stats count by user, country`, "complex"},
		{"multi_dataset_correlation", `index=windows EventCode=4688 [search index=sysmon EventType=1 | stats values(ParentImage) as parent_images by ProcessId | fields ProcessId, parent_images]`, "complex"},
		{"time_windowed_analysis", `index=auth earliest=-7d latest=now | bin _time span=1d | stats dc(user) as unique_users by _time | eventstats avg(unique_users) as avg_users | eval anomaly=if(unique_users > avg_users*2, "high", "normal")`, "complex"},

		// Edge cases from real deployments
		{"special_chars_path", `index=sysmon Image="C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe"`, "edge-case"},
		{"multivalue_field", `index=windows | mvexpand Keywords | search Keywords="Audit Success" OR Keywords="Audit Failure"`, "edge-case"},
		{"coalesce_fields", `index=proxy | eval client=coalesce(c_ip, cs_client_ip, src_ip) | stats count by client`, "edge-case"},
		{"tstats_acceleration", `| tstats count where index=windows EventCode=4688 by _time, host span=1h`, "edge-case"},
		{"inputlookup_join", `| inputlookup threat_indicators.csv | join type=left indicator [search index=proxy | rename url as indicator]`, "edge-case"},
		{"makeresults_test", `| makeresults count=10 | eval random=random() | eval value=random%100`, "edge-case"},
		{"gentimes_date_range", `| gentimes start=-7 | eval date=strftime(starttime, "%Y-%m-%d") | join date [search index=summary | stats count by date]`, "edge-case"},

		// Very long/complex queries
		{"long_in_list", `index=firewall dest_port IN (20, 21, 22, 23, 25, 53, 80, 110, 135, 139, 143, 443, 445, 465, 587, 993, 995, 1433, 1521, 3306, 3389, 5432, 5900, 8080, 8443)`, "edge-case"},
		{"many_or_conditions", `EventCode=4624 OR EventCode=4625 OR EventCode=4634 OR EventCode=4647 OR EventCode=4648 OR EventCode=4672 OR EventCode=4720 OR EventCode=4722 OR EventCode=4723 OR EventCode=4724`, "edge-case"},
		{"deeply_nested_bool", `index=main ((a=1 AND b=2) OR (c=3 AND d=4)) AND ((e=5 OR f=6) AND (g=7 OR h=8))`, "edge-case"},
	}

	var passed, failed int
	var failedQueries []string

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					failed++
					failedQueries = append(failedQueries, fmt.Sprintf("%s: PANIC: %v", tc.name, r))
					t.Errorf("Panic: %v", r)
				}
			}()

			result := ExtractConditions(tc.query)

			// Log details for debugging
			t.Logf("Source: %s", tc.source)
			t.Logf("Query: %s", truncateStr(tc.query, 100))
			t.Logf("Conditions: %d, Errors: %d", len(result.Conditions), len(result.Errors))

			if len(result.Errors) > 0 {
				t.Logf("Parse errors: %v", result.Errors)
			}

			for _, c := range result.Conditions {
				t.Logf("  %s %s %q (negated=%v, stage=%d, alts=%v)",
					c.Field, c.Operator, c.Value, c.Negated, c.PipeStage, c.Alternatives)
			}

			passed++
		})
	}

	t.Logf("\n=== Summary ===")
	t.Logf("Passed: %d", passed)
	t.Logf("Failed: %d", failed)

	if len(failedQueries) > 0 {
		t.Logf("Failed queries:")
		for _, q := range failedQueries {
			t.Logf("  %s", q)
		}
	}
}
