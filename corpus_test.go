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

// TestCorpus tests the SPL parser against the corpus of real-world queries.
// Distinguishes between:
// - success: conditions extracted with no errors
// - partial: conditions extracted but with errors
// - no_conditions: parsed cleanly but no filterable conditions (e.g., generating commands)
// - failed: parse errors and no conditions (real parser failure)
// - panic: parser crash
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

	var success, partial, noConditions, failed, panics int

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
			} else if len(result.Errors) > 0 {
				failed++
				t.Logf("FAILED: %s — %v", q.Name, result.Errors)
			} else {
				// Parsed cleanly but no conditions — not a parser failure
				noConditions++
			}
		}()
	}

	total := len(queries)
	testable := success + partial + failed + panics // excludes no-condition queries
	t.Logf("Results: success=%d, partial=%d, no_conditions=%d, failed=%d, panics=%d (total=%d)",
		success, partial, noConditions, failed, panics, total)
	if testable > 0 {
		parseRate := float64(success+partial) * 100 / float64(testable)
		t.Logf("Parse rate (of testable queries): %.1f%%", parseRate)
	}

	if panics > 0 {
		t.Errorf("Parser panicked on %d queries!", panics)
	}

	if failed > 0 {
		t.Errorf("Parser failed on %d queries with errors", failed)
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
