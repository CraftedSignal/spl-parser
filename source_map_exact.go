package spl

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func mapSPLUnicodeWhitespace(query string, spans []sourceRuneSpan, budget *parseBudget) (string, []sourceRuneSpan) {
	runes := []rune(query)
	out, mapped := make([]rune, 0, len(runes)), make([]sourceRuneSpan, 0, len(runes))
	inString := false
	var quote rune
	for i := 0; i < len(runes); i++ {
		budget.step()
		value := runes[i]
		if !inString && value == '`' {
			if end := splMacroRuneEnd(runes, i); end > i {
				for j := i; j < end; j++ {
					budget.step()
					out, mapped = append(out, runes[j]), append(mapped, spans[j])
				}
				i = end - 1
				continue
			}
		}
		if !inString && (value == '"' || value == '\'') {
			inString, quote = true, value
		} else if inString && value == quote && !escapedSPLRune(runes, i) {
			inString = false
		}
		if !inString && value >= utf8.RuneSelf {
			if value == 0x200b || value == 0x200c || value == 0x200d || value == 0xfeff {
				continue
			}
			if unicode.IsSpace(value) {
				value = ' '
			}
		}
		out, mapped = append(out, value), append(mapped, spans[i])
	}
	return string(out), mapped
}

func mapSPLLineContinuations(query string, spans []sourceRuneSpan, budget *parseBudget) (string, []sourceRuneSpan) {
	runes := []rune(query)
	out, mapped := make([]rune, 0, len(runes)), make([]sourceRuneSpan, 0, len(runes))
	inString := false
	var quote rune
	for i := 0; i < len(runes); i++ {
		budget.step()
		value := runes[i]
		if !inString && value == '`' {
			if end := splMacroRuneEnd(runes, i); end > i {
				for j := i; j < end; j++ {
					budget.step()
					out, mapped = append(out, runes[j]), append(mapped, spans[j])
				}
				i = end - 1
				continue
			}
		}
		if !inString && (value == '"' || value == '\'') {
			inString, quote = true, value
		} else if inString && value == quote && !escapedSPLRune(runes, i) {
			inString = false
		}
		if !inString && value == '\\' {
			j := i + 1
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
				budget.step()
				j++
			}
			if j < len(runes) && (runes[j] == '\n' || runes[j] == '\r') {
				i = j - 1
				continue
			}
		}
		out, mapped = append(out, value), append(mapped, spans[i])
	}
	return string(out), mapped
}

func mapSPLDoubleEquals(query string, spans []sourceRuneSpan, budget *parseBudget) (string, []sourceRuneSpan) {
	runes := []rune(query)
	out, mapped := make([]rune, 0, len(runes)), make([]sourceRuneSpan, 0, len(runes))
	inString := false
	var quote rune
	for i := 0; i < len(runes); i++ {
		budget.step()
		value := runes[i]
		if !inString && value == '`' {
			if end := splMacroRuneEnd(runes, i); end > i {
				for j := i; j < end; j++ {
					budget.step()
					out, mapped = append(out, runes[j]), append(mapped, spans[j])
				}
				i = end - 1
				continue
			}
		}
		if !inString && (value == '"' || value == '\'') {
			inString, quote = true, value
		} else if inString && value == quote && !escapedSPLRune(runes, i) {
			inString = false
		}
		operatorSpan := spans[i]
		if !inString && value == '=' && i+1 < len(runes) && runes[i+1] == '=' {
			operatorSpan.end = spans[i+1].end
			i++
		}
		out, mapped = append(out, value), append(mapped, operatorSpan)
	}
	return string(out), mapped
}

func mapSPLBangFunctions(query string, spans []sourceRuneSpan, budget *parseBudget) (string, []sourceRuneSpan) {
	runes := []rune(query)
	out, mapped := make([]rune, 0, len(runes)), make([]sourceRuneSpan, 0, len(runes))
	inString := false
	var quote rune
	for i := 0; i < len(runes); i++ {
		budget.step()
		value := runes[i]
		if !inString && value == '`' {
			if end := splMacroRuneEnd(runes, i); end > i {
				for j := i; j < end; j++ {
					budget.step()
					out, mapped = append(out, runes[j]), append(mapped, spans[j])
				}
				i = end - 1
				continue
			}
		}
		if !inString && (value == '"' || value == '\'') {
			inString, quote = true, value
		} else if inString && value == quote && !escapedSPLRune(runes, i) {
			inString = false
		}
		if !inString && value == '!' {
			for _, name := range []string{"isnull(", "isnotnull(", "match(", "like(", "cidrmatch("} {
				candidate := []rune(name)
				if i+1+len(candidate) > len(runes) || string(runes[i+1:i+1+len(candidate)]) != name {
					continue
				}
				for _, prefix := range "NOT " {
					out, mapped = append(out, prefix), append(mapped, spans[i])
				}
				for j, letter := range candidate {
					out, mapped = append(out, letter), append(mapped, spans[i+1+j])
				}
				i += len(candidate)
				value = 0
				break
			}
			if value == 0 {
				continue
			}
		}
		out, mapped = append(out, value), append(mapped, spans[i])
	}
	return string(out), mapped
}

func mapSPLInputlookupOptions(query string, spans []sourceRuneSpan, budget *parseBudget) (string, []sourceRuneSpan) {
	byteRune := make([]int, len(query)+1)
	for byteIndex, runeIndex := 0, 0; byteIndex < len(query); runeIndex++ {
		budget.step()
		_, width := utf8.DecodeRuneInString(query[byteIndex:])
		for j := byteIndex; j < byteIndex+width; j++ {
			byteRune[j] = runeIndex
		}
		byteIndex += width
	}
	var out strings.Builder
	out.Grow(len(query))
	mapped := make([]sourceRuneSpan, 0, len(spans))
	appendRange := func(start, end int) {
		for i := start; i < end; {
			budget.step()
			value, width := utf8.DecodeRuneInString(query[i:])
			out.WriteRune(value)
			mapped = append(mapped, spans[byteRune[i]])
			i += width
		}
	}
	flushOutside := func(start, end int) {
		segment := query[start:end]
		cursor := start
		for _, match := range inputlookupAppendOptionPattern.FindAllStringSubmatchIndex(segment, -1) {
			budget.step()
			matchEnd := start + match[1]
			groupOneEnd, groupTwoStart := start+match[3], start+match[4]
			appendRange(cursor, groupOneEnd)
			originalStart := groupOneEnd
			for i, letter := range "append_option" {
				out.WriteRune(letter)
				origin := originalStart + i
				if i >= len("append") {
					origin = originalStart + len("append") - 1
				}
				mapped = append(mapped, spans[byteRune[origin]])
			}
			appendRange(groupTwoStart, matchEnd)
			cursor = matchEnd
		}
		appendRange(cursor, end)
	}
	inString := false
	var quote byte
	segmentStart := 0
	for i := 0; i < len(query); i++ {
		budget.step()
		value := query[i]
		if !inString && value == '`' {
			if end := splMacroEnd(query, i); end > i {
				flushOutside(segmentStart, i)
				appendRange(i, end)
				i = end - 1
				segmentStart = end
				continue
			}
		}
		if !inString && (value == '"' || value == '\'') {
			flushOutside(segmentStart, i)
			inString, quote, segmentStart = true, value, i
		} else if inString && value == quote && !isEscapedByte(query, i) {
			appendRange(segmentStart, i+1)
			inString, segmentStart = false, i+1
		}
	}
	if inString {
		appendRange(segmentStart, len(query))
	} else {
		flushOutside(segmentStart, len(query))
	}
	return out.String(), mapped
}

func splMacroRuneEnd(runes []rune, start int) int {
	if start < 0 || start >= len(runes) || runes[start] != '`' || start+2 < len(runes) && runes[start+1] == '`' && runes[start+2] == '`' {
		return start
	}
	for i := start + 2; i < len(runes); i++ {
		if runes[i] == '`' {
			return i + 1
		}
	}
	return start
}

func mapSPLSourceWithCanonical(query string, spans []sourceRuneSpan, transform func(string) string,
	mapper func(string, []sourceRuneSpan, *parseBudget) (string, []sourceRuneSpan), budget *parseBudget) (string, []sourceRuneSpan) {
	next, mapped := mapper(query, spans, budget)
	if next != transform(query) || len([]rune(next)) != len(mapped) {
		budget.reject("source provenance")
	}
	return next, mapped
}

// The grammar's wildcardValue can absorb a following assignment after skipped
// whitespace (user=* action=* becomes one value). Insert an explicit search
// conjunction only for the adjacent wildcard-assignment shape.
func normalizeSPLAdjacentWildcardPredicates(query string) string {
	result, _ := mapSPLAdjacentWildcardPredicates(query, nil, nil)
	return result
}

func mapSPLAdjacentWildcardPredicates(query string, spans []sourceRuneSpan, budget *parseBudget) (string, []sourceRuneSpan) {
	runes := []rune(query)
	out := make([]rune, 0, len(runes))
	var mapped []sourceRuneSpan
	if spans != nil {
		mapped = make([]sourceRuneSpan, 0, len(spans))
	}
	appendRune := func(value rune, origin int) {
		out = append(out, value)
		if spans != nil {
			mapped = append(mapped, spans[origin])
		}
	}
	inString := false
	var quote rune
	for i := 0; i < len(runes); i++ {
		if budget != nil {
			budget.step()
		}
		value := runes[i]
		if !inString && value == '`' {
			if end := splMacroRuneEnd(runes, i); end > i {
				for j := i; j < end; j++ {
					if budget != nil {
						budget.step()
					}
					appendRune(runes[j], j)
				}
				i = end - 1
				continue
			}
		}
		if !inString && (value == '"' || value == '\'') {
			inString, quote = true, value
		} else if inString && value == quote && !escapedSPLRune(runes, i) {
			inString = false
		}
		appendRune(value, i)
		if inString || value != '*' || i == 0 || runes[i-1] != '=' {
			continue
		}
		gap := i + 1
		for gap < len(runes) && (runes[gap] == ' ' || runes[gap] == '\t') {
			gap++
		}
		if gap == i+1 || gap >= len(runes) || !isSPLAdjacentFieldStart(runes[gap]) {
			continue
		}
		fieldEnd := gap + 1
		for fieldEnd < len(runes) && isSPLAdjacentFieldPart(runes[fieldEnd]) {
			fieldEnd++
		}
		if fieldEnd+1 >= len(runes) || runes[fieldEnd] != '=' || runes[fieldEnd+1] != '*' {
			continue
		}
		for _, letter := range " AND " {
			appendRune(letter, i+1)
		}
		i = gap - 1
	}
	return string(out), mapped
}

func isSPLAdjacentFieldStart(value rune) bool {
	return value == '_' || unicode.IsLetter(value)
}

func isSPLAdjacentFieldPart(value rune) bool {
	return isSPLAdjacentFieldStart(value) || unicode.IsDigit(value) || value == '.' || value == ':'
}
