package spl

import "testing"

func requireSPLCondition(t *testing.T, conditions []Condition, field, op, value string, negated bool) Condition {
	t.Helper()

	for _, condition := range conditions {
		if condition.Field == field && condition.Operator == op && condition.Value == value && condition.Negated == negated {
			return condition
		}
	}
	t.Fatalf("missing condition field=%q op=%q value=%q negated=%v in %#v", field, op, value, negated, conditions)
	return Condition{}
}

func TestPortablePredicateAdvancedExtractionEdges(t *testing.T) {
	text := `NOT user IN ("alice", "bob") OR cidrmatch("10.0.0.0/8", src_ip) AND ` +
		`net_ipsubnet(dest_ip, "192.168.0.0/16") AND NOT isnull(host) AND ` +
		`lower(Image) contains "cmd" AND process_name matches ("cmd.exe", "powershell.exe") AND severity>=5`

	conditions := extractPortablePredicatesFromText(text, 3)

	user := requireSPLCondition(t, conditions, "user", "in", "alice", true)
	if len(user.Alternatives) != 2 || user.Alternatives[1] != "bob" {
		t.Fatalf("IN alternatives were not preserved: %#v", user)
	}
	cidr := requireSPLCondition(t, conditions, "src_ip", "cidrmatch", "10.0.0.0/8", false)
	if cidr.LogicalOp != "OR" {
		t.Fatalf("cidr condition logical operator = %q, want OR", cidr.LogicalOp)
	}
	requireSPLCondition(t, conditions, "dest_ip", "cidrmatch", "192.168.0.0/16", false)
	requireSPLCondition(t, conditions, "host", "isnull", "", true)
	requireSPLCondition(t, conditions, "Image", "contains", "cmd", false)
	matches := requireSPLCondition(t, conditions, "process_name", "matches", "cmd.exe", false)
	if len(matches.Alternatives) != 2 || matches.Alternatives[1] != "powershell.exe" {
		t.Fatalf("parenthesized comparison alternatives were not preserved: %#v", matches)
	}
	requireSPLCondition(t, conditions, "severity", ">=", "5", false)
}

func TestPortablePredicateGuardEdges(t *testing.T) {
	if got := scanBalancedGroupEnd("abc", 0, '(', ')'); got != -1 {
		t.Fatalf("non-group start returned %d", got)
	}
	if hasWordAt("annot", 2, "not") {
		t.Fatalf("word-boundary check matched inside another token")
	}
	if portableLogicalOpBefore("a=1 OR", 99) != "OR" {
		t.Fatalf("logical op before clamped start should be OR")
	}
	if !portableNegatedAt(`field!="value"`, 0, true) {
		t.Fatalf("explicit != should be represented as negated equality")
	}
	if !portableNegatedAt(`!(field="value")`, len("!("), false) {
		t.Fatalf("group negation was not detected")
	}
	if !shouldSkipPortableField("bad field") || !shouldSkipPortableField(".bad") || !shouldSkipPortableField("and") {
		t.Fatalf("invalid portable fields were not skipped")
	}
	if shouldSkipPortableField("EventCode") {
		t.Fatalf("valid portable field was skipped")
	}
}

func TestLDAPFilterExtractionEdges(t *testing.T) {
	conditions := parseLDAPFilterConditions(`(&(objectClass=user)(|(cn=Alice)(cn=Bob))(!(description=disabled))(age>=21)(displayName~=Adm*))`, 2)

	requireSPLCondition(t, conditions, "objectClass", "=", "user", false)
	firstCN := requireSPLCondition(t, conditions, "cn", "=", "Alice", false)
	if firstCN.LogicalOp != "AND" {
		t.Fatalf("first OR branch should retain leading AND, got %q", firstCN.LogicalOp)
	}
	secondCN := requireSPLCondition(t, conditions, "cn", "=", "Bob", false)
	if secondCN.LogicalOp != "OR" {
		t.Fatalf("second OR branch logical operator = %q, want OR", secondCN.LogicalOp)
	}
	requireSPLCondition(t, conditions, "description", "=", "disabled", true)
	requireSPLCondition(t, conditions, "age", ">=", "21", false)
	requireSPLCondition(t, conditions, "displayName", "matches", "Adm*", false)

	exists, ok := ldapAssertionCondition("memberOf=*", false, 4)
	if !ok || exists.Operator != "isnotnull" || exists.Value != "" {
		t.Fatalf("LDAP presence assertion not normalized: %#v ok=%v", exists, ok)
	}
	if _, _, _, ok := splitLDAPAssertion("not-an-assertion"); ok {
		t.Fatalf("invalid LDAP assertion was accepted")
	}
	if got := (&ldapFilterParser{text: "missing-parens"}).parse(); got != nil {
		t.Fatalf("invalid LDAP filter parsed to %#v", got)
	}
}

func TestEmbeddedAndResourceExtractionEdges(t *testing.T) {
	embedded := extractEmbeddedPredicateConditions(`columns x | where user="alice" | filter status>=400 | search action="blocked"`, 5)
	requireSPLCondition(t, embedded, "user", "=", "alice", false)
	requireSPLCondition(t, embedded, "status", ">=", "400", false)
	requireSPLCondition(t, embedded, "action", "=", "blocked", false)

	query := `index=main [ search index=child user="alice" ] | ldapsearch search="(|(cn=Alice)(cn=Bob))" | ` +
		`inputlookup append=true "risky users.csv" | outputlookup create_empty=false saved.csv | ` +
		`collect index=summary source="alert source" | tstats count FROM datamodel=Endpoint.Processes where Processes.process_name="cmd.exe"`
	conditions := extractPortableSPLConditionsDepth(normalizeSPLQuery(query), 0)
	requireSPLCondition(t, conditions, "user", "=", "alice", false)
	requireSPLCondition(t, conditions, "cn", "=", "Alice", false)
	requireSPLCondition(t, conditions, "Processes.process_name", "=", "cmd.exe", false)

	resources := extractCommandResourceConditions(query)
	requireSPLCondition(t, resources, "_spl.inputlookup", "=", "risky users.csv", false)
	requireSPLCondition(t, resources, "_spl.outputlookup", "=", "saved.csv", false)
	requireSPLCondition(t, resources, "_spl.collect.index", "=", "summary", false)
	requireSPLCondition(t, resources, "_spl.collect.source", "=", "alert source", false)
	requireSPLCondition(t, resources, "_spl.datamodel", "=", "Endpoint.Processes", false)

	if got := firstCommandResourceArg(`inputlookup append=true "risky users.csv"`, "inputlookup"); got != "risky users.csv" {
		t.Fatalf("first command resource = %q", got)
	}
	if got := extractOptionValue(`collect index=summary source="alert source"`, "source"); got != "alert source" {
		t.Fatalf("quoted option value = %q", got)
	}
	if got := extractOptionValue(`collect index=summary`, "source"); got != "" {
		t.Fatalf("missing option value = %q", got)
	}
}

func TestSPLCommandListenersAndProvenanceEdges(t *testing.T) {
	extractor := &conditionExtractor{
		groupByFields:  make([]string, 0),
		computedFields: make(map[string]string),
		commands:       make([]string, 0),
	}

	tstatsCtx, ok := splParserForTest(t, `tstats count FROM datamodel=Endpoint.Processes BY Processes.user "host.name"`).TstatsCommand().(*TstatsCommandContext)
	if !ok {
		t.Fatalf("expected tstats context")
	}
	extractor.EnterTstatsCommand(tstatsCtx)
	if extractor.computedFields["_datamodel"] != "Endpoint.Processes" {
		t.Fatalf("tstats datamodel = %q", extractor.computedFields["_datamodel"])
	}
	if !containsString(extractor.groupByFields, "Processes.user") || !containsString(extractor.groupByFields, "host.name") {
		t.Fatalf("tstats group-by fields not preserved: %#v", extractor.groupByFields)
	}

	mstatsCtx, ok := splParserForTest(t, `mstats avg(_value) WHERE metric_name="cpu" BY metric_name "host.name" span=30s`).MstatsCommand().(*MstatsCommandContext)
	if !ok {
		t.Fatalf("expected mstats context")
	}
	extractor.EnterMstatsCommand(mstatsCtx)
	if !containsString(extractor.commands, "mstats") {
		t.Fatalf("mstats command not tracked: %#v", extractor.commands)
	}
	if !containsString(extractor.groupByFields, "metric_name") || !containsString(extractor.groupByFields, "host.name") {
		t.Fatalf("mstats group-by fields not preserved: %#v", extractor.groupByFields)
	}

	rexCtx, ok := splParserForTest(t, `rex field=CommandLine "(?<script>[^ ]+)(?P<extension>\.ps1)"`).RexCommand().(*RexCommandContext)
	if !ok {
		t.Fatalf("expected rex context")
	}
	extractor.EnterRexCommand(rexCtx)
	if extractor.computedFields["script"] != "CommandLine" || extractor.computedFields["extension"] != "CommandLine" {
		t.Fatalf("rex capture groups not mapped to source field: %#v", extractor.computedFields)
	}

	result := &ParseResult{
		Conditions:     []Condition{{Field: "EventCode", PipeStage: 0}, {Field: "JoinedField", PipeStage: 3}},
		ComputedFields: map[string]string{"derived": "EventCode"},
		Joins: []JoinInfo{{
			PipeStage:     2,
			JoinFields:    []string{"user"},
			ExposedFields: []string{"JoinedField"},
		}},
	}
	if got := ClassifyFieldProvenance(nil, "user"); got != ProvenanceAmbiguous {
		t.Fatalf("nil provenance = %q", got)
	}
	if got := ClassifyFieldProvenance(result, "user"); got != ProvenanceJoinKey {
		t.Fatalf("join key provenance = %q", got)
	}
	if got := ClassifyFieldProvenance(result, "JoinedField"); got != ProvenanceJoined {
		t.Fatalf("joined provenance = %q", got)
	}
	if got := ClassifyFieldProvenance(result, "EventCode"); got != ProvenanceMain {
		t.Fatalf("main provenance = %q", got)
	}
	if got := ClassifyFieldProvenance(result, "derived"); got != ProvenanceMain {
		t.Fatalf("computed provenance = %q", got)
	}
	if got := ClassifyFieldProvenance(result, "missing"); got != ProvenanceAmbiguous {
		t.Fatalf("missing provenance = %q", got)
	}
}

func TestSPLValueGroupingAndDedupEdges(t *testing.T) {
	quoted := splParserForTest(t, `"alice"`).Value()
	if got := extractValue(quoted); got != "alice" {
		t.Fatalf("quoted value = %q", got)
	}
	identifier := splParserForTest(t, `OtherField`).Value()
	if !splValueIsFieldReference(identifier, "OtherField") {
		t.Fatalf("identifier value should be a field reference")
	}
	if splValueIsFieldReference(splParserForTest(t, `true`).Value(), "true") {
		t.Fatalf("boolean literal should not be a field reference")
	}
	if splValueIsFieldReference(nil, "OtherField") {
		t.Fatalf("nil value should not be a field reference")
	}
	values := extractValueList(splParserForTest(t, `"alice", OtherField, 42`).ValueList())
	if len(values) != 3 || values[0] != "alice" || values[1] != "OtherField" || values[2] != "42" {
		t.Fatalf("value list not preserved: %#v", values)
	}

	for pattern, want := range map[string]string{
		"*cmd*": "contains",
		"*cmd":  "endswith",
		"cmd*":  "startswith",
		"cmd":   "matches",
	} {
		if got := likePatternKind(pattern); got != want {
			t.Fatalf("likePatternKind(%q) = %q, want %q", pattern, got, want)
		}
	}

	grouped := groupORConditions([]Condition{
		{Field: "index", Operator: "=", Value: "main", LogicalOp: "AND"},
		{Field: "Image", Operator: "like", Value: "*cmd*", LogicalOp: "AND"},
		{Field: "Image", Operator: "like", Value: "*powershell*", LogicalOp: "OR"},
		{Field: "User", Operator: "=", Value: "alice", LogicalOp: "AND"},
	})
	image := requireSPLCondition(t, grouped, "Image", "like", "*cmd*", false)
	if len(image.Alternatives) != 2 || image.Alternatives[1] != "*powershell*" {
		t.Fatalf("OR alternatives were not grouped: %#v", image)
	}

	deduped := DeduplicateConditions([]Condition{
		{Field: "EventCode", Operator: "=", Value: "*", PipeStage: 0},
		{Field: "User", Operator: "=", Value: "alice", PipeStage: 0},
		{Field: "user", Operator: "=", Value: "alice", PipeStage: 2},
		{Field: "user", Operator: "=", Value: "alice", PipeStage: 2},
		{Field: "Host", Operator: "=", Value: "server", PipeStage: 1},
	})
	if requireSPLCondition(t, deduped, "user", "=", "alice", false).PipeStage != 2 {
		t.Fatalf("dedup did not keep latest user stage: %#v", deduped)
	}
	requireSPLCondition(t, deduped, "Host", "=", "server", false)
	for _, condition := range deduped {
		if condition.Value == "*" {
			t.Fatalf("pure wildcard condition was not removed: %#v", deduped)
		}
	}
}

func TestSPLPipelineStageClassificationEdges(t *testing.T) {
	tests := []struct {
		segment string
		want    string
	}{
		{`search index=main`, "search"},
		{`where user="alice"`, "where"},
		{`eval lower_user=lower(user)`, "eval"},
		{`stats count by user`, "stats"},
		{`table user host`, "table"},
		{`fields user host`, "fields"},
		{`rename user AS Account`, "rename"},
		{`rex field=CommandLine "(?<script>[^ ]+)"`, "rex"},
		{`dedup user`, "dedup"},
		{`sort user`, "sort"},
		{`head 5`, "head"},
		{`tail 5`, "tail"},
		{`top user`, "top"},
		{`rare user`, "rare"},
		{`lookup users.csv user OUTPUT dept`, "lookup"},
		{`join user [ search index=joined ]`, "join"},
		{`append [ search index=joined ]`, "append"},
		{`transaction user`, "transaction"},
		{`spath input=_raw`, "spath"},
		{`eventstats count by user`, "eventstats"},
		{`streamstats count by user`, "streamstats"},
		{`timechart count by user`, "timechart"},
		{`chart count by user`, "chart"},
		{`fillnull value="-" user`, "fillnull"},
		{`makemv delim="," tags`, "makemv"},
		{`mvexpand tags`, "mvexpand"},
		{`format`, "format"},
		{`convert ctime(_time) AS date`, "convert"},
		{`bucket _time span=1h`, "bucket"},
		{`rest /services/server/info`, "rest"},
		{`tstats count FROM datamodel=Endpoint.Processes BY Processes.user`, "tstats"},
		{`mstats avg(_value) WHERE metric_name="cpu" BY metric_name`, "mstats"},
		{`inputlookup users.csv`, "inputlookup"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			stage := splParserForTest(t, tt.segment).PipelineStage()
			if got := classifyStage(stage); got != tt.want {
				t.Fatalf("classifyStage(%q) = %q, want %q", tt.segment, got, tt.want)
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
