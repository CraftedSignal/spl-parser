package spl

import (
	"reflect"
	"testing"
)

// ClassifyPipelineStages reads the pipeline ExtractConditions reads: an
// inline ``` comment hides no later stage, and each OriginalText is the
// authored text of its stage, also after a non-ASCII character.
func TestClassifyPipelineStagesSeesNormalizedPipeline(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"index=a ```triage note``` | join user [search index=b]", []string{"index=a", "join user [search index=b]"}},
		{`index=a user="é" | where b=2 | stats count`, []string{`index=a user="é"`, `where b=2`, `stats count`}},
		{"index=a ```note``` user=b | where x==1", []string{"index=a ```note``` user=b", "where x==1"}},
	} {
		stages := ClassifyPipelineStages(tc.query)
		got := make([]string, 0, len(stages))
		for _, stage := range stages {
			got = append(got, stage.OriginalText)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: stage texts %q, want %q", tc.query, got, tc.want)
		}
	}
	query := "index=a ```triage note``` | join user [search index=b]"
	if got := FirstJoinOrSubsearchStage(query); got != 1 {
		t.Errorf("FirstJoinOrSubsearchStage = %d, want 1", got)
	}
}
