package spl

import "testing"

// convert takes several functions separated by spaces, as the Search
// Reference writes them (convert dur2sec(xdelay) dur2sec(delay)); a comma
// between them parses as before. auto(*) and none(<field>) name every
// field and the ones to leave alone.
func TestConvertWithSeveralFunctionsParses(t *testing.T) {
	assertNativeParse(t, `index=x | convert dur2sec(xdelay) dur2sec(delay) | where marker=1`, "search", "convert", "where")
	assertNativeParse(t, `index=x | convert ctime(first_seen) ctime(last_seen) | where marker=1`, "search", "convert", "where")
	assertNativeParse(t, `index=x | convert timeformat="%H:%M:%S" ctime(a) AS b ctime(c) AS d | where marker=1`, "search", "convert", "where")
	assertNativeParse(t, `index=x | convert ctime(a) AS b, ctime(c) | where marker=1`, "search", "convert", "where")
	assertNativeParse(t, `index=x | convert auto(*) none(src_ip) | where marker=1`, "search", "convert", "where")
}
