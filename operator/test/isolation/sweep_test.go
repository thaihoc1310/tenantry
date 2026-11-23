//go:build lab

package isolation

import (
	"net/http"
	"strings"
	"testing"
	"time"
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

// TestSweepOptestOrganizations deletes, as the Platform Admin, every optest
// Organization still Active: what a run left when it was stopped before its
// cleanup. An Organization whose Workspaces are still in address quarantine
// is tried again until it can go, for up to 20 minutes.
func TestSweepOptestOrganizations(t *testing.T) {
	adm := adminPortal(t)
	var live []struct{ ID, Slug string }
	cursor := ""
	for {
		var page struct {
			Items      []struct{ ID, Slug, Status string }
			NextCursor string `json:"nextCursor"`
		}
		adm.api(t, http.MethodGet, "/admin/organizations?limit=50&cursor="+cursor, nil, &page)
		for _, o := range page.Items {
			if strings.HasPrefix(o.Slug, "optest") && o.Status == "Active" {
				live = append(live, struct{ ID, Slug string }{o.ID, o.Slug})
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	for _, o := range live {
		deleted := false
		for end := time.Now().Add(20 * time.Minute); time.Now().Before(end); time.Sleep(15 * time.Second) {
			if code := adm.api(t, http.MethodDelete, "/admin/organizations/"+o.ID, nil, nil); code == http.StatusNoContent || code == http.StatusNotFound {
				deleted = true
				break
			}
		}
		if !deleted {
			t.Errorf("Organization %s not deleted after 20 minutes", o.Slug)
			continue
		}
		t.Logf("deleted %s", o.Slug)
	}
}
