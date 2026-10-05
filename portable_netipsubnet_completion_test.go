package spl

import "testing"

func TestSPLPortableNetIPSubnetSkipEdges(t *testing.T) {
	conditions := extractPortablePredicatesFromText(`net_ipsubnet(and,"10.0.0.0/8") OR net_ipsubnet(src_ip,"")`, 9)
	if len(conditions) != 0 {
		t.Fatalf("invalid net_ipsubnet predicates produced conditions: %#v", conditions)
	}
}
