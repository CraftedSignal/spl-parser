package spl

import (
	"strings"
	"testing"
)

func TestSPLNormalizationRemainingEdges(t *testing.T) {
	comment := stripSPLBacktickComments("search a=1 ```unterminated comment\n| where b=2")
	if strings.Contains(comment, "unterminated comment") {
		t.Fatalf("unterminated triple-backtick comment was preserved: %q", comment)
	}
	if !strings.Contains(comment, "\n| where b=2") {
		t.Fatalf("unterminated comment stripped following pipeline: %q", comment)
	}

	invalidUTF8 := string([]byte{'a', 0xff, 'b'})
	if got := normalizeSPLUnicodeWhitespace(invalidUTF8); got != invalidUTF8 {
		t.Fatalf("invalid utf-8 bytes changed: %q", got)
	}

	unicodeSpace := normalizeSPLUnicodeWhitespace("index=main\u00a0user=\"a\u00a0b\"\u200bhost=server")
	if unicodeSpace != "index=main user=\"a\u00a0b\"host=server" {
		t.Fatalf("unicode whitespace normalized incorrectly: %q", unicodeSpace)
	}

	segment, ok := portablePredicateSegment(`search user="alice"`, false)
	if !ok || segment != `user="alice"` {
		t.Fatalf("search segment = (%q, %v), want predicate", segment, ok)
	}
}
