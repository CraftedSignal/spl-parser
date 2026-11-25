package spl

import "testing"

func BenchmarkExtractQuotedSearchValues(b *testing.B) {
	query := `search="(&(cn=Alice)(sn=Smith))" search='(&(uid=alice)(mail=alice@example.com))'`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = extractQuotedSearchValues(query)
	}
}

func BenchmarkExtractTstatsDatamodelValue(b *testing.B) {
	segment := `tstats count FROM datamodel=Endpoint.Processes where Processes.process_name="cmd.exe"`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = extractTstatsDatamodelValue(segment)
	}
}
