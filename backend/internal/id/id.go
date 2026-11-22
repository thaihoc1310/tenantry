// Package id makes resource ids: a kebab prefix plus a lowercase ULID, e.g.
// org-01jd3m6k4x8c9vq2b7t5n0w1yz (IMPLEMENTATION-PLAN §12, decision 3).
// ULIDs sort by creation time, which keyset pagination relies on.
package id

import (
	"strings"

	"github.com/oklog/ulid/v2"
)

const (
	Organization    = "org"
	Operation       = "op"
	User            = "usr"
	Group           = "grp"
	Invitation      = "inv"
	Transfer        = "transfer"
	ClusterGrant    = "grant"
	NodeGrant       = "nodegrant"
	Workspace       = "ws"
	Namespace       = "ns"
	NodeReservation = "noderes"
)

// ULID returns the ULID part of an id made by New.
func ULID(id string) string {
	_, u, _ := strings.Cut(id, "-")
	return u
}

// New returns a fresh id with the given prefix.
func New(prefix string) string {
	return prefix + "-" + strings.ToLower(ulid.Make().String())
}
