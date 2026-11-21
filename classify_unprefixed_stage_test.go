package spl

import (
	"reflect"
	"testing"
)

// Splunk implies the search command only at the beginning of a search, so a
// later stage, or the first stage after a leading pipe, that has no grammar
// rule of its own is a generic command, not "search".
func TestClassifyPipelineStagesUnprefixedLaterStageIsGeneric(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{`index=a | iplocation src_ip | regex user="^adm" | appendcols [search index=b] | search user=bob`,
			[]string{"search", "generic", "generic", "generic", "search"}},
		{`| makeresults count=5 | search user=bob`, []string{"generic", "search"}},
		{`| search index=a | where user="bob"`, []string{"search", "where"}},
	} {
		stages := ClassifyPipelineStages(tc.query)
		got := make([]string, 0, len(stages))
		for _, stage := range stages {
			got = append(got, stage.CommandType)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: command types %q, want %q", tc.query, got, tc.want)
		}
	}
}
