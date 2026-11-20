package spl

import "testing"

// Only a positive equality selects the event type: a negated or != EventCode,
// a range comparison and a negated sysmon sourcetype do not. When several
// EventCode conditions qualify, the first one wins.
func TestGetEventTypeFromConditionsUsesFirstPositiveEquality(t *testing.T) {
	cases := []struct{ query, want string }{
		{`index=x EventCode=4688 EventCode!=4624`, "windows_4688"},
		{`index=x EventCode=4624 | where NOT EventCode=4625`, "windows_4624"},
		{`index=x NOT EventCode=4688 process=cmd.exe`, ""},
		{`index=x EventCode>=4688`, ""},
		{`index=x sourcetype!=*sysmon* EventCode=1`, ""},
		{`index=x NOT sourcetype=*sysmon* EventCode=1`, ""},
		{`index=x EventCode=4688 | where EventCode=4624`, "windows_4688"},
		{`index=x EventCode IN (4624, 4625)`, "windows_4624"},
	}
	for _, tc := range cases {
		if got := GetEventTypeFromConditions(ExtractConditions(tc.query)); got != tc.want {
			t.Errorf("%s: event type %q, want %q", tc.query, got, tc.want)
		}
	}
}
