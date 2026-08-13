package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sigma "github.com/craftedsignal/sigma-parser"
	spl "github.com/craftedsignal/spl-parser"
	"gopkg.in/yaml.v3"
)

type generatedCase struct {
	ID       int64               `json:"id"`
	Seed     int64               `json:"seed"`
	Query    string              `json:"query"`
	Expected []expectedCondition `json:"expected"`
}

type expectedCondition struct {
	Field        string   `json:"field"`
	Operator     string   `json:"operator"`
	Value        string   `json:"value,omitempty"`
	Alternatives []string `json:"alternatives,omitempty"`
	Negated      bool     `json:"negated,omitempty"`
}

type queryEntry struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Query  string `json:"query"`
}

type inputQueryEntry struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Query  string `json:"query"`
}

type runResult struct {
	ID           int64         `json:"id"`
	Query        string        `json:"query,omitempty"`
	SigmaYAML    string        `json:"sigma_yaml,omitempty"`
	BackSPL      string        `json:"back_spl,omitempty"`
	Entry        *queryEntry   `json:"entry,omitempty"`
	Verification *verification `json:"verification,omitempty"`
	Failure      *failure      `json:"failure,omitempty"`
}

type failure struct {
	Stage    string   `json:"stage"`
	Reason   string   `json:"reason"`
	Errors   []string `json:"errors,omitempty"`
	Expected []string `json:"expected,omitempty"`
	Actual   []string `json:"actual,omitempty"`
	Missing  []string `json:"missing,omitempty"`
	Extra    []string `json:"extra,omitempty"`
}

type verification struct {
	ExpectedConditions int `json:"expected_conditions"`
	ParserConditions   int `json:"parser_conditions"`
	SigmaConditions    int `json:"sigma_conditions"`
	BackConditions     int `json:"back_conditions"`
}

type summary struct {
	Total             int64         `json:"total"`
	Passed            int64         `json:"passed"`
	Failed            int64         `json:"failed"`
	DuplicateQueries  int64         `json:"duplicate_queries"`
	ParserOracleCheck int64         `json:"parser_oracle_checks"`
	SigmaCheck        int64         `json:"sigma_checks"`
	BackCheck         int64         `json:"back_conversion_checks"`
	ExpectedConds     int64         `json:"expected_conditions_compared"`
	ParserConds       int64         `json:"parser_conditions_compared"`
	SigmaConds        int64         `json:"sigma_conditions_compared"`
	BackConds         int64         `json:"back_conditions_compared"`
	SPLParseErrors    int64         `json:"spl_parse_errors"`
	ExpectedMismatch  int64         `json:"expected_mismatch"`
	SigmaParseErrors  int64         `json:"sigma_parse_errors"`
	SigmaMismatch     int64         `json:"sigma_mismatch"`
	BackSPLParseError int64         `json:"back_spl_parse_errors"`
	BackSPLMismatch   int64         `json:"back_spl_mismatch"`
	Duration          time.Duration `json:"duration"`
}

type corpusWriter struct {
	file  *os.File
	enc   *json.Encoder
	first bool
}

type failureWriter struct {
	file *os.File
	enc  *json.Encoder
}

type sigmaRule struct {
	Title     string         `yaml:"title"`
	Status    string         `yaml:"status"`
	Logsource sigmaLogsource `yaml:"logsource"`
	Detection map[string]any `yaml:"detection"`
	Fields    []string       `yaml:"fields,omitempty"`
}

type sigmaLogsource struct {
	Category string `yaml:"category,omitempty"`
	Product  string `yaml:"product,omitempty"`
	Service  string `yaml:"service,omitempty"`
}

type sigmaSelection struct {
	Raw     any
	Negated bool
	Name    string
}

type normCondition struct {
	Field   string
	Op      string
	Value   string
	Negated bool
}

var (
	fields = []string{
		"EventCode", "EventID", "Image", "ParentImage", "CommandLine", "TargetFilename",
		"DestinationIp", "SourceIp", "DestinationPort", "SourcePort", "QueryName",
		"ProcessGuid", "ProcessId", "Logon_Type", "TargetUserName", "Account_Name",
		"user", "host", "src_ip", "dest_ip", "src_port", "dest_port", "status",
		"action", "method", "uri", "uri_path", "bytes", "duration", "response_time",
		"process.name", "process.pid", "user.name", "c-uri", "cs-user-agent",
		"registry.path", "file.hash", "message", "level", "severity",
	}
	numericFields = []string{"EventCode", "EventID", "DestinationPort", "SourcePort", "src_port", "dest_port", "status", "bytes", "duration", "response_time", "event_count"}
	stringFields  = []string{"Image", "ParentImage", "CommandLine", "TargetFilename", "QueryName", "TargetUserName", "Account_Name", "user", "host", "src_ip", "dest_ip", "action", "method", "uri", "uri_path", "process.name", "user.name", "c-uri", "cs-user-agent", "message"}
	commands      = []string{"stats", "eventstats", "streamstats", "table", "fields", "sort", "dedup", "head", "tail", "rename", "rex", "lookup", "bucket", "convert", "fillnull", "transaction", "spath", "top", "rare", "eval"}
)

func main() {
	var (
		total       = flag.Int64("n", 1_000_000, "number of generated SPL queries to test")
		seed        = flag.Int64("seed", 1337, "base random seed; use a negative value for crypto-random choices")
		workers     = flag.Int("workers", runtime.NumCPU(), "parallel worker count")
		corpusPath  = flag.String("corpus", "testdata/generated/spl_sigma_roundtrip_corpus.json", "verified generated corpus JSON array path; empty disables writing")
		failPath    = flag.String("failures", "testdata/generated/spl_sigma_roundtrip_failures.jsonl", "failure JSONL path; empty disables writing")
		failLimit   = flag.Int("failure-limit", 1000, "maximum failure records to write")
		progress    = flag.Int64("progress", 10000, "print progress every N completed cases")
		strict      = flag.Bool("strict", false, "exit non-zero if any generated case fails")
		stopOnFirst = flag.Bool("stop-on-first", false, "stop scheduling new work after first failure")
		unique      = flag.Bool("unique", false, "write only unique query strings; keep generating until n unique verified queries are written")
		inputCorpus = flag.String("input-corpus", "", "JSON array of real query entries to verify instead of generated cases")
	)
	flag.Parse()

	if *total < 0 {
		fatalf("-n must be non-negative")
	}
	if *workers <= 0 {
		*workers = 1
	}

	cw, err := newCorpusWriter(*corpusPath)
	if err != nil {
		fatalf("open corpus writer: %v", err)
	}
	defer cw.close()

	fw, err := newFailureWriter(*failPath)
	if err != nil {
		fatalf("open failure writer: %v", err)
	}
	defer fw.close()

	start := time.Now()
	if *inputCorpus != "" {
		s, failureRecords := runInputCorpus(*inputCorpus, *failLimit, cw, fw, start)
		printInputSummary(s, *inputCorpus, *failPath, failureRecords)
		if *strict && s.Failed > 0 {
			os.Exit(1)
		}
		return
	}

	s, failureRecords := runGenerated(*seed, *total, *workers, *failLimit, *progress, *stopOnFirst, *unique, cw, fw, start)
	printSummary(s, *corpusPath, *failPath, failureRecords)

	if *strict && s.Failed > 0 {
		os.Exit(1)
	}
}

func runGenerated(seed, target int64, workers, failLimit int, progress int64, stopOnFirst, unique bool, cw *corpusWriter, fw *failureWriter, start time.Time) (summary, int) {
	if unique {
		return runGeneratedUnique(seed, target, workers, failLimit, progress, stopOnFirst, cw, fw, start)
	}
	return runGeneratedFixed(seed, target, workers, failLimit, progress, stopOnFirst, cw, fw, start)
}

func runGeneratedFixed(seed, total int64, workers, failLimit int, progress int64, stopOnFirst bool, cw *corpusWriter, fw *failureWriter, start time.Time) (summary, int) {
	jobs := make(chan int64, workers*2)
	results := make(chan runResult, workers*2)
	done := make(chan struct{})
	var doneOnce sync.Once
	stop := func() {
		doneOnce.Do(func() {
			close(done)
		})
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				select {
				case <-done:
					return
				default:
				}
				results <- processID(seed, id)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for i := int64(0); i < total; i++ {
			select {
			case <-done:
				return
			case jobs <- i:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var s summary
	var failureRecords int
	for result := range results {
		acceptResult(result, &s, &failureRecords, failLimit, cw, fw)
		if result.Failure != nil && stopOnFirst {
			stop()
		}

		if progress > 0 && s.Total%progress == 0 {
			printProgress(s, total, start)
		}
		if stopOnFirst && s.Failed > 0 {
			break
		}
	}
	s.Duration = time.Since(start)
	return s, failureRecords
}

func runGeneratedUnique(seed, target int64, workers, failLimit int, progress int64, stopOnFirst bool, cw *corpusWriter, fw *failureWriter, start time.Time) (summary, int) {
	jobs := make(chan int64, workers*2)
	results := make(chan runResult, workers*2)
	seen := make(map[[32]byte]struct{}, mapCapacityHint(target))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				results <- processID(seed, id)
			}
		}()
	}

	var s summary
	var failureRecords int
	nextID := int64(0)
	active := 0
	maxActive := workers * 2
	jobsClosed := false
	closeJobs := func() {
		if !jobsClosed {
			close(jobs)
			jobsClosed = true
		}
	}
	schedule := func() {
		for !jobsClosed && active < maxActive && s.Passed+int64(active) < target {
			jobs <- nextID
			nextID++
			active++
		}
	}

	schedule()
	for active > 0 {
		result := <-results
		active--
		s.Total++

		if result.Failure == nil {
			hash := sha256.Sum256([]byte(result.Query))
			if _, ok := seen[hash]; ok {
				s.DuplicateQueries++
			} else {
				seen[hash] = struct{}{}
				s.Passed++
				recordVerification(result, &s)
				if result.Entry != nil {
					if err := cw.write(*result.Entry); err != nil {
						closeJobs()
						fatalf("write corpus: %v", err)
					}
				}
			}
		} else {
			recordFailure(result, &s, &failureRecords, failLimit, fw)
			if stopOnFirst {
				closeJobs()
			}
		}

		if !jobsClosed {
			schedule()
		}
		if s.Passed == target {
			closeJobs()
		}
		if progress > 0 && (s.Total%progress == 0 || s.Passed == target) {
			printProgress(s, target, start)
		}
	}

	closeJobs()
	wg.Wait()
	s.Duration = time.Since(start)
	return s, failureRecords
}

func runInputCorpus(path string, failLimit int, cw *corpusWriter, fw *failureWriter, start time.Time) (summary, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		fatalf("read input corpus: %v", err)
	}
	var entries []inputQueryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		fatalf("parse input corpus: %v", err)
	}

	var s summary
	var failureRecords int
	for i, entry := range entries {
		if strings.TrimSpace(entry.Query) == "" {
			continue
		}
		s.Total++
		result := processInputEntry(int64(i), entry)
		if result.Failure == nil {
			s.Passed++
			if result.Verification != nil {
				s.SigmaCheck++
				s.BackCheck++
				s.ParserConds += int64(result.Verification.ParserConditions)
				s.SigmaConds += int64(result.Verification.SigmaConditions)
				s.BackConds += int64(result.Verification.BackConditions)
			}
			if result.Entry != nil {
				if err := cw.write(*result.Entry); err != nil {
					fatalf("write corpus: %v", err)
				}
			}
			continue
		}
		recordFailure(result, &s, &failureRecords, failLimit, fw)
	}
	s.Duration = time.Since(start)
	return s, failureRecords
}

func acceptResult(result runResult, s *summary, failureRecords *int, failLimit int, cw *corpusWriter, fw *failureWriter) {
	s.Total++
	if result.Failure == nil {
		s.Passed++
		recordVerification(result, s)
		if result.Entry != nil {
			if err := cw.write(*result.Entry); err != nil {
				fatalf("write corpus: %v", err)
			}
		}
		return
	}
	recordFailure(result, s, failureRecords, failLimit, fw)
}

func recordVerification(result runResult, s *summary) {
	if result.Verification == nil {
		return
	}
	s.ParserOracleCheck++
	s.SigmaCheck++
	s.BackCheck++
	s.ExpectedConds += int64(result.Verification.ExpectedConditions)
	s.ParserConds += int64(result.Verification.ParserConditions)
	s.SigmaConds += int64(result.Verification.SigmaConditions)
	s.BackConds += int64(result.Verification.BackConditions)
}

func recordFailure(result runResult, s *summary, failureRecords *int, failLimit int, fw *failureWriter) {
	s.Failed++
	countFailureStage(s, result.Failure.Stage)
	if *failureRecords < failLimit {
		if err := fw.write(result); err != nil {
			fatalf("write failure: %v", err)
		}
		(*failureRecords)++
	}
}

func mapCapacityHint(target int64) int {
	if target <= 0 {
		return 0
	}
	if int64(int(target)) != target {
		return 0
	}
	return int(target)
}

func processInputEntry(id int64, entry inputQueryEntry) runResult {
	name := entry.Name
	if name == "" {
		name = entry.Title
	}
	result := runResult{
		ID:    id,
		Query: entry.Query,
		Entry: &queryEntry{
			Source: entry.Source,
			Name:   name,
			Query:  entry.Query,
		},
	}

	splResult := spl.ExtractConditions(entry.Query)
	if splResult == nil {
		result.Failure = &failure{Stage: "spl_parse", Reason: "ExtractConditions returned nil"}
		return result
	}
	if len(splResult.Errors) > 0 {
		result.Failure = &failure{Stage: "spl_parse", Reason: "SPL parser returned errors", Errors: splResult.Errors}
		return result
	}

	actualSPL := normalizeSPLConditions(flattenSPLConditions(splResult))
	if len(actualSPL) == 0 {
		result.Failure = &failure{Stage: "spl_parse", Reason: "SPL parser extracted no conditions"}
		return result
	}

	sigmaYAML, err := splResultToSigmaYAML(id, splResult)
	if err != nil {
		result.Failure = &failure{Stage: "sigma_build", Reason: err.Error()}
		return result
	}
	result.SigmaYAML = sigmaYAML

	sigmaResult := sigma.ExtractConditions(sigmaYAML)
	if sigmaResult == nil {
		result.Failure = &failure{Stage: "sigma_parse", Reason: "sigma ExtractConditions returned nil"}
		return result
	}
	if len(sigmaResult.Errors) > 0 {
		result.Failure = &failure{Stage: "sigma_parse", Reason: "Sigma parser returned errors", Errors: sigmaResult.Errors}
		return result
	}

	actualSigma := normalizeSigmaConditions(sigmaResult.Conditions)
	if missing, extra := compareConditionSets(actualSPL, actualSigma); len(missing) > 0 || len(extra) > 0 {
		result.Failure = &failure{
			Stage:    "sigma_mismatch",
			Reason:   "SPL parse result and Sigma parser result differ",
			Expected: conditionKeys(actualSPL),
			Actual:   conditionKeys(actualSigma),
			Missing:  conditionKeys(missing),
			Extra:    conditionKeys(extra),
		}
		return result
	}

	backSPL, err := sigmaResultToSPL(sigmaResult)
	if err != nil {
		result.Failure = &failure{Stage: "back_spl_build", Reason: err.Error()}
		return result
	}
	result.BackSPL = backSPL

	backResult := spl.ExtractConditions(backSPL)
	if backResult == nil {
		result.Failure = &failure{Stage: "back_spl_parse", Reason: "SPL parser returned nil for back-converted SPL"}
		return result
	}
	if len(backResult.Errors) > 0 {
		result.Failure = &failure{Stage: "back_spl_parse", Reason: "SPL parser returned errors for back-converted SPL", Errors: backResult.Errors}
		return result
	}

	actualBack := normalizeSPLConditions(flattenSPLConditions(backResult))
	if missing, extra := compareConditionSets(actualSigma, actualBack); len(missing) > 0 || len(extra) > 0 {
		result.Failure = &failure{
			Stage:    "back_spl_mismatch",
			Reason:   "Sigma parser result and back-converted SPL parser result differ",
			Expected: conditionKeys(actualSigma),
			Actual:   conditionKeys(actualBack),
			Missing:  conditionKeys(missing),
			Extra:    conditionKeys(extra),
		}
		return result
	}

	result.Verification = &verification{
		ParserConditions: len(actualSPL),
		SigmaConditions:  len(actualSigma),
		BackConditions:   len(actualBack),
	}
	return result
}

func processCase(tc generatedCase) runResult {
	result := runResult{ID: tc.ID, Query: tc.Query}

	splResult := spl.ExtractConditions(tc.Query)
	if splResult == nil {
		result.Failure = &failure{Stage: "spl_parse", Reason: "ExtractConditions returned nil"}
		return result
	}
	if len(splResult.Errors) > 0 {
		result.Failure = &failure{Stage: "spl_parse", Reason: "SPL parser returned errors", Errors: splResult.Errors}
		return result
	}

	expected := normalizeExpected(tc.Expected)
	actualSPL := normalizeSPLConditions(flattenSPLConditions(splResult))
	if missing, extra := compareConditionSets(expected, actualSPL); len(missing) > 0 || len(extra) > 0 {
		result.Failure = &failure{
			Stage:    "expected_mismatch",
			Reason:   "SPL parser extraction differed from generated semantic oracle",
			Expected: conditionKeys(expected),
			Actual:   conditionKeys(actualSPL),
			Missing:  conditionKeys(missing),
			Extra:    conditionKeys(extra),
		}
		return result
	}

	sigmaYAML, err := splResultToSigmaYAML(tc.ID, splResult)
	if err != nil {
		result.Failure = &failure{Stage: "sigma_build", Reason: err.Error()}
		return result
	}
	result.SigmaYAML = sigmaYAML

	sigmaResult := sigma.ExtractConditions(sigmaYAML)
	if sigmaResult == nil {
		result.Failure = &failure{Stage: "sigma_parse", Reason: "sigma ExtractConditions returned nil"}
		return result
	}
	if len(sigmaResult.Errors) > 0 {
		result.Failure = &failure{Stage: "sigma_parse", Reason: "Sigma parser returned errors", Errors: sigmaResult.Errors}
		return result
	}

	actualSigma := normalizeSigmaConditions(sigmaResult.Conditions)
	if missing, extra := compareConditionSets(actualSPL, actualSigma); len(missing) > 0 || len(extra) > 0 {
		result.Failure = &failure{
			Stage:    "sigma_mismatch",
			Reason:   "SPL parse result and Sigma parser result differ",
			Expected: conditionKeys(actualSPL),
			Actual:   conditionKeys(actualSigma),
			Missing:  conditionKeys(missing),
			Extra:    conditionKeys(extra),
		}
		return result
	}

	backSPL, err := sigmaResultToSPL(sigmaResult)
	if err != nil {
		result.Failure = &failure{Stage: "back_spl_build", Reason: err.Error()}
		return result
	}
	result.BackSPL = backSPL

	backResult := spl.ExtractConditions(backSPL)
	if backResult == nil {
		result.Failure = &failure{Stage: "back_spl_parse", Reason: "SPL parser returned nil for back-converted SPL"}
		return result
	}
	if len(backResult.Errors) > 0 {
		result.Failure = &failure{Stage: "back_spl_parse", Reason: "SPL parser returned errors for back-converted SPL", Errors: backResult.Errors}
		return result
	}

	actualBack := normalizeSPLConditions(flattenSPLConditions(backResult))
	if missing, extra := compareConditionSets(actualSigma, actualBack); len(missing) > 0 || len(extra) > 0 {
		result.Failure = &failure{
			Stage:    "back_spl_mismatch",
			Reason:   "Sigma parser result and back-converted SPL parser result differ",
			Expected: conditionKeys(actualSigma),
			Actual:   conditionKeys(actualBack),
			Missing:  conditionKeys(missing),
			Extra:    conditionKeys(extra),
		}
		return result
	}

	result.Entry = &queryEntry{
		Source: "generated_spl_sigma_roundtrip",
		Name:   fmt.Sprintf("generated_%09d", tc.ID),
		Query:  tc.Query,
	}
	result.Verification = &verification{
		ExpectedConditions: len(expected),
		ParserConditions:   len(actualSPL),
		SigmaConditions:    len(actualSigma),
		BackConditions:     len(actualBack),
	}
	return result
}

func processID(seed, id int64) (result runResult) {
	result = runResult{ID: id}
	defer func() {
		if r := recover(); r != nil {
			result.Failure = &failure{
				Stage:  "generator_panic",
				Reason: fmt.Sprintf("panic while generating or processing case: %v", r),
			}
		}
	}()

	tc := generateCase(seed, id)
	return processCase(tc)
}

func generateCase(seed, id int64) generatedCase {
	return generateBoundedSemanticCase(seed, id)
}

func seedForCase(seed, id int64) int64 {
	x := uint64(seed) + uint64(id)*0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return int64(x ^ (x >> 31))
}

func generateSimpleSearch(rng *rand.Rand) (string, []expectedCondition) {
	var parts []string
	var expected []expectedCondition

	idx := expectedCondition{Field: "index", Operator: "=", Value: oneOf(rng, "main", "windows", "sysmon", "auth", "dns", "proxy", "firewall")}
	st := expectedCondition{Field: "sourcetype", Operator: "=", Value: oneOf(rng, "WinEventLog:Security", "XmlWinEventLog:Microsoft-Windows-Sysmon/Operational", "access_combined", "aws:cloudtrail", "o365:management:activity")}
	parts = append(parts, renderCondition(idx), renderCondition(st))
	expected = append(expected, idx, st)

	for i := 0; i < rng.Intn(5)+2; i++ {
		cond := randomCondition(rng)
		parts = append(parts, renderCondition(cond))
		expected = append(expected, cond)
	}

	return joinSearchTerms(rng, parts), expected
}

func generateBooleanSearch(rng *rand.Rand) (string, []expectedCondition) {
	conditions := make([]expectedCondition, rng.Intn(6)+5)
	terms := make([]string, len(conditions))
	for i := range conditions {
		conditions[i] = randomCondition(rng)
		if rng.Intn(6) == 0 {
			conditions[i].Negated = !conditions[i].Negated
		}
		terms[i] = renderCondition(conditions[i])
	}

	var query string
	switch rng.Intn(4) {
	case 0:
		query = fmt.Sprintf("(%s OR %s) AND (%s OR %s) %s",
			terms[0], terms[1], terms[2], terms[3], strings.Join(terms[4:], " "))
	case 1:
		query = fmt.Sprintf("(%s AND (%s OR %s)) OR (%s AND %s)",
			terms[0], terms[1], terms[2], terms[3], terms[4])
		if len(terms) > 5 {
			query += " " + strings.Join(terms[5:], " ")
		}
	case 2:
		query = fmt.Sprintf("NOT (%s OR %s) %s", terms[0], terms[1], strings.Join(terms[2:], " "))
		conditions[0].Negated = !conditions[0].Negated
		conditions[1].Negated = !conditions[1].Negated
	default:
		query = strings.Join(terms, " OR ")
	}

	if rng.Intn(2) == 0 {
		query = "search " + query
	}
	return applyFormatting(rng, query), conditions
}

func generatePipelineSearch(rng *rand.Rand) (string, []expectedCondition) {
	query, expected := generateSimpleSearch(rng)
	stageCount := rng.Intn(9) + 3

	for i := 0; i < stageCount; i++ {
		cmd, conds := randomPipelineCommand(rng)
		query += pipe(rng) + cmd
		expected = append(expected, conds...)
	}

	return applyFormatting(rng, query), expected
}

func generateSubsearchPipeline(rng *rand.Rand) (string, []expectedCondition) {
	baseQuery, expected := generateSimpleSearch(rng)
	subQuery, subExpected := generateSimpleSearch(rng)
	joinField := oneOf(rng, "user", "src_ip", "dest_ip", "host", "ProcessGuid")
	joinType := oneOf(rng, "inner", "left", "outer")

	switch rng.Intn(4) {
	case 0:
		baseQuery += pipe(rng) + fmt.Sprintf("join type=%s max=%d %s [ search %s | fields %s threat_score ]", joinType, rng.Intn(3)+1, joinField, subQuery, joinField)
	case 1:
		baseQuery += pipe(rng) + fmt.Sprintf("append [ search %s | table %s indicator ]", subQuery, joinField)
	case 2:
		baseQuery += fmt.Sprintf(" [ search %s | fields %s ]", subQuery, joinField)
	default:
		nestedQuery, nestedExpected := generateSimpleSearch(rng)
		subExpected = append(subExpected, nestedExpected...)
		baseQuery += pipe(rng) + fmt.Sprintf("join %s [ search %s [ search %s | fields %s ] | stats count by %s ]", joinField, subQuery, nestedQuery, joinField, joinField)
	}

	expected = append(expected, subExpected...)
	if rng.Intn(2) == 0 {
		cond := expectedCondition{Field: "threat_score", Operator: ">", Value: strconv.Itoa(rng.Intn(90) + 10)}
		baseQuery += pipe(rng) + "where " + renderCondition(cond)
		expected = append(expected, cond)
	}

	return applyFormatting(rng, baseQuery), expected
}

func generateTstatsSearch(rng *rand.Rand) (string, []expectedCondition) {
	indexCond := expectedCondition{Field: "index", Operator: "=", Value: oneOf(rng, "windows", "sysmon", "auth", "dns", "proxy")}
	eventCond := expectedCondition{Field: "EventCode", Operator: "=", Value: oneOf(rng, "1", "3", "11", "22", "4624", "4625", "4688")}
	groupFields := []string{oneOf(rng, "host", "user", "src_ip", "dest_ip", "Image"), oneOf(rng, "sourcetype", "EventCode", "action", "status")}
	query := fmt.Sprintf("| tstats summariesonly=%s count as event_count from datamodel=%s where %s %s by %s %s",
		oneOf(rng, "true", "false"),
		oneOf(rng, "Endpoint.Processes", "Authentication.Authentication", "Network_Traffic.All_Traffic"),
		renderCondition(indexCond),
		renderCondition(eventCond),
		groupFields[0],
		groupFields[1],
	)
	expected := []expectedCondition{indexCond, eventCond}
	if rng.Intn(2) == 0 {
		cond := expectedCondition{Field: "event_count", Operator: ">", Value: strconv.Itoa(rng.Intn(50) + 1)}
		query += pipe(rng) + "where " + renderCondition(cond)
		expected = append(expected, cond)
	}
	return applyFormatting(rng, query), expected
}

func generateLargeSearch(rng *rand.Rand) (string, []expectedCondition) {
	var parts []string
	var expected []expectedCondition

	eventValues := uniqueValues(rng, []string{"1", "3", "7", "10", "11", "12", "13", "15", "22", "23", "25", "4624", "4625", "4634", "4648", "4688", "4698", "4720", "4726", "4769"}, rng.Intn(12)+8)
	inCond := expectedCondition{Field: "EventCode", Operator: "in", Alternatives: eventValues, Value: eventValues[0]}
	parts = append(parts, "index="+oneOf(rng, "windows", "sysmon", "main"), renderCondition(inCond))
	expected = append(expected, expectedCondition{Field: "index", Operator: "=", Value: strings.TrimPrefix(parts[0], "index=")}, inCond)

	for i := 0; i < rng.Intn(18)+12; i++ {
		cond := randomCondition(rng)
		parts = append(parts, renderCondition(cond))
		expected = append(expected, cond)
	}

	query := joinSearchTerms(rng, parts)
	if rng.Intn(2) == 0 {
		query += pipe(rng) + "stats count as event_count dc(host) as unique_hosts by user host EventCode"
		cond := expectedCondition{Field: "event_count", Operator: ">=", Value: strconv.Itoa(rng.Intn(20) + 1)}
		query += pipe(rng) + "where " + renderCondition(cond)
		expected = append(expected, cond)
	}
	query += pipe(rng) + "sort - event_count" + pipe(rng) + fmt.Sprintf("head %d", rng.Intn(250)+10)
	return applyFormatting(rng, query), expected
}

func generateWindowsDetection(rng *rand.Rand) (string, []expectedCondition) {
	templates := []struct {
		query    string
		expected []expectedCondition
	}{
		{
			query: `index=sysmon sourcetype=XmlWinEventLog:Microsoft-Windows-Sysmon/Operational EventCode=1 ParentImage="*\\WINWORD.EXE" (Image="*\\cmd.exe" OR Image="*\\powershell.exe" OR Image="*\\wscript.exe") CommandLine="*-enc*" NOT user="SYSTEM" | stats count by host user Image ParentImage CommandLine | sort - count`,
			expected: []expectedCondition{
				{Field: "index", Operator: "=", Value: "sysmon"},
				{Field: "sourcetype", Operator: "=", Value: "XmlWinEventLog:Microsoft-Windows-Sysmon/Operational"},
				{Field: "EventCode", Operator: "=", Value: "1"},
				{Field: "ParentImage", Operator: "=", Value: `*\\WINWORD.EXE`},
				{Field: "Image", Operator: "=", Value: `*\\cmd.exe`},
				{Field: "Image", Operator: "=", Value: `*\\powershell.exe`},
				{Field: "Image", Operator: "=", Value: `*\\wscript.exe`},
				{Field: "CommandLine", Operator: "=", Value: `*-enc*`},
				{Field: "user", Operator: "=", Value: "SYSTEM", Negated: true},
			},
		},
		{
			query: `index=windows source="WinEventLog:Security" EventCode IN (4624, 4625, 4634, 4648) Logon_Type IN (2, 3, 7, 10) TargetUserName!="*$" | eval user=lower(TargetUserName) | bucket _time span=5m | stats count as auth_count by user host Logon_Type | where auth_count>10 | sort - auth_count`,
			expected: []expectedCondition{
				{Field: "index", Operator: "=", Value: "windows"},
				{Field: "source", Operator: "=", Value: "WinEventLog:Security"},
				{Field: "EventCode", Operator: "in", Value: "4624", Alternatives: []string{"4624", "4625", "4634", "4648"}},
				{Field: "Logon_Type", Operator: "in", Value: "2", Alternatives: []string{"2", "3", "7", "10"}},
				{Field: "TargetUserName", Operator: "=", Value: "*$", Negated: true},
				{Field: "auth_count", Operator: ">", Value: "10"},
			},
		},
		{
			query: `index=dns QueryName="*.onion" OR QueryName="*pastebin*" OR QueryName="*ngrok*" | stats count as dns_hits by src_ip QueryName | where dns_hits>=3 | sort - dns_hits`,
			expected: []expectedCondition{
				{Field: "index", Operator: "=", Value: "dns"},
				{Field: "QueryName", Operator: "=", Value: "*.onion"},
				{Field: "QueryName", Operator: "=", Value: "*pastebin*"},
				{Field: "QueryName", Operator: "=", Value: "*ngrok*"},
				{Field: "dns_hits", Operator: ">=", Value: "3"},
			},
		},
	}
	t := templates[rng.Intn(len(templates))]
	return applyFormatting(rng, t.query), t.expected
}

func generateWhereFunctionSearch(rng *rand.Rand) (string, []expectedCondition) {
	query, expected := generateSimpleSearch(rng)
	var conds []expectedCondition

	switch rng.Intn(5) {
	case 0:
		cond := expectedCondition{Field: "CommandLine", Operator: "matches", Value: `(?i)(powershell|cmd|wscript)\.exe`}
		query += pipe(rng) + `where match(CommandLine, "(?i)(powershell|cmd|wscript)\.exe")`
		conds = append(conds, cond)
	case 1:
		cond := expectedCondition{Field: "src_ip", Operator: "cidrmatch", Value: oneOf(rng, "10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12")}
		query += pipe(rng) + fmt.Sprintf(`where cidrmatch("%s", src_ip)`, cond.Value)
		conds = append(conds, cond)
	case 2:
		cond := expectedCondition{Field: "user", Operator: "isnotnull"}
		query += pipe(rng) + "where isnotnull(user)"
		conds = append(conds, cond)
	case 3:
		cond := expectedCondition{Field: "Image", Operator: "like", Value: "*powershell*"}
		query += pipe(rng) + `where like(Image, "%powershell%")`
		conds = append(conds, cond)
	default:
		a := expectedCondition{Field: "bytes", Operator: ">", Value: strconv.Itoa(rng.Intn(4000) + 100)}
		b := expectedCondition{Field: "duration", Operator: "<=", Value: strconv.Itoa(rng.Intn(3600) + 60)}
		query += pipe(rng) + fmt.Sprintf("eval kb=bytes/1024, rounded=round(duration/60,2)%swhere (%s AND %s)",
			pipe(rng), renderCondition(a), renderCondition(b))
		conds = append(conds, a, b)
	}

	expected = append(expected, conds...)
	query += pipe(rng) + oneOf(rng, "table _time host user Image CommandLine", "fields _time host src_ip dest_ip user", "sort - _time", "head 100")
	return applyFormatting(rng, query), expected
}

func generateFormattedSearch(rng *rand.Rand) (string, []expectedCondition) {
	query, expected := generatePipelineSearch(rng)
	query = strings.ReplaceAll(query, " | ", "\n    | ")
	query = strings.ReplaceAll(query, " AND ", "\n        AND ")
	query = strings.ReplaceAll(query, " OR ", "\n        OR ")
	if rng.Intn(2) == 0 {
		trimmed := strings.TrimSpace(query)
		if !strings.HasPrefix(strings.ToLower(trimmed), "search ") && !strings.HasPrefix(trimmed, "|") {
			query = "\n\tsearch " + trimmed + "\n"
		}
	}
	return query, expected
}

func randomPipelineCommand(rng *rand.Rand) (string, []expectedCondition) {
	field := oneOf(rng, fields...)
	switch oneOf(rng, commands...) {
	case "stats":
		return fmt.Sprintf("stats count as event_count sum(bytes) as total_bytes dc(host) as unique_hosts by %s %s", field, oneOf(rng, "host", "user", "src_ip", "dest_ip")), nil
	case "eventstats":
		return fmt.Sprintf("eventstats avg(bytes) as avg_bytes by %s", oneOf(rng, "host", "user", "src_ip")), nil
	case "streamstats":
		return fmt.Sprintf("streamstats count as stream_count by %s", oneOf(rng, "session_id", "user", "host")), nil
	case "table":
		return fmt.Sprintf("table _time host user %s %s", field, oneOf(rng, "Image", "CommandLine", "src_ip")), nil
	case "fields":
		if rng.Intn(3) == 0 {
			return fmt.Sprintf("fields - _raw _indextime %s", oneOf(rng, "debug", "temp", "payload")), nil
		}
		return fmt.Sprintf("fields _time host user %s %s", field, oneOf(rng, "Image", "CommandLine", "src_ip")), nil
	case "sort":
		return fmt.Sprintf("sort %s %s", oneOf(rng, "-", "+"), field), nil
	case "dedup":
		return fmt.Sprintf("dedup %d %s %s", rng.Intn(5)+1, field, oneOf(rng, "host", "user", "src_ip")), nil
	case "head":
		return fmt.Sprintf("head %d", rng.Intn(500)+1), nil
	case "tail":
		return fmt.Sprintf("tail %d", rng.Intn(250)+1), nil
	case "rename":
		return fmt.Sprintf("rename %s AS renamed_%s", field, sanitizeField(field)), nil
	case "rex":
		source := oneOf(rng, "_raw", "CommandLine", "message", "uri")
		return fmt.Sprintf(`rex field=%s "(?<extracted_%d>[A-Za-z0-9_./:-]+)"`, source, rng.Intn(100)), nil
	case "lookup":
		return fmt.Sprintf("lookup threat_lookup %s OUTPUT threat_score category", field), nil
	case "bucket":
		return fmt.Sprintf("bucket _time span=%s", oneOf(rng, "1m", "5m", "1h", "1d")), nil
	case "convert":
		return fmt.Sprintf(`convert timeformat="%s" ctime(_time) AS readable_time`, oneOf(rng, "%Y-%m-%d", "%Y/%m/%d %H:%M:%S")), nil
	case "fillnull":
		return fmt.Sprintf(`fillnull value="%s" %s`, oneOf(rng, "unknown", "N/A", "0"), field), nil
	case "transaction":
		return fmt.Sprintf("transaction %s maxspan=%s", oneOf(rng, "user", "session_id", "src_ip"), oneOf(rng, "5m", "30m", "1h")), nil
	case "spath":
		return fmt.Sprintf("spath output=json_%s path=data.%s", sanitizeField(field), strings.ReplaceAll(field, ".", ".")), nil
	case "top":
		return fmt.Sprintf("top %d %s by %s", rng.Intn(20)+5, field, oneOf(rng, "host", "user", "src_ip")), nil
	case "rare":
		return fmt.Sprintf("rare %d %s by %s", rng.Intn(20)+5, field, oneOf(rng, "host", "user", "src_ip")), nil
	default:
		if rng.Intn(2) == 0 {
			cond := randomWhereCondition(rng)
			return "where " + renderCondition(cond), []expectedCondition{cond}
		}
		cond := randomCondition(rng)
		return "search " + renderCondition(cond), []expectedCondition{cond}
	}
}

func randomCondition(rng *rand.Rand) expectedCondition {
	field := oneOf(rng, fields...)
	if rng.Intn(10) == 0 {
		field = strconv.Itoa(oneOfInt(rng, 1, 3, 7, 10, 11, 22, 42, 255))
	}

	if contains(numericFields, field) || isNumericFieldName(field) {
		op := oneOf(rng, "=", "!=", ">", "<", ">=", "<=")
		return expectedCondition{Field: field, Operator: op, Value: strconv.Itoa(rng.Intn(9000) + 1)}
	}

	if rng.Intn(7) == 0 {
		values := uniqueValues(rng, []string{"success", "failure", "blocked", "allowed", "admin", "root", "SYSTEM", "*.exe", "*powershell*", "cmd.exe", "10.*", "web01"}, rng.Intn(5)+2)
		return expectedCondition{Field: field, Operator: "in", Value: values[0], Alternatives: values}
	}

	op := oneOf(rng, "=", "!=")
	return expectedCondition{Field: field, Operator: op, Value: randomValueForField(rng, field)}
}

func randomWhereCondition(rng *rand.Rand) expectedCondition {
	field := oneOf(rng, numericFields...)
	op := oneOf(rng, ">", "<", ">=", "<=", "=", "!=")
	return expectedCondition{Field: field, Operator: op, Value: strconv.Itoa(rng.Intn(10000) + 1)}
}

func randomValueForField(rng *rand.Rand, field string) string {
	switch field {
	case "Image", "ParentImage":
		return oneOf(rng, `*\\cmd.exe`, `*\\powershell.exe`, `*\\rundll32.exe`, `*\\svchost.exe`, `C:\\Windows\\System32\\cmd.exe`)
	case "CommandLine":
		return oneOf(rng, `*-enc*`, `*downloadstring*`, `*whoami*`, `*net user*`, `* /c *`)
	case "TargetFilename":
		return oneOf(rng, `C:\\Users\\*\\AppData\\*.exe`, `*\\Temp\\*.dll`, `*.ps1`, `*.exe`)
	case "QueryName":
		return oneOf(rng, "*.onion", "*pastebin*", "*ngrok*", "*.example.com", "update.microsoft.com")
	case "src_ip", "dest_ip", "SourceIp", "DestinationIp":
		return oneOf(rng, "10.0.0.*", "192.168.1.*", "172.16.*", "8.8.8.8", "203.0.113.10")
	case "action":
		return oneOf(rng, "success", "failure", "blocked", "allowed", "created", "deleted")
	case "method":
		return oneOf(rng, "GET", "POST", "PUT", "DELETE")
	case "uri", "uri_path", "c-uri":
		return oneOf(rng, "/admin/*", "/api/v1/*", "*.php", "/login", "/owa/auth/*")
	case "cs-user-agent":
		return oneOf(rng, "*sqlmap*", "*nikto*", "Mozilla/*", "*curl*")
	case "host":
		return oneOf(rng, "web01", "dc01", "srv-*", "workstation-*")
	case "user", "TargetUserName", "Account_Name", "user.name":
		return oneOf(rng, "admin", "root", "SYSTEM", "*$", "svc_*", "john.doe")
	default:
		return oneOf(rng, "success", "failure", "critical", "medium", "test", "value_*")
	}
}

func renderCondition(cond expectedCondition) string {
	body := ""
	if strings.EqualFold(cond.Operator, "in") {
		values := cond.Alternatives
		if len(values) == 0 {
			values = []string{cond.Value}
		}
		body = fmt.Sprintf("%s IN (%s)", cond.Field, joinValues(values))
	} else if cond.Operator == "isnotnull" {
		body = fmt.Sprintf("isnotnull(%s)", cond.Field)
	} else if cond.Operator == "isnull" {
		body = fmt.Sprintf("isnull(%s)", cond.Field)
	} else if cond.Operator == "matches" {
		body = fmt.Sprintf("match(%s, %s)", cond.Field, formatSPLValue(cond.Value))
	} else if cond.Operator == "cidrmatch" {
		body = fmt.Sprintf("cidrmatch(%s, %s)", formatSPLValue(cond.Value), cond.Field)
	} else if cond.Operator == "like" {
		body = fmt.Sprintf("like(%s, %s)", cond.Field, formatSPLValue(strings.ReplaceAll(cond.Value, "*", "%")))
	} else {
		body = fmt.Sprintf("%s%s%s", cond.Field, cond.Operator, formatSPLValue(cond.Value))
	}
	if cond.Negated {
		return "NOT " + body
	}
	return body
}

func joinSearchTerms(rng *rand.Rand, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			switch rng.Intn(6) {
			case 0:
				b.WriteString(" AND ")
			case 1:
				b.WriteString("\t")
			case 2:
				b.WriteString("\n")
			default:
				b.WriteString(" ")
			}
		}
		if rng.Intn(10) == 0 {
			b.WriteString("(")
			b.WriteString(part)
			b.WriteString(")")
		} else {
			b.WriteString(part)
		}
	}
	return b.String()
}

func pipe(rng *rand.Rand) string {
	return oneOf(rng, " | ", "\n| ", "\n    | ", "\t|\t")
}

func applyFormatting(rng *rand.Rand, query string) string {
	if rng.Intn(4) == 0 && !strings.HasPrefix(strings.TrimSpace(query), "|") && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(query)), "search ") {
		query = "search " + query
	}
	if rng.Intn(3) == 0 {
		query = spaceEquals(query, oneOf(rng, " = ", "\t=\t"))
	}
	if rng.Intn(5) == 0 {
		query = "\n" + query + "\n"
	}
	return query
}

func splResultToSigmaYAML(id int64, result *spl.ParseResult) (string, error) {
	conditions := flattenSPLConditions(result)
	if len(conditions) == 0 {
		return "", fmt.Errorf("no SPL conditions to convert")
	}

	rule := sigmaRule{
		Title:     fmt.Sprintf("Generated SPL Roundtrip %d", id),
		Status:    "test",
		Logsource: inferLogsource(conditions),
		Detection: make(map[string]any, len(conditions)+1),
	}

	conditionParts := make([]string, 0, len(conditions)*2)
	fieldSet := make(map[string]bool)
	for i, cond := range conditions {
		selection := splConditionToSigmaSelection(cond, fmt.Sprintf("selection_%06d", i))
		rule.Detection[selection.Name] = selection.Raw

		if len(conditionParts) > 0 {
			op := strings.ToLower(cond.LogicalOp)
			if op != "or" {
				op = "and"
			}
			conditionParts = append(conditionParts, op)
		}

		ref := selection.Name
		if selection.Negated {
			ref = "not " + ref
		}
		conditionParts = append(conditionParts, ref)

		if cond.Field != "" {
			fieldSet[cond.Field] = true
		}
	}
	rule.Detection["condition"] = strings.Join(conditionParts, " ")

	for field := range fieldSet {
		rule.Fields = append(rule.Fields, field)
	}
	sort.Strings(rule.Fields)

	data, err := yaml.Marshal(rule)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func splConditionToSigmaSelection(cond spl.Condition, name string) sigmaSelection {
	field := cond.Field
	op := strings.ToLower(cond.Operator)
	negated := cond.Negated
	values := conditionValues(cond.Value, cond.Alternatives)
	if op == "starts_with" {
		op = "startswith"
	}

	if field == "_raw" && op == "contains" {
		if len(values) == 1 {
			return sigmaSelection{Name: name, Raw: values[0], Negated: negated}
		}
		return sigmaSelection{Name: name, Raw: values, Negated: negated}
	}

	key := field
	var value any = sigmaValue(values)
	switch op {
	case "=", "==":
		if len(values) == 1 && first(values) == "*" {
			key += "|exists"
			value = true
		}
	case "in":
		value = values
	case "!=":
		negated = !negated
	case ">":
		key += "|gt"
	case "<":
		key += "|lt"
	case ">=":
		key += "|gte"
	case "<=":
		key += "|lte"
	case "matches":
		key += "|re"
	case "cidrmatch":
		key += "|cidr"
	case "like":
		modifier, stripped := sigmaModifierFromLike(first(values))
		key += modifier
		if len(values) == 1 {
			value = stripped
		} else {
			strippedValues := make([]string, 0, len(values))
			for _, pattern := range values {
				nextModifier, nextStripped := sigmaModifierFromLike(pattern)
				if nextModifier != modifier {
					nextStripped = pattern
				}
				strippedValues = append(strippedValues, nextStripped)
			}
			value = strippedValues
		}
	case "isnotnull":
		key += "|exists"
		value = true
	case "isnull":
		key += "|exists"
		value = false
	default:
		if op != "" {
			key += "|" + op
		}
	}

	return sigmaSelection{Name: name, Raw: map[string]any{key: value}, Negated: negated}
}

func sigmaValue(values []string) any {
	if len(values) > 1 {
		return values
	}
	return first(values)
}

func sigmaResultToSPL(result *sigma.ParseResult) (string, error) {
	if result == nil || len(result.Conditions) == 0 {
		return "", fmt.Errorf("no Sigma conditions to convert")
	}

	parts := make([]string, 0, len(result.Conditions)*2)
	for i, cond := range result.Conditions {
		if i > 0 {
			logical := strings.ToUpper(cond.LogicalOp)
			if logical != "OR" {
				logical = "AND"
			}
			parts = append(parts, logical)
		}

		expr := sigmaConditionToSPL(cond)
		if cond.Negated {
			expr = "NOT (" + expr + ")"
		}
		parts = append(parts, expr)
	}

	return strings.Join(parts, " "), nil
}

func sigmaConditionToSPL(cond sigma.Condition) string {
	values := conditionValues(cond.Value, cond.Alternatives)
	if cond.Field == "" || cond.Operator == "keyword" {
		return joinAlternativeExpressions(values, func(v string) string {
			return quoteSPLString(v)
		})
	}

	switch cond.Operator {
	case "=":
		if len(values) > 1 {
			return fmt.Sprintf("%s IN (%s)", cond.Field, joinValues(values))
		}
		return fmt.Sprintf("%s=%s", cond.Field, formatSPLValue(first(values)))
	case ">":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("%s>%s", cond.Field, formatSPLValue(v))
		})
	case ">=":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("%s>=%s", cond.Field, formatSPLValue(v))
		})
	case "<":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("%s<%s", cond.Field, formatSPLValue(v))
		})
	case "<=":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("%s<=%s", cond.Field, formatSPLValue(v))
		})
	case "contains":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("like(%s,%s)", cond.Field, formatSPLValue("%"+v+"%"))
		})
	case "startswith":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("like(%s,%s)", cond.Field, formatSPLValue(v+"%"))
		})
	case "endswith":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("like(%s,%s)", cond.Field, formatSPLValue("%"+v))
		})
	case "matches":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("match(%s,%s)", cond.Field, formatSPLValue(v))
		})
	case "cidrmatch":
		return joinAlternativeExpressions(values, func(v string) string {
			return fmt.Sprintf("cidrmatch(%s,%s)", formatSPLValue(v), cond.Field)
		})
	case "exists":
		if strings.EqualFold(first(values), "false") || first(values) == "0" {
			return fmt.Sprintf("isnull(%s)", cond.Field)
		}
		return fmt.Sprintf("isnotnull(%s)", cond.Field)
	case "fieldref":
		return fmt.Sprintf("%s=%s", cond.Field, first(values))
	default:
		return fmt.Sprintf("%s=%s", cond.Field, formatSPLValue(first(values)))
	}
}

func joinAlternativeExpressions(values []string, render func(string) string) string {
	if len(values) == 0 {
		return render("")
	}
	if len(values) == 1 {
		return render(values[0])
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, render(value))
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func flattenSPLConditions(result *spl.ParseResult) []spl.Condition {
	if result == nil {
		return nil
	}
	conditions := append([]spl.Condition(nil), result.Conditions...)
	for _, subsearch := range result.Subsearches {
		conditions = append(conditions, flattenSPLConditions(subsearch)...)
	}
	for _, join := range result.Joins {
		conditions = append(conditions, flattenSPLConditions(join.Subsearch)...)
	}
	return conditions
}

func normalizeExpected(conditions []expectedCondition) []normCondition {
	normalized := make([]normCondition, 0, len(conditions))
	for _, cond := range conditions {
		values := conditionValues(cond.Value, cond.Alternatives)
		for _, value := range values {
			normalized = append(normalized, normalizeParts(cond.Field, cond.Operator, value, cond.Negated))
		}
	}
	return dedupeNorm(normalized)
}

func normalizeSPLConditions(conditions []spl.Condition) []normCondition {
	normalized := make([]normCondition, 0, len(conditions))
	for _, cond := range conditions {
		values := conditionValues(cond.Value, cond.Alternatives)
		for _, value := range values {
			normalized = append(normalized, normalizeParts(cond.Field, cond.Operator, value, cond.Negated))
		}
	}
	return dedupeNorm(normalized)
}

func normalizeSigmaConditions(conditions []sigma.Condition) []normCondition {
	normalized := make([]normCondition, 0, len(conditions))
	for _, cond := range conditions {
		values := conditionValues(cond.Value, cond.Alternatives)
		for _, value := range values {
			field := cond.Field
			op := cond.Operator
			if field == "" && op == "keyword" {
				field = "_raw"
				op = "contains"
			}
			normalized = append(normalized, normalizeParts(field, op, value, cond.Negated))
		}
	}
	return dedupeNorm(normalized)
}

func normalizeParts(field, op, value string, negated bool) normCondition {
	field = strings.ToLower(strings.TrimSpace(field))
	op = strings.ToLower(strings.TrimSpace(op))
	value = strings.TrimSpace(value)
	value = normalizeEscapedQuotes(value)
	value = trimMatchingQuotes(value)
	value = normalizeEscapedQuotes(value)
	value = trimMatchingQuotes(value)
	value = collapseTerminalEscapedBackslash(value)

	switch op {
	case "in", "==":
		op = "="
	case "!=":
		op = "="
		negated = !negated
	case "like":
		modifier, stripped := sigmaModifierFromLike(value)
		value = stripped
		switch modifier {
		case "|contains":
			op = "contains"
		case "|startswith":
			op = "startswith"
		case "|endswith":
			op = "endswith"
		default:
			op = "matches"
		}
	case "starts_with":
		op = "startswith"
	case "isnotnull":
		op = "exists"
		value = "true"
	case "isnull":
		op = "exists"
		value = "false"
	case "cidr":
		op = "cidrmatch"
	case "re":
		op = "matches"
	case "keyword":
		field = "_raw"
		op = "contains"
	}

	if op == "=" && value == "*" {
		op = "exists"
		value = "true"
	}

	if op == "exists" && negated {
		negated = false
		if strings.EqualFold(value, "true") {
			value = "false"
		} else {
			value = "true"
		}
	}

	return normCondition{Field: field, Op: op, Value: value, Negated: negated}
}

func normalizeEscapedQuotes(value string) string {
	for i := 0; i < 8; i++ {
		next := strings.ReplaceAll(value, `\"`, `"`)
		next = strings.ReplaceAll(next, `\'`, `'`)
		if next == value {
			return next
		}
		value = next
	}
	return value
}

func trimMatchingQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	first := value[0]
	last := value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func collapseTerminalEscapedBackslash(value string) string {
	for strings.HasSuffix(value, `\\`) {
		value = strings.TrimSuffix(value, `\`)
	}
	return value
}

func conditionKeys(conditions []normCondition) []string {
	keys := make([]string, 0, len(conditions))
	for _, cond := range dedupeNorm(conditions) {
		keys = append(keys, cond.key())
	}
	sort.Strings(keys)
	return keys
}

func (c normCondition) key() string {
	neg := ""
	if c.Negated {
		neg = "!"
	}
	return neg + c.Field + "|" + c.Op + "|" + c.Value
}

func dedupeNorm(conditions []normCondition) []normCondition {
	seen := make(map[string]bool, len(conditions))
	out := make([]normCondition, 0, len(conditions))
	for _, cond := range conditions {
		key := cond.key()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, cond)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

func missingConditions(expected, actual []normCondition) []normCondition {
	actualSet := make(map[string]bool, len(actual))
	for _, cond := range actual {
		actualSet[cond.key()] = true
	}
	var missing []normCondition
	for _, cond := range expected {
		if !actualSet[cond.key()] {
			missing = append(missing, cond)
		}
	}
	return dedupeNorm(missing)
}

func compareConditionSets(expected, actual []normCondition) ([]normCondition, []normCondition) {
	return missingConditions(expected, actual), missingConditions(actual, expected)
}

func inferLogsource(conditions []spl.Condition) sigmaLogsource {
	for _, cond := range conditions {
		if strings.EqualFold(cond.Field, "sourcetype") {
			value := strings.ToLower(cond.Value)
			if strings.Contains(value, "sysmon") {
				return sigmaLogsource{Product: "windows", Service: "sysmon"}
			}
			if strings.Contains(value, "wineventlog") || strings.Contains(value, "security") {
				return sigmaLogsource{Product: "windows"}
			}
		}
	}
	return sigmaLogsource{Category: "process_creation", Product: "windows"}
}

func sigmaModifierFromLike(pattern string) (string, string) {
	pattern = strings.ReplaceAll(pattern, "%", "*")
	start := strings.HasPrefix(pattern, "*")
	end := strings.HasSuffix(pattern, "*")
	stripped := strings.Trim(pattern, "*")
	switch {
	case start && end:
		return "|contains", stripped
	case start:
		return "|endswith", stripped
	case end:
		return "|startswith", stripped
	default:
		return "|re", wildcardToRegex(pattern)
	}
}

func wildcardToRegex(pattern string) string {
	quoted := regexp.QuoteMeta(pattern)
	quoted = strings.ReplaceAll(quoted, `\*`, ".*")
	quoted = strings.ReplaceAll(quoted, `\?`, ".")
	return "^" + quoted + "$"
}

func conditionValues(value string, alternatives []string) []string {
	if len(alternatives) > 0 {
		out := append([]string(nil), alternatives...)
		sort.Strings(out)
		return out
	}
	return []string{value}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func joinValues(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, formatSPLValue(value))
	}
	return strings.Join(out, ", ")
}

var numericValue = regexp.MustCompile(`^-?([0-9]+(\.[0-9]+)?|\.[0-9]+)$`)

func formatSPLValue(value string) string {
	if numericValue.MatchString(value) {
		return value
	}
	return quoteSPLString(value)
}

func quoteSPLString(value string) string {
	if strings.Contains(value, `"`) && !strings.Contains(value, `'`) {
		return "'" + value + "'"
	}
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for i := 0; i < len(value); i++ {
		if value[i] == '"' && !isEscaped(value, i) {
			b.WriteByte('\\')
		}
		b.WriteByte(value[i])
	}
	if len(value) > 0 && value[len(value)-1] == '\\' && !isEscaped(value, len(value)-1) {
		b.WriteByte('\\')
	}
	b.WriteByte('"')
	return b.String()
}

func isEscaped(value string, idx int) bool {
	backslashes := 0
	for i := idx - 1; i >= 0 && value[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func spaceEquals(query, replacement string) string {
	var b strings.Builder
	for i := 0; i < len(query); i++ {
		if query[i] != '=' {
			b.WriteByte(query[i])
			continue
		}
		prev := byte(0)
		next := byte(0)
		if i > 0 {
			prev = query[i-1]
		}
		if i+1 < len(query) {
			next = query[i+1]
		}
		if prev == '!' || prev == '<' || prev == '>' || next == '=' {
			b.WriteByte(query[i])
			continue
		}
		b.WriteString(replacement)
	}
	return b.String()
}

func uniqueValues(rng *rand.Rand, pool []string, n int) []string {
	if n > len(pool) {
		n = len(pool)
	}
	perm := rng.Perm(len(pool))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, pool[perm[i]])
	}
	sort.Strings(out)
	return out
}

func oneOf[T any](rng *rand.Rand, values ...T) T {
	return values[rng.Intn(len(values))]
}

func oneOfInt(rng *rand.Rand, values ...int) int {
	return values[rng.Intn(len(values))]
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func isNumericFieldName(field string) bool {
	_, err := strconv.Atoi(field)
	return err == nil
}

func sanitizeField(field string) string {
	replacer := strings.NewReplacer(".", "_", "-", "_", ":", "_")
	return replacer.Replace(field)
}

func newCorpusWriter(path string) (*corpusWriter, error) {
	if path == "" {
		return &corpusWriter{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if _, err := file.WriteString("[\n"); err != nil {
		file.Close()
		return nil, err
	}
	return &corpusWriter{file: file, enc: json.NewEncoder(file), first: true}, nil
}

func (w *corpusWriter) write(entry queryEntry) error {
	if w.file == nil {
		return nil
	}
	if !w.first {
		if _, err := w.file.WriteString(",\n"); err != nil {
			return err
		}
	}
	w.first = false
	return w.enc.Encode(entry)
}

func (w *corpusWriter) close() error {
	if w.file == nil {
		return nil
	}
	if _, err := w.file.WriteString("]\n"); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}

func newFailureWriter(path string) (*failureWriter, error) {
	if path == "" {
		return &failureWriter{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &failureWriter{file: file, enc: json.NewEncoder(file)}, nil
}

func (w *failureWriter) write(result runResult) error {
	if w.file == nil {
		return nil
	}
	return w.enc.Encode(result)
}

func (w *failureWriter) close() error {
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func countFailureStage(s *summary, stage string) {
	switch stage {
	case "spl_parse":
		s.SPLParseErrors++
	case "expected_mismatch":
		s.ExpectedMismatch++
	case "sigma_parse":
		s.SigmaParseErrors++
	case "sigma_mismatch":
		s.SigmaMismatch++
	case "back_spl_parse":
		s.BackSPLParseError++
	case "back_spl_mismatch":
		s.BackSPLMismatch++
	}
}

func printProgress(s summary, total int64, start time.Time) {
	elapsed := time.Since(start)
	rate := float64(s.Total) / elapsed.Seconds()
	remaining := ""
	if rate > 0 && total > s.Total {
		remaining = fmt.Sprintf(", eta=%s", time.Duration(float64(total-s.Total)/rate)*time.Second)
	}
	duplicates := ""
	if s.DuplicateQueries > 0 {
		duplicates = fmt.Sprintf(" duplicates=%d", s.DuplicateQueries)
	}
	fmt.Fprintf(os.Stderr, "processed=%d/%d passed=%d failed=%d%s rate=%.0f/s%s\n", s.Total, total, s.Passed, s.Failed, duplicates, rate, remaining)
}

func printSummary(s summary, corpusPath, failPath string, failureRecords int) {
	fmt.Printf("generated=%d passed=%d failed=%d duplicates=%d duration=%s\n", s.Total, s.Passed, s.Failed, s.DuplicateQueries, s.Duration)
	fmt.Printf("assertions: parser_oracle=%d sigma=%d back_conversion=%d\n", s.ParserOracleCheck, s.SigmaCheck, s.BackCheck)
	fmt.Printf("conditions compared: expected=%d parser=%d sigma=%d back=%d\n", s.ExpectedConds, s.ParserConds, s.SigmaConds, s.BackConds)
	fmt.Printf("failures: spl_parse=%d expected_mismatch=%d sigma_parse=%d sigma_mismatch=%d back_spl_parse=%d back_spl_mismatch=%d\n",
		s.SPLParseErrors, s.ExpectedMismatch, s.SigmaParseErrors, s.SigmaMismatch, s.BackSPLParseError, s.BackSPLMismatch)
	if corpusPath != "" {
		fmt.Printf("verified corpus: %s\n", corpusPath)
	}
	if failPath != "" {
		fmt.Printf("failure records: %s (%d written)\n", failPath, failureRecords)
	}
}

func printInputSummary(s summary, inputPath, failPath string, failureRecords int) {
	fmt.Printf("input=%s total=%d passed=%d failed=%d duration=%s\n", inputPath, s.Total, s.Passed, s.Failed, s.Duration)
	fmt.Printf("assertions: sigma=%d back_conversion=%d\n", s.SigmaCheck, s.BackCheck)
	fmt.Printf("conditions compared: parser=%d sigma=%d back=%d\n", s.ParserConds, s.SigmaConds, s.BackConds)
	fmt.Printf("failures: spl_parse=%d sigma_parse=%d sigma_mismatch=%d back_spl_parse=%d back_spl_mismatch=%d\n",
		s.SPLParseErrors, s.SigmaParseErrors, s.SigmaMismatch, s.BackSPLParseError, s.BackSPLMismatch)
	if failPath != "" {
		fmt.Printf("failure records: %s (%d written)\n", failPath, failureRecords)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
