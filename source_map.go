package spl

import "unicode/utf8"

type sourceRuneSpan struct{ start, end int }

// splSourceMap follows each normalization edit back to the authored bytes.
// Token offsets are runes, while Go string slices use bytes.
type splSourceMap struct {
	original string
	spans    []sourceRuneSpan
}

func normalizeSPLQueryMapped(query string, budget *parseBudget) (string, splSourceMap) {
	projection := splSourceMap{original: query}
	for byteIndex := range query {
		budget.step()
		_, width := utf8.DecodeRuneInString(query[byteIndex:])
		projection.spans = append(projection.spans, sourceRuneSpan{byteIndex, byteIndex + width})
	}
	stages := []struct {
		canonical func(string) string
		mapper    func(string, []sourceRuneSpan, *parseBudget) (string, []sourceRuneSpan)
	}{
		{normalizeSPLUnicodeWhitespace, mapSPLUnicodeWhitespace},
		{stripSPLLineContinuations, mapSPLLineContinuations},
		{stripSPLBacktickComments, mapSPLBacktickComments},
		{normalizeSPLDoubleEquals, mapSPLDoubleEquals},
		{normalizeSPLBangFunctions, mapSPLBangFunctions},
		{normalizeSPLAdjacentWildcardPredicates, mapSPLAdjacentWildcardPredicates},
		{normalizeSPLInputlookupKeywordOptions, mapSPLInputlookupOptions},
	}
	current := query
	for _, stage := range stages {
		budget.step()
		current, projection.spans = mapSPLSourceWithCanonical(current, projection.spans, stage.canonical, stage.mapper, budget)
	}
	return current, projection
}

func mapSPLBacktickComments(query string, spans []sourceRuneSpan, budget *parseBudget) (string, []sourceRuneSpan) {
	runes := []rune(query)
	out := make([]rune, 0, len(runes))
	mapped := make([]sourceRuneSpan, 0, len(runes))
	inString := false
	var quote rune
	appendRune := func(index int) { out = append(out, runes[index]); mapped = append(mapped, spans[index]) }
	for i := 0; i < len(runes); {
		budget.step()
		if !inString && i+2 < len(runes) && runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			start := i
			i += 3
			closed := false
			for i+2 < len(runes) {
				budget.step()
				if runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
					i += 3
					closed = true
					break
				}
				i++
			}
			if !closed {
				for i < len(runes) && runes[i] != '\n' && runes[i] != '\r' {
					budget.step()
					i++
				}
			}
			out = append(out, ' ')
			mapped = append(mapped, sourceRuneSpan{spans[start].start, spans[i-1].end})
			continue
		}
		if !inString && (runes[i] == '"' || runes[i] == '\'') {
			inString, quote = true, runes[i]
		} else if inString && runes[i] == quote && !escapedSPLRune(runes, i) {
			inString = false
		}
		appendRune(i)
		i++
	}
	return string(out), mapped
}

func escapedSPLRune(runes []rune, index int) bool {
	count := 0
	for i := index - 1; i >= 0 && runes[i] == '\\'; i-- {
		count++
	}
	return count%2 == 1
}

func (m splSourceMap) source(start, stop int) string {
	if start < 0 || stop < start || stop >= len(m.spans) {
		return ""
	}
	from, through := m.spans[start].start, m.spans[stop].end
	if from < 0 || through < from || through > len(m.original) {
		return ""
	}
	return m.original[from:through]
}
