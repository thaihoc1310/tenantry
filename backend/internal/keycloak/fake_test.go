package keycloak

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
)

// fakeKeycloak is the slice of Keycloak's Admin API the worker uses, kept in
// memory and answering the way Keycloak 26 does.
type fakeKeycloak struct {
	srv *httptest.Server

	mu      sync.Mutex
	token   string                     // the access token currently valid
	groups  map[string]string          // id -> name
	users   map[string]bool            // Keycloak user ids
	members map[string]map[string]bool // group id -> user ids
	calls   map[string]int             // "METHOD /path pattern" -> count
	tokens  int                        // token requests
	nextID  int

	// before may answer a request instead of the fake (non-zero status);
	// after runs once the fake handled it. Both run outside the lock.
	before func(r *http.Request) int
	after  func(r *http.Request)
}

const (
	fakeRealm  = "tenantry"
	fakeClient = "tenantry-sync"
	fakeSecret = "s3cret"
)

func newFakeKeycloak(t *testing.T, users ...string) *fakeKeycloak {
	f := &fakeKeycloak{
		token: "token-1", groups: map[string]string{}, users: map[string]bool{},
		members: map[string]map[string]bool{}, calls: map[string]int{},
	}
	for _, u := range users {
		f.users[u] = true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /realms/tenantry/protocol/openid-connect/token", f.issueToken)
	admin := map[string]http.HandlerFunc{
		"GET /admin/realms/tenantry/groups":                         f.findGroups,
		"POST /admin/realms/tenantry/groups":                        f.createGroup,
		"DELETE /admin/realms/tenantry/groups/{id}":                 f.deleteGroup,
		"PUT /admin/realms/tenantry/users/{user}/groups/{group}":    f.addMember,
		"DELETE /admin/realms/tenantry/users/{user}/groups/{group}": f.removeMember,
	}
	for pattern, h := range admin {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.calls[pattern]++
			valid := r.Header.Get("Authorization") == "Bearer "+f.token
			before, after := f.before, f.after
			f.mu.Unlock()
			if !valid {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if before != nil {
				if status := before(r); status != 0 {
					http.Error(w, `{"error":"injected"}`, status)
					return
				}
			}
			f.mu.Lock()
			h(w, r)
			f.mu.Unlock()
			if after != nil {
				after(r)
			}
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeKeycloak) client() *Client {
	return NewClient(config.KeycloakSync{URL: f.srv.URL, Realm: fakeRealm, ClientID: fakeClient, ClientSecret: fakeSecret})
}

func (f *fakeKeycloak) issueToken(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, secret, ok := r.BasicAuth()
	if !ok || id != fakeClient || secret != fakeSecret || r.FormValue("grant_type") != "client_credentials" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.tokens++
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.token, "expires_in": 300})
}

func (f *fakeKeycloak) findGroups(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	out := []map[string]any{}
	for id, name := range f.groups {
		if name == search { // exact=true
			out = append(out, map[string]any{"id": id, "name": name, "path": "/" + name})
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeKeycloak) createGroup(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, name := range f.groups {
		if name == body.Name {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}
	f.nextID++
	id := fmt.Sprintf("kc-group-%d", f.nextID)
	f.groups[id] = body.Name
	w.Header().Set("Location", f.srv.URL+"/admin/realms/tenantry/groups/"+id)
	w.WriteHeader(http.StatusCreated)
}

func (f *fakeKeycloak) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := f.groups[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	delete(f.groups, id)
	delete(f.members, id)
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeKeycloak) memberPath(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	user, group := r.PathValue("user"), r.PathValue("group")
	_, groupOK := f.groups[group]
	if !f.users[user] || !groupOK {
		w.WriteHeader(http.StatusNotFound)
		return "", "", false
	}
	return user, group, true
}

func (f *fakeKeycloak) addMember(w http.ResponseWriter, r *http.Request) {
	if user, group, ok := f.memberPath(w, r); ok {
		if f.members[group] == nil {
			f.members[group] = map[string]bool{}
		}
		f.members[group][user] = true
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeKeycloak) removeMember(w http.ResponseWriter, r *http.Request) {
	if user, group, ok := f.memberPath(w, r); ok {
		delete(f.members[group], user)
		w.WriteHeader(http.StatusNoContent)
	}
}

// groupNamed returns the id of the group called name, or "".
func (f *fakeKeycloak) groupNamed(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := ""
	for gid, n := range f.groups {
		if n == name {
			if id != "" {
				panic("two Keycloak groups named " + name)
			}
			id = gid
		}
	}
	return id
}

func (f *fakeKeycloak) isMember(user, groupName string) bool {
	id := f.groupNamed(groupName)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.members[id][user]
}

func (f *fakeKeycloak) count(pattern string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[pattern]
}

func (f *fakeKeycloak) set(fn func(f *fakeKeycloak)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeKeycloak) tokenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens
}
