package id

import (
	"regexp"
	"testing"
)

func TestNew(t *testing.T) {
	shape := regexp.MustCompile(`^org-[0-9a-hjkmnp-tv-z]{26}$`) // lowercase Crockford base32
	prev := ""
	for range 1000 {
		got := New(Organization)
		if !shape.MatchString(got) {
			t.Fatalf("%q does not look like org-<ulid>", got)
		}
		if got <= prev {
			t.Fatalf("%q sorts before the id made just before it (%q)", got, prev)
		}
		prev = got
	}
}
