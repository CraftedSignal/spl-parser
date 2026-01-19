package spl

import (
	"encoding/json"
	"os"
	"testing"
)

// QueryEntry represents a single query from the corpus
type QueryEntry struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Query  string `json:"query"`
}

// TestCorpus tests the SPL parser against the corpus of real-world queries
func TestCorpus(t *testing.T) {
	corpusPath := "testdata/corpus.json"

	// Check if corpus exists
	if _, err := os.Stat(corpusPath); os.IsNotExist(err) {
		t.Skip("Corpus not available at testdata/corpus.json")
	}

	// Load corpus
	data, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatalf("Failed to read corpus: %v", err)
	}

	var queries []QueryEntry
	if err := json.Unmarshal(data, &queries); err != nil {
		t.Fatalf("Failed to parse corpus: %v", err)
	}

	t.Logf("Loaded %d queries from corpus", len(queries))

	var success, partial, failed, panics int

	for _, q := range queries {
		func() {
			defer func() {
				if r := recover(); r != nil {
					panics++
					t.Logf("PANIC on %s: %v", q.Name, r)
				}
			}()

			result := ExtractConditions(q.Query)

			if len(result.Conditions) > 0 && len(result.Errors) == 0 {
				success++
			} else if len(result.Conditions) > 0 {
				partial++
			} else {
				failed++
			}
		}()
	}

	total := success + partial + failed + panics
	t.Logf("Results: success=%d (%.1f%%), partial=%d (%.1f%%), failed=%d (%.1f%%), panics=%d",
		success, float64(success)*100/float64(total),
		partial, float64(partial)*100/float64(total),
		failed, float64(failed)*100/float64(total),
		panics)

	if panics > 0 {
		t.Errorf("Parser panicked on %d queries!", panics)
	}
}

// BenchmarkCorpus benchmarks parsing speed on the corpus
func BenchmarkCorpus(b *testing.B) {
	corpusPath := "testdata/corpus.json"

	if _, err := os.Stat(corpusPath); os.IsNotExist(err) {
		b.Skip("Corpus not available")
	}

	data, err := os.ReadFile(corpusPath)
	if err != nil {
		b.Fatalf("Failed to read corpus: %v", err)
	}

	var queries []QueryEntry
	if err := json.Unmarshal(data, &queries); err != nil {
		b.Fatalf("Failed to parse corpus: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			_ = ExtractConditions(q.Query)
		}
	}
}
