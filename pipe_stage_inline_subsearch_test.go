package spl

import "testing"

// A condition written after an inline subsearch keeps the PipeStage of its
// own pipeline stage: the subsearch's stages do not advance it.
func TestConditionAfterInlineSubsearchKeepsItsStage(t *testing.T) {
	for _, tc := range []struct {
		query string
		stage int
	}{
		{`index=a [search index=b | fields x] action=allowed | join user [search index=c]`, 0},
		{`index=a | search [search index=b | fields x] action=allowed | stats count`, 1},
	} {
		result := ExtractConditions(tc.query)
		if result.Dataflow == nil {
			t.Fatalf("%s: dataflow missing, errors %v", tc.query, result.Errors)
		}
		stages := make(map[string]int, len(result.Dataflow.Stages))
		for _, stage := range result.Dataflow.Stages {
			stages[stage.ID] = stage.PipeStage
		}
		var action *Condition
		for i := range result.Conditions {
			condition := &result.Conditions[i]
			if want, ok := stages[condition.StageID]; ok && condition.PipeStage != want {
				t.Errorf("%s: %s %s %q has PipeStage %d but its stage %s has %d", tc.query,
					condition.Field, condition.Operator, condition.Value, condition.PipeStage, condition.StageID, want)
			}
			if condition.Field == "action" {
				action = condition
			}
		}
		if action == nil {
			t.Fatalf("%s: action condition missing: %+v", tc.query, result.Conditions)
		}
		if action.PipeStage != tc.stage {
			t.Errorf("%s: action=allowed has PipeStage %d, want %d", tc.query, action.PipeStage, tc.stage)
		}
	}
	query := `index=a [search index=b | fields x] action=allowed | join user [search index=c]`
	if join := FirstJoinOrSubsearchStage(query); join != 1 {
		t.Errorf("FirstJoinOrSubsearchStage = %d, want 1", join)
	}
}
