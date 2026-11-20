package spl

import (
	"reflect"
	"testing"
)

// The same query must report a join's exposed fields in the same, sorted
// order on every call.
func TestJoinExposedFieldsOrderIsStable(t *testing.T) {
	want := []string{"user", "w", "x", "y", "z"}
	for i := 0; i < 200; i++ {
		result := ExtractConditions(`index=a | join user [search index=b x=1 y=2 z=3 w=4]`)
		if len(result.Joins) != 1 {
			t.Fatalf("want one join, got %d", len(result.Joins))
		}
		if got := result.Joins[0].ExposedFields; !reflect.DeepEqual(got, want) {
			t.Fatalf("call %d: ExposedFields = %v, want %v", i, got, want)
		}
	}
}
