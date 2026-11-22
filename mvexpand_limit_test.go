package spl

import (
	"context"
	"testing"
)

func TestMvexpandLimitPublicContract(t *testing.T) {
	for _, stage := range []string{"mvexpand foo limit=100", "mvexpand foo limit=0", "mvexpand foo"} {
		t.Run(stage, func(t *testing.T) {
			query := "search index=main | " + stage
			stages, err := ClassifyPipelineStagesContext(context.Background(), query)
			if err != nil || len(stages) != 2 {
				t.Fatalf("classification = %#v, %v; want two stages", stages, err)
			}
			if stages[0].CommandType != "search" || stages[1].CommandType != "mvexpand" || stages[1].OriginalText != stage {
				t.Fatalf("authored command lost: %#v", stages)
			}
			parsed, analyzed := ExtractConditions(query), AnalyzeQuery(query)
			for _, result := range []struct {
				name       string
				errors     []string
				conditions []Condition
				flow       *QueryDataflow
			}{
				{"extractor", parsed.Errors, parsed.Conditions, parsed.Dataflow},
				{"analyzer", analyzed.Errors, analyzed.Conditions, analyzed.Dataflow},
			} {
				if len(result.errors) != 0 || len(result.conditions) != 1 || result.conditions[0].Field != "index" {
					t.Errorf("%s invented an option predicate or rejected syntax: errors=%v conditions=%#v", result.name, result.errors, result.conditions)
				}
				flow := result.flow
				if flow == nil || flow.Complete || len(flow.Stages) != 2 {
					t.Fatalf("%s expansion must retain an incomplete graph: %#v", result.name, flow)
				}
				if got := flow.Stages[1]; got.Command != "mvexpand" || got.Source != " "+stage || got.Effect != "opaque" || len(got.BindingIDs) != 0 {
					t.Errorf("%s expansion must retain authored opaque stage: %#v", result.name, got)
				}
			}
		})
	}
	stages, err := ClassifyPipelineStagesContext(context.Background(), "search index=main | mvexpand foo arbitrary=100")
	if err != nil || stages != nil {
		t.Errorf("undocumented option accepted: %#v, %v", stages, err)
	}
}
