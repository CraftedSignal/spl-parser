package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type QueryEntry struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Query  string `json:"query"`
}

type Detection struct {
	Name   string `yaml:"name"`
	Search string `yaml:"search"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: scrape-corpus <detections-dir> [corpus.json]\n")
		fmt.Fprintf(os.Stderr, "  Reads Splunk Security Content YAML files and appends to corpus.\n")
		os.Exit(1)
	}

	detectionsDir := os.Args[1]
	corpusPath := "testdata/corpus.json"
	if len(os.Args) >= 3 {
		corpusPath = os.Args[2]
	}

	// Load existing corpus
	existing := make(map[string]bool)
	var corpus []QueryEntry

	data, err := os.ReadFile(corpusPath)
	if err == nil {
		if err := json.Unmarshal(data, &corpus); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing existing corpus: %v\n", err)
			os.Exit(1)
		}
		for _, q := range corpus {
			existing[q.Query] = true
		}
	}

	fmt.Printf("Existing corpus: %d entries\n", len(corpus))

	// Walk the detections directory
	var added int
	err = filepath.Walk(detectionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var det Detection
		if err := yaml.Unmarshal(raw, &det); err != nil {
			return nil
		}

		query := strings.TrimSpace(det.Search)
		if query == "" {
			return nil
		}

		// Deduplicate
		if existing[query] {
			return nil
		}
		existing[query] = true

		name := det.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}

		corpus = append(corpus, QueryEntry{
			Source: "splunk_security_content",
			Name:   name,
			Query:  query,
		})
		added++

		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Added: %d new queries\n", added)
	fmt.Printf("Total: %d entries\n", len(corpus))

	// Write corpus
	out, err := json.MarshalIndent(corpus, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling corpus: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(corpusPath, out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing corpus: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Written to %s\n", corpusPath)
}
