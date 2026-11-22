package main

import (
	"strings"
	"testing"
)

func TestParseLoops(t *testing.T) {
	for in, want := range map[string]string{
		"all":                       "provisioning,housekeeping",
		"housekeeping":              "housekeeping",
		"provisioning,housekeeping": "provisioning,housekeeping",
		" housekeeping , provisioning ,housekeeping": "housekeeping,provisioning",
	} {
		got, err := parseLoops(in)
		if err != nil || strings.Join(got, ",") != want {
			t.Errorf("parseLoops(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"", " ", "groupsync", "housekeeping,", "all,housekeeping", "Housekeeping"} {
		if got, err := parseLoops(in); err == nil {
			t.Errorf("parseLoops(%q) = %v, want an error", in, got)
		}
	}
}

// A worker started without --loops stops before touching anything.
func TestLoopsRequired(t *testing.T) {
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "--loops is required") {
		t.Fatalf("run without --loops: %v", err)
	}
}
