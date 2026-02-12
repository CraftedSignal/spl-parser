package spl

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// FuzzSPLParser tests the parser with random inputs
func FuzzSPLParser(f *testing.F) {
	for _, seed := range fuzzSeeds {
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

// TestConditionCompleteness verifies the parser doesn't silently drop conditions.
// It tests curated valid SPL queries with known expected field=value pairs and checks
// they all appear in the parser output. This catches bugs like numeric field names
// (3=3) causing the parser to drop all subsequent conditions.
func TestConditionCompleteness(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectedFields []string // field names that must be extracted (case-insensitive)
	}{
		// === Numeric field names (the original 3=3 bug) ===
		{
			name:           "numeric field 3=3 with surrounding conditions",
			query:          `index=main sourcetype=sysmon 3=3 ParentImage="*\WINWORD.EXE" Image="*\powershell.exe"`,
			expectedFields: []string{"index", "sourcetype", "3", "ParentImage", "Image"},
		},
		{
			name:           "multiple numeric fields",
			query:          `0=0 1=1 2=2 3=3 EventCode=4688`,
			expectedFields: []string{"0", "1", "2", "3", "EventCode"},
		},
		{
			name:           "numeric field at start",
			query:          `3=3 index=main Image="test"`,
			expectedFields: []string{"3", "index", "Image"},
		},
		{
			name:           "numeric field in middle",
			query:          `index=sysmon 3=3 DestinationPort=443`,
			expectedFields: []string{"index", "3", "DestinationPort"},
		},
		{
			name:           "Sysmon event type 1",
			query:          `sourcetype=sysmon 1=1 Image="*\powershell.exe" CommandLine="*-enc*"`,
			expectedFields: []string{"sourcetype", "1", "Image", "CommandLine"},
		},
		{
			name:           "Sysmon event type 3 with network",
			query:          `index=sysmon 3=3 SourceIp="10.0.0.1" DestinationIp="192.168.1.1"`,
			expectedFields: []string{"index", "3", "SourceIp", "DestinationIp"},
		},
		{
			name:           "Sysmon event type 7 with parent/child",
			query:          `7=7 Image="*\rundll32.exe" ParentImage="*\explorer.exe"`,
			expectedFields: []string{"7", "Image", "ParentImage"},
		},
		{
			name:           "Sysmon many event types",
			query:          `sourcetype=sysmon 1=1 3=3 11=11 22=22 Image="*"`,
			expectedFields: []string{"sourcetype", "1", "3", "11", "22", "Image"},
		},
		{
			name:           "large numeric field",
			query:          `index=main 255=something field=value`,
			expectedFields: []string{"index", "255", "field"},
		},

		// === Basic conditions (regression) ===
		{
			name:           "simple multi-field",
			query:          `index=main sourcetype=access_combined status=200`,
			expectedFields: []string{"index", "sourcetype", "status"},
		},
		{
			name:           "Windows security events",
			query:          `EventCode=4624 Logon_Type=10`,
			expectedFields: []string{"EventCode", "Logon_Type"},
		},
		{
			name:           "multiple wildcard values",
			query:          `Image="*\powershell.exe" CommandLine="*-enc*" ParentImage="*\winword.exe"`,
			expectedFields: []string{"Image", "CommandLine", "ParentImage"},
		},

		// === Boolean logic ===
		{
			name:           "OR conditions",
			query:          `index=main (status=200 OR status=404) host=web01`,
			expectedFields: []string{"index", "status", "host"},
		},
		{
			name:           "NOT conditions",
			query:          `EventCode=4688 NOT user=SYSTEM Image="*\cmd.exe"`,
			expectedFields: []string{"EventCode", "user", "Image"},
		},
		{
			name:           "complex boolean with parens",
			query:          `(EventCode=4624 OR EventCode=4625) Logon_Type=10 TargetUserName=admin`,
			expectedFields: []string{"EventCode", "Logon_Type", "TargetUserName"},
		},
		{
			name:           "nested parens",
			query:          `index=main ((status=200 AND method=GET) OR (status=201 AND method=POST))`,
			expectedFields: []string{"index", "status", "method"},
		},

		// === Comparison operators ===
		{
			name:           "greater/less than",
			query:          `index=main status>200 bytes>1000 duration<3600`,
			expectedFields: []string{"index", "status", "bytes", "duration"},
		},
		{
			name:           "not equals",
			query:          `index=main status!=200 host=web01`,
			expectedFields: []string{"index", "status", "host"},
		},

		// === IN operator ===
		{
			name:           "IN with subsequent conditions",
			query:          `status IN (200, 201, 204) host=web01 method=GET`,
			expectedFields: []string{"status", "host", "method"},
		},
		{
			name:           "EventCode IN with conditions",
			query:          `EventCode IN (4624, 4625, 4634) Logon_Type=10`,
			expectedFields: []string{"EventCode", "Logon_Type"},
		},

		// === Pipe commands (conditions only before pipe) ===
		{
			name:           "conditions before stats pipe",
			query:          `index=main status>=400 host=prod | stats count by uri`,
			expectedFields: []string{"index", "status", "host"},
		},

		// === Dotted field names ===
		{
			name:           "dotted field names",
			query:          `process.name="cmd.exe" process.pid=1234 user=admin`,
			expectedFields: []string{"process.name", "process.pid", "user"},
		},

		// === Quoted values with special chars ===
		{
			name:           "values with backslashes",
			query:          `CommandLine="C:\Windows\System32\cmd.exe /c whoami" Image="*\cmd.exe" user=admin`,
			expectedFields: []string{"CommandLine", "Image", "user"},
		},

		// === Real-world detection queries ===
		{
			name:           "process creation with parent",
			query:          `index=sysmon EventCode=1 ParentImage="*\services.exe" Image="*\svchost.exe" user=SYSTEM`,
			expectedFields: []string{"index", "EventCode", "ParentImage", "Image", "user"},
		},
		{
			name:           "network connection with ports",
			query:          `index=sysmon EventCode=3 DestinationPort=443 SourceIp="10.0.0.1" Image="*\chrome.exe"`,
			expectedFields: []string{"index", "EventCode", "DestinationPort", "SourceIp", "Image"},
		},
		{
			name:           "file creation monitoring",
			query:          `index=sysmon EventCode=11 TargetFilename="*.exe" Image="*\powershell.exe" user=admin`,
			expectedFields: []string{"index", "EventCode", "TargetFilename", "Image", "user"},
		},
		{
			name:           "mixed event types and sourcetypes",
			query:          `(sourcetype=sysmon OR sourcetype=xmlwineventlog) EventCode=1 Image="*\powershell.exe"`,
			expectedFields: []string{"sourcetype", "EventCode", "Image"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractConditions(tt.query)
			if result == nil {
				t.Fatal("ExtractConditions returned nil")
			}

			extractedFields := make(map[string]bool)
			for _, c := range result.Conditions {
				extractedFields[strings.ToLower(c.Field)] = true
			}

			for _, expected := range tt.expectedFields {
				if !extractedFields[strings.ToLower(expected)] {
					t.Errorf("field %q not extracted\n  query: %s\n  extracted: %v\n  errors: %v",
						expected, tt.query, extractedFields, result.Errors)
				}
			}
		})
	}
}

// fuzzSeeds is a comprehensive seed corpus covering SPL edge cases.
// These are shared across all fuzz targets so the fuzzer can discover
// interesting mutations from any of them.
var fuzzSeeds = []string{
	// === Basic field=value ===
	`index=main`,
	`index=main status=200`,
	`index=main sourcetype=access_combined status=200`,
	`EventCode=4624 Logon_Type=10`,
	`source="/var/log/*.log" error`,

	// === Numeric field names (Sysmon-style) ===
	`3=3`,
	`1=1`,
	`index=main 3=3`,
	`index=main 3=3 ParentImage="*\\cmd.exe"`,
	`sourcetype=sysmon 1=1 Image="*\\powershell.exe" CommandLine="*-enc*"`,
	`index=main 10=10 TargetFilename="*.exe"`,
	`42=hello field2=world`,
	`EventCode=4624 3=3 user=admin status=success`,
	`index=main 7=7 22=22 Image="test"`,
	`0=0 1=1 2=2 3=3 EventCode=4688`,
	`index=sysmon 3=3 SourceIp="10.0.0.1" DestinationIp="192.168.1.1"`,
	`sourcetype=xmlwineventlog 3=3 Image="*\\svchost.exe"`,
	`index=main 255=something field=value`,

	// === Wildcard values ===
	`Image="*\\powershell.exe"`,
	`CommandLine="*-enc*"`,
	`ParentImage="*\\winword.exe"`,
	`Image="*\\powershell.exe" CommandLine="*-enc*" ParentImage="*\\winword.exe"`,
	`TargetFilename="C:\\Users\\*\\AppData\\*"`,
	`process_name="*sh" user="admin*"`,
	`host=web* status=5*`,

	// === Quoted values ===
	`field="value with spaces"`,
	`CommandLine="C:\\Windows\\System32\\cmd.exe /c whoami"`,
	`Message="Login failed for user 'admin'"`,
	`query="SELECT * FROM users WHERE id=1"`,
	`path="C:\\Program Files (x86)\\Application\\app.exe"`,

	// === Boolean logic ===
	`index=main (status=200 OR status=404)`,
	`index=main NOT status=500`,
	`index=main status=200 OR status=301 OR status=302`,
	`(EventCode=4624 OR EventCode=4625) Logon_Type=10`,
	`NOT (status=200 OR status=204) host=web01`,
	`EventCode=4688 (Image="*\\cmd.exe" OR Image="*\\powershell.exe") NOT user=SYSTEM`,
	`(src_ip="10.0.0.*" OR src_ip="192.168.*") dest_port=443`,
	`index=main ((status=200 AND method=GET) OR (status=201 AND method=POST))`,

	// === Comparison operators ===
	`status>200`,
	`status>=400`,
	`status<500`,
	`status<=299`,
	`status!=200`,
	`bytes>1000000`,
	`response_time>5.0`,
	`count>=10 duration<3600`,

	// === IN operator ===
	`status IN (200, 201, 204)`,
	`EventCode IN (4624, 4625, 4634)`,
	`index=main status IN (200, 201, 204)`,
	`method IN ("GET", "POST", "PUT", "DELETE")`,
	`user IN ("admin", "root", "system")`,
	`host IN ("web01", "web02", "web03") status!=200`,

	// === Pipe commands ===
	`index=main | stats count by host`,
	`index=main | where status>200`,
	`index=main | eval x=len(field) | where x>10`,
	`index=main | rex field=_raw "(?<ip>\d+\.\d+\.\d+\.\d+)"`,
	`index=main | stats count avg(duration) max(bytes) by src_ip`,
	`index=main | timechart span=1h count by status`,
	`index=main | dedup 3 user sortby -_time`,
	`index=main | fillnull value=0 count`,
	`index=main | table _time host status uri`,
	`index=main | sort -_time | head 100`,
	`index=main | rename src_ip AS source_address dest_ip AS destination_address`,
	`index=main | fields host status uri response_time`,
	`index=main | top 10 uri by host`,

	// === Stats with aliases ===
	`index=main | stats count as events by user`,
	`index=main | stats count as events dc(host) as hosts by user | where events>5`,
	`index=main | stats avg(duration) as avg_dur max(duration) as max_dur by src_ip`,
	`index=main | stats count as cnt sum(bytes) as total_bytes by host`,
	`index=main | eventstats count as total_events by user`,
	`index=main | streamstats count as running_count by session_id`,

	// === Join and subsearch ===
	`index=main | join type=left user [search index=users]`,
	`index=main user IN [search index=users status=active | fields user]`,
	`index=main | append [search index=firewall action=blocked]`,
	`index=main | join src_ip [search index=threat_intel | fields ip as src_ip indicator]`,

	// === Transaction ===
	`index=main | transaction user maxspan=30m`,
	`index=web | transaction session_id maxpause=30m | where duration>3600`,
	`index=main | transaction user startswith=login endswith=logout`,

	// === Eval expressions ===
	`index=main | eval status_group=case(status<300,"success",status<400,"redirect",status<500,"client_error",1=1,"server_error")`,
	`index=main | eval mb=bytes/1024/1024`,
	`index=main | eval is_local=if(cidrmatch("10.0.0.0/8",src_ip),"yes","no")`,
	`index=main | eval duration_min=duration/60`,
	`index=main | eval combined=src_ip.":".tostring(src_port)`,

	// === Tstats ===
	`| tstats count where index=* by sourcetype`,
	`| tstats count where index=sysmon EventCode=1 by host`,
	`| tstats summariesonly=true count from datamodel=Authentication where Authentication.action=failure by Authentication.src`,
	`| tstats count where index=windows EventCode=4688 by host process`,

	// === Mstats ===
	`| mstats avg(cpu.usage) where index=metrics by host`,

	// === Inputlookup ===
	`| inputlookup threat_intel.csv where score>50`,
	`| inputlookup users.csv | search status=active`,

	// === Rex ===
	`index=main | rex field=_raw "(?<username>[^\\\\]+)$"`,
	`index=main | rex field=CommandLine "(?<script>[^\s]+\.ps1)"`,
	`index=main | rex field=uri "(?<api_version>/v\d+)"`,

	// === Makemv / Mvexpand ===
	`index=main | makemv delim="," values | mvexpand values`,

	// === Lookup ===
	`index=main | lookup users_lookup user OUTPUT department`,
	`index=main | lookup geo_ip src_ip OUTPUT country city`,

	// === Complex real-world queries ===
	`index=sysmon EventCode=1 ParentImage="*\\services.exe" NOT Image IN ("*\\svchost.exe","*\\msiexec.exe","*\\taskhost.exe") | stats count by Image host`,
	`index=windows sourcetype="WinEventLog:Security" EventCode=4688 (CommandLine="*whoami*" OR CommandLine="*net user*" OR CommandLine="*ipconfig*") | table _time host user CommandLine`,
	`index=sysmon EventCode=3 NOT (DestinationIp="10.*" OR DestinationIp="192.168.*" OR DestinationIp="172.16.*") DestinationPort IN (4444,5555,8080,8443) | stats count by Image DestinationIp DestinationPort`,
	`index=main sourcetype=access_combined status>=400 | stats count as errors by uri | where errors>100 | sort -errors`,
	`index=wineventlog EventCode=4625 | stats count as failed_logins by src_ip | where failed_logins>10`,
	`index=sysmon EventCode=11 TargetFilename="*.exe" NOT TargetFilename="C:\\Windows\\*" | stats count by Image TargetFilename | where count>5`,
	`index=main sourcetype=sysmon EventCode=1 Image="*\\powershell.exe" (CommandLine="*-e *" OR CommandLine="*-enc *" OR CommandLine="*-encodedcommand *" OR CommandLine="*downloadstring*" OR CommandLine="*downloadfile*") | table _time host user CommandLine`,
	`index=windows EventCode IN (4624,4625,4634) | eval login_type=case(Logon_Type=2,"Interactive",Logon_Type=3,"Network",Logon_Type=7,"Unlock",Logon_Type=10,"RemoteInteractive",1=1,"Other") | stats count by login_type EventCode`,
	`index=sysmon 3=3 DestinationPort=443 NOT DestinationIp IN ("10.0.0.0/8","172.16.0.0/12","192.168.0.0/16") | stats count by Image DestinationIp`,
	`index=sysmon 1=1 ParentImage="*\\winword.exe" (Image="*\\cmd.exe" OR Image="*\\powershell.exe" OR Image="*\\wscript.exe" OR Image="*\\cscript.exe") | table _time host ParentImage Image CommandLine`,

	// === Sysmon all event types with numeric fields ===
	`sourcetype=sysmon 1=1`,   // Process creation
	`sourcetype=sysmon 2=2`,   // File creation time changed
	`sourcetype=sysmon 3=3`,   // Network connection
	`sourcetype=sysmon 5=5`,   // Process terminated
	`sourcetype=sysmon 6=6`,   // Driver loaded
	`sourcetype=sysmon 7=7`,   // Image loaded
	`sourcetype=sysmon 8=8`,   // CreateRemoteThread
	`sourcetype=sysmon 9=9`,   // RawAccessRead
	`sourcetype=sysmon 10=10`, // ProcessAccess
	`sourcetype=sysmon 11=11`, // FileCreate
	`sourcetype=sysmon 12=12`, // Registry event
	`sourcetype=sysmon 13=13`, // Registry value set
	`sourcetype=sysmon 15=15`, // FileCreateStreamHash
	`sourcetype=sysmon 17=17`, // PipeEvent (created)
	`sourcetype=sysmon 18=18`, // PipeEvent (connected)
	`sourcetype=sysmon 22=22`, // DNSEvent
	`sourcetype=sysmon 23=23`, // FileDelete
	`sourcetype=sysmon 25=25`, // ProcessTampering

	// === Multiple numeric fields ===
	`1=1 3=3 Image="test"`,
	`index=main 3=3 10=10 22=22`,
	`7=7 Image="*\\rundll32.exe" ParentImage="*\\explorer.exe"`,
	`sourcetype=sysmon 1=1 3=3 11=11 22=22 Image="*"`,

	// === Edge cases ===
	`index=main earliest=-24h latest=now`,
	`index=main | eval 1=1`,
	`field.sub.path="value"`,
	`process.name="cmd.exe" process.pid=1234`,
	`host=web* status=5*`,
	`_time>1234567890`,
	`field="value=with=equals"`,
	`field="value with \"quotes\""`,
	`*=*`,
	`field=*`,

	// === Bucket/bin ===
	`index=main | bucket _time span=1h | stats count by _time`,

	// === Convert ===
	`index=main | convert dur2sec(duration)`,

	// === Chart ===
	`index=main | chart count by status over host`,
	`index=main | chart avg(response_time) by uri`,

	// === Eventstats ===
	`index=main | eventstats avg(bytes) as avg_bytes by host | where bytes>avg_bytes*2`,

	// === Search with multiple sourcetypes ===
	`(sourcetype=sysmon OR sourcetype=xmlwineventlog) EventCode=1 Image="*\\powershell.exe"`,
	`(index=windows OR index=sysmon) EventCode IN (1,3,7,11,22)`,

	// === Very long condition lists ===
	`EventCode IN (1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26)`,
	`Image IN ("*\\cmd.exe","*\\powershell.exe","*\\wscript.exe","*\\cscript.exe","*\\mshta.exe","*\\certutil.exe","*\\bitsadmin.exe","*\\regsvr32.exe","*\\rundll32.exe","*\\msiexec.exe")`,

	// === SPL with Windows paths ===
	`Image="C:\\Windows\\System32\\cmd.exe"`,
	`TargetFilename="C:\\Users\\Public\\Downloads\\*.exe"`,
	`Image="C:\\Windows\\SysWOW64\\WindowsPowerShell\\v1.0\\powershell.exe"`,

	// === Time constraints mixed with conditions ===
	`index=main earliest=-1h status>=500 host=prod-*`,
	`index=sysmon earliest="2024-01-01" latest="2024-01-31" EventCode=1`,

	// === Spath (JSON extraction) ===
	`index=main sourcetype=json | spath output=user path=data.user.name`,

	// === Rare edge case: eval case with 1=1 ===
	`index=main | eval severity=case(status>=500,"critical",status>=400,"high",status>=300,"medium",1=1,"low")`,

	// === Multiple pipes with conditions at each stage ===
	`index=main status>=400 | stats count by src_ip | where count>50 | sort -count | head 10`,
	`index=sysmon EventCode=1 | search Image="*\\cmd.exe" | stats count by host | where count>100`,
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
		// Numeric field names (Sysmon-style)
		"1", "3", "7", "10", "22", "42",
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
