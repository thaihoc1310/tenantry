// Package keycloak mirrors organization groups to Keycloak (IMPLEMENTATION-PLAN
// §5.2): a thin client for the few Admin API calls needed, and the group sync
// worker built on it. It authenticates as the service account of the client
// tenantry-sync: query-users and query-groups of realm-management, the rest
// through fine-grained admin permissions (deploy/cluster/keycloak-fgap.py,
// architecture/06 §7). It cannot grant roles, impersonate, or touch Platform
// Admins, clients or the realm; it can take over a plain user's account
// (Users manage, which enabling accounts needs). Only the group sync
// Deployment holds its secret.
//
// It also turns accounts on and off when a Platform Admin disables or
// enables a user (M5).
//
// Every call is idempotent: creating a group that exists, adding a member
// who is in, removing one who is not, deleting a deleted group all succeed,
// so a worker that dies half-way just runs again.
package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
)

// ErrUserNotFound is a Keycloak user that does not exist (any more).
var ErrUserNotFound = errors.New("keycloak user not found")

type Client struct {
	cfg  config.KeycloakSync
	http *http.Client
	now  func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

func NewClient(cfg config.KeycloakSync) *Client {
	cfg.URL = strings.TrimSuffix(cfg.URL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}, now: time.Now}
}

// accessToken returns a service-account token, fetched again shortly before it expires.
func (c *Client) accessToken(ctx context.Context, fresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !fresh && c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.URL+"/realms/"+url.PathEscape(c.cfg.Realm)+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(c.cfg.ClientID), url.QueryEscape(c.cfg.ClientSecret))
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("keycloak token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("keycloak token: %s", status(resp))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("keycloak token: %w", err)
	}
	c.token = tok.AccessToken
	// Renew 30 seconds early, so a token never expires in the middle of a call.
	c.expires = c.now().Add(time.Duration(tok.ExpiresIn)*time.Second - 30*time.Second)
	return c.token, nil
}

// admin calls the Admin API of the realm; a 401 refreshes the token once.
func (c *Client) admin(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}
	for attempt := 0; ; attempt++ {
		token, err := c.accessToken(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.cfg.URL+"/admin/realms/"+url.PathEscape(c.cfg.Realm)+path, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("keycloak %s %s: %w", method, path, err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_ = resp.Body.Close()
			continue
		}
		return resp, nil
	}
}

// FindGroup returns the id of the top-level group with exactly this name.
func (c *Client) FindGroup(ctx context.Context, name string) (id string, found bool, err error) {
	q := url.Values{"search": {name}, "exact": {"true"}, "briefRepresentation": {"true"}, "max": {"10"}}
	resp, err := c.admin(ctx, http.MethodGet, "/groups?"+q.Encode(), nil)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("keycloak find group %q: %s", name, status(resp))
	}
	var groups []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return "", false, err
	}
	for _, g := range groups {
		if g.Name == name {
			return g.ID, true, nil
		}
	}
	return "", false, nil
}

// EnsureGroup returns the id of the top-level group named name, creating it if needed.
func (c *Client) EnsureGroup(ctx context.Context, name string) (string, error) {
	if id, found, err := c.FindGroup(ctx, name); err != nil || found {
		return id, err
	}
	resp, err := c.admin(ctx, http.MethodPost, "/groups", map[string]string{"name": name})
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusCreated:
		loc := resp.Header.Get("Location")
		if i := strings.LastIndex(loc, "/"); i >= 0 && i < len(loc)-1 {
			return loc[i+1:], nil
		}
		return "", fmt.Errorf("keycloak create group %q: no id in Location %q", name, loc)
	case http.StatusConflict: // created meanwhile by someone else
		id, found, err := c.FindGroup(ctx, name)
		if err == nil && !found {
			err = fmt.Errorf("keycloak create group %q: conflict, yet not found", name)
		}
		return id, err
	}
	return "", fmt.Errorf("keycloak create group %q: %s", name, status(resp))
}

// DeleteGroup deletes a group with its memberships; a missing group is deleted already.
func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	return c.expect(ctx, http.MethodDelete, "/groups/"+url.PathEscape(id), "delete group", http.StatusNotFound)
}

// AddMember puts the user in the group.
func (c *Client) AddMember(ctx context.Context, userID, groupID string) error {
	resp, err := c.admin(ctx, http.MethodPut, "/users/"+url.PathEscape(userID)+"/groups/"+url.PathEscape(groupID), nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("keycloak add %s to group %s: %w (or the group is gone)", userID, groupID, ErrUserNotFound)
	}
	return fmt.Errorf("keycloak add %s to group %s: %s", userID, groupID, status(resp))
}

// RemoveMember takes the user out of the group; a user or group that is gone
// is out already.
func (c *Client) RemoveMember(ctx context.Context, userID, groupID string) error {
	return c.expect(ctx, http.MethodDelete, "/users/"+url.PathEscape(userID)+"/groups/"+url.PathEscape(groupID),
		"remove "+userID+" from group "+groupID, http.StatusNotFound)
}

// SetUserEnabled turns the user's account on or off. Disabling a user who is
// gone succeeds; enabling one is ErrUserNotFound.
func (c *Client) SetUserEnabled(ctx context.Context, userID string, enabled bool) error {
	resp, err := c.admin(ctx, http.MethodPut, "/users/"+url.PathEscape(userID), map[string]bool{"enabled": enabled})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300, resp.StatusCode == http.StatusNotFound && !enabled:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("keycloak enable %s: %w", userID, ErrUserNotFound)
	}
	return fmt.Errorf("keycloak set enabled=%t on %s: %s", enabled, userID, status(resp))
}

// expect makes a call whose success is 2xx or one of alsoOK.
func (c *Client) expect(ctx context.Context, method, path, what string, alsoOK ...int) error {
	resp, err := c.admin(ctx, method, path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 || slices.Contains(alsoOK, resp.StatusCode) {
		return nil
	}
	return fmt.Errorf("keycloak %s: %s", what, status(resp))
}

// status is a response's status with the start of its body, for error messages.
func status(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return strings.TrimSpace(resp.Status + " " + string(b))
}
