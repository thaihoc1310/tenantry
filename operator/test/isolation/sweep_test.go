//go:build lab

package isolation

import (
	"net/http"
	"strings"
	"testing"
)

// TestSweepOptestAccounts disables every Active optest-* account a run that
// died early left behind (go test -tags lab -run TestSweepOptestAccounts).
func TestSweepOptestAccounts(t *testing.T) {
	adm := adminPortal(t)
	var users struct {
		Items []struct {
			ID, Status string
			Email      *string
		}
	}
	adm.api(t, http.MethodGet, "/admin/users?q=optest-&limit=200", nil, &users)
	for _, u := range users.Items {
		if u.Email == nil || !strings.HasPrefix(*u.Email, "optest-") || u.Status != "Active" {
			continue
		}
		if code := adm.api(t, http.MethodPost, "/admin/users/"+u.ID+":disable", nil, nil); code != http.StatusOK {
			t.Errorf("disable %s: %d", *u.Email, code)
			continue
		}
		t.Logf("disabled %s", *u.Email)
	}
}

// TestListOptestOrganizations shows the optest Organizations, any status, and
// what keeps them, read-only (go test -tags lab -run TestListOptestOrganizations).
func TestListOptestOrganizations(t *testing.T) {
	adm := adminPortal(t)
	cursor := ""
	for {
		var page struct {
			Items      []struct{ ID, Slug, Status string }
			NextCursor string `json:"nextCursor"`
		}
		adm.api(t, http.MethodGet, "/admin/organizations?limit=50&cursor="+cursor, nil, &page)
		for _, o := range page.Items {
			if !strings.HasPrefix(o.Slug, "optest") {
				continue
			}
			var detail map[string]any
			adm.api(t, http.MethodGet, "/admin/organizations/"+o.ID, nil, &detail)
			t.Logf("%s %s %s usage=%v dedicatedNodes=%v", o.Slug, o.ID, o.Status, detail["usage"], detail["dedicatedNodes"])
		}
		if page.NextCursor == "" {
			return
		}
		cursor = page.NextCursor
	}
}
