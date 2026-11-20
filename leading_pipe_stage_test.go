package spl

import "testing"

// A leading pipe opens the first command and is not a stage of its own, so
// resource and fallback conditions number stages as the parser does.
func TestLeadingPipeIsNotAStage(t *testing.T) {
	for _, tc := range []struct {
		query  string
		stages map[string]int
	}{
		{`| tstats count from datamodel=Endpoint.Processes where Processes.user=a by Processes.dest`,
			map[string]int{"Processes.user": 0, "_spl.datamodel": 0}},
		{`| inputlookup users.csv | where user="a" | outputlookup out.csv`,
			map[string]int{"_spl.inputlookup": 0, "user": 1, "_spl.outputlookup": 2}},
		{`| ldapsearch search="(cn=alice)"`,
			map[string]int{"cn": 0}},
		{`| inputlookup users.csv | search action=search user=bob`,
			map[string]int{"_spl.inputlookup": 0, "user": 1}},
		{`| makeresults count=5 | outputlookup out.csv`,
			map[string]int{"_spl.outputlookup": 1}},
		{`index=a | outputlookup out.csv`,
			map[string]int{"index": 0, "_spl.outputlookup": 1}},
	} {
		result := ExtractConditions(tc.query)
		for field, stage := range tc.stages {
			found := false
			for _, condition := range result.Conditions {
				if condition.Field != field {
					continue
				}
				found = true
				if condition.PipeStage != stage {
					t.Errorf("%s: %s has PipeStage %d, want %d", tc.query, field, condition.PipeStage, stage)
				}
			}
			if !found {
				t.Errorf("%s: no %s condition in %+v", tc.query, field, result.Conditions)
			}
		}
		for _, condition := range result.Conditions {
			if _, ok := tc.stages[condition.Field]; !ok && condition.Field != "action" {
				t.Errorf("%s: unexpected condition %s %s %q", tc.query, condition.Field, condition.Operator, condition.Value)
			}
		}
	}
}
