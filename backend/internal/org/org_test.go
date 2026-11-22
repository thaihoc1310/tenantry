package org

import (
	"strings"
	"testing"
)

func TestCheckSlug(t *testing.T) {
	cases := map[string]bool{ // slug -> valid
		"acme": true, "a1b": true, "myteam2": true, strings.Repeat("a", 20): true,
		"kubeflow": true,  // only kube itself is reserved
		"my-team":  false, // no hyphen: <org>-<ws> must name one Workspace of one organization
		"ab":       false, strings.Repeat("a", 21): false,
		"1abc": false, "Acme": false, "my_team": false, "my--team": false, "team-": false, "-team": false,
		"admin": false, "default": false, "kube": false, "kube-x": false, "tenantry": false,
	}
	for slug, valid := range cases {
		if err := checkSlug(slug); (err == nil) != valid {
			t.Errorf("checkSlug(%q) = %v, want valid=%v", slug, err, valid)
		}
	}
}

func TestClean(t *testing.T) {
	if v, err := clean("displayName", "  Ác Mê  ", 5, true); err != nil || v != "Ác Mê" {
		t.Fatalf("got %q, %v", v, err)
	}
	if _, err := clean("displayName", "   ", 5, true); err == nil {
		t.Fatal("blank required value accepted")
	}
	if v, err := clean("description", "  ", 5, false); err != nil || v != "" {
		t.Fatalf("blank optional value: got %q, %v", v, err)
	}
	if _, err := clean("displayName", "ẵẵẵẵẵẵ", 5, true); err == nil { // counted in characters, not bytes
		t.Fatal("6 characters accepted with max 5")
	}
	if _, err := clean("displayName", "ẵẵẵẵẵ", 5, true); err != nil {
		t.Fatalf("5 multi-byte characters rejected: %v", err)
	}
}
