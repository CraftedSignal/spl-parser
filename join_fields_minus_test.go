package spl

import "testing"

// `fields - secret` removes secret, so a join over that subsearch does not
// expose it, while a kept field list still names the exposed fields.
func TestJoinExposedFieldsOmitFieldsRemovedBySubsearch(t *testing.T) {
	removed := ExtractConditions(`index=a | join user [search index=b | fields - secret]`)
	if len(removed.Joins) != 1 {
		t.Fatalf("want one join, got %d", len(removed.Joins))
	}
	if containsString(removed.Joins[0].ExposedFields, "secret") {
		t.Errorf("ExposedFields = %v contains the field that `fields - secret` removes", removed.Joins[0].ExposedFields)
	}
	if got := ClassifyFieldProvenance(removed, "secret"); got == ProvenanceJoined {
		t.Errorf("ClassifyFieldProvenance(secret) = %q for a field the subsearch removes", got)
	}

	for _, query := range []string{
		`index=a | join user [search index=b | fields kept]`,
		`index=a | join user [search index=b | fields + kept]`,
	} {
		kept := ExtractConditions(query)
		if len(kept.Joins) != 1 || !containsString(kept.Joins[0].ExposedFields, "kept") {
			t.Errorf("%s: want kept in ExposedFields, got joins %+v", query, kept.Joins)
		}
	}

	if main := ExtractConditions(`index=a | fields - secret`); containsString(main.GroupByFields, "secret") {
		t.Errorf("GroupByFields = %v lists the field that `fields - secret` removes", main.GroupByFields)
	}
}
