// Package config reads the environment shared by every backend binary, so a
// typo in a variable fails at start-up instead of silently using a default.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string // DATABASE_URL, required
	Addr        string // TENANTRY_ADDR, API listen address; default :8080
	// TENANTRY_METRICS_ADDR: where /metrics is served, apart from the API so
	// no Ingress reaches it; default :9090, "off" serves none.
	MetricsAddr string
	LogLevel    slog.Level // TENANTRY_LOG_LEVEL: debug, info (default), warn, error
	LogFormat   string     // TENANTRY_LOG_FORMAT: json (default) or text
	// TENANTRY_TRUSTED_PROXIES: comma-separated CIDRs or addresses of the
	// proxies in front of the API (Traefik), whose X-Forwarded-For names the
	// client. Empty: the header is never believed.
	TrustedProxies []netip.Prefix
}

// Load reads the configuration and reports every invalid variable at once.
func Load() (Config, error) {
	c := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Addr:        os.Getenv("TENANTRY_ADDR"),
		LogFormat:   os.Getenv("TENANTRY_LOG_FORMAT"),
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	switch c.MetricsAddr = os.Getenv("TENANTRY_METRICS_ADDR"); c.MetricsAddr {
	case "":
		c.MetricsAddr = ":9090"
	case "off":
		c.MetricsAddr = ""
	default:
		if _, _, err := net.SplitHostPort(c.MetricsAddr); err != nil {
			errs = append(errs, fmt.Errorf("TENANTRY_METRICS_ADDR=%q: want host:port such as :9090, or off", c.MetricsAddr))
		}
	}
	if v := os.Getenv("TENANTRY_LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("TENANTRY_LOG_LEVEL=%q: want debug, info, warn or error", v))
		}
	}
	for v := range strings.SplitSeq(os.Getenv("TENANTRY_TRUSTED_PROXIES"), ",") {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		p, err := netip.ParsePrefix(v)
		if err != nil {
			a, aerr := netip.ParseAddr(v)
			if aerr != nil {
				errs = append(errs, fmt.Errorf("TENANTRY_TRUSTED_PROXIES: %q is neither a CIDR nor an address", v))
				continue
			}
			p = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, p.Masked())
	}
	switch c.LogFormat {
	case "":
		c.LogFormat = "json"
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("TENANTRY_LOG_FORMAT=%q: want json or text", c.LogFormat))
	}
	return c, errors.Join(errs...)
}

// Logger builds the process logger: JSON for log collectors in the cluster,
// text for a terminal.
func (c Config) Logger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// Auth is how tenantry-api signs users in through Keycloak
// (architecture/06-security-and-identity.md §3). Only the API reads it.
type Auth struct {
	Issuer       string // TENANTRY_OIDC_ISSUER, e.g. https://auth.tenantry.site/realms/tenantry
	ClientID     string // TENANTRY_OIDC_CLIENT_ID; default portal
	ClientSecret string // TENANTRY_OIDC_CLIENT_SECRET, from a Secret
	// TENANTRY_PUBLIC_URL: the portal origin browsers use, e.g.
	// https://portal.tenantry.site; Keycloak redirects back to <it>/auth/callback.
	PublicURL string
	// TENANTRY_SESSION_KEY: 32 random bytes, base64. Encrypts refresh tokens
	// and the login cookie; changing it signs everyone out.
	SessionKey []byte
}

// LoadAuth reads Auth and reports every invalid variable at once.
func LoadAuth() (Auth, error) {
	a := Auth{
		Issuer:       os.Getenv("TENANTRY_OIDC_ISSUER"),
		ClientID:     os.Getenv("TENANTRY_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("TENANTRY_OIDC_CLIENT_SECRET"),
		PublicURL:    strings.TrimSuffix(os.Getenv("TENANTRY_PUBLIC_URL"), "/"),
	}
	var errs []error
	if err := checkOrigin(a.Issuer, true); err != nil {
		errs = append(errs, fmt.Errorf("TENANTRY_OIDC_ISSUER=%q: %w", a.Issuer, err))
	}
	if a.ClientID == "" {
		a.ClientID = "portal"
	}
	if a.ClientSecret == "" {
		errs = append(errs, errors.New("TENANTRY_OIDC_CLIENT_SECRET is required"))
	}
	if err := checkOrigin(a.PublicURL, false); err != nil {
		errs = append(errs, fmt.Errorf("TENANTRY_PUBLIC_URL=%q: %w", a.PublicURL, err))
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("TENANTRY_SESSION_KEY"))
	if err != nil || len(key) != 32 {
		// The value itself is a secret: never echo it.
		errs = append(errs, errors.New("TENANTRY_SESSION_KEY: want 32 random bytes in base64 (openssl rand -base64 32)"))
	}
	a.SessionKey = key
	return a, errors.Join(errs...)
}

// checkOrigin accepts an https URL, or plain http on the developer's own
// machine (cookies are Secure, and browsers only treat localhost as a secure
// context over http). The public URL is an origin: no path. The issuer has a
// path (/realms/<name>).
func checkOrigin(raw string, pathAllowed bool) error {
	if raw == "" {
		return errors.New("required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("want an absolute URL without credentials, query or fragment")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return errors.New("want https (http only for localhost)")
	}
	if !pathAllowed && u.Path != "" {
		return errors.New("want an origin without a path")
	}
	return nil
}

// KeycloakSync is how the group sync worker reaches the Keycloak Admin API,
// as the service account of the client tenantry-sync (IMPLEMENTATION-PLAN
// §5.2). Only the groupsync loop reads it: its secret manages every user.
type KeycloakSync struct {
	// TENANTRY_KEYCLOAK_URL: Keycloak's base URL, in the cluster
	// http://keycloak-http.keycloak.svc (no /realms path).
	URL          string
	Realm        string // TENANTRY_KEYCLOAK_REALM; default tenantry
	ClientID     string // TENANTRY_KEYCLOAK_CLIENT_ID; default tenantry-sync
	ClientSecret string // TENANTRY_KEYCLOAK_CLIENT_SECRET, from a Secret
}

// LoadKeycloakSync reads KeycloakSync and reports every invalid variable at once.
func LoadKeycloakSync() (KeycloakSync, error) {
	k := KeycloakSync{
		URL:          strings.TrimSuffix(os.Getenv("TENANTRY_KEYCLOAK_URL"), "/"),
		Realm:        os.Getenv("TENANTRY_KEYCLOAK_REALM"),
		ClientID:     os.Getenv("TENANTRY_KEYCLOAK_CLIENT_ID"),
		ClientSecret: os.Getenv("TENANTRY_KEYCLOAK_CLIENT_SECRET"),
	}
	var errs []error
	// Plain http is fine: the worker talks to Keycloak's Service inside the cluster.
	if u, err := url.Parse(k.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "/realms") {
		errs = append(errs, fmt.Errorf("TENANTRY_KEYCLOAK_URL=%q: want Keycloak's base URL, e.g. http://keycloak-http.keycloak.svc", k.URL))
	}
	if k.Realm == "" {
		k.Realm = "tenantry"
	}
	if k.ClientID == "" {
		k.ClientID = "tenantry-sync"
	}
	if k.ClientSecret == "" {
		errs = append(errs, errors.New("TENANTRY_KEYCLOAK_CLIENT_SECRET is required"))
	}
	return k, errors.Join(errs...)
}

// Provisioning configures the provisioning loop (IMPLEMENTATION-PLAN §5.1).
type Provisioning struct {
	// TENANTRY_CLUSTER_ID: the cluster this worker's credential reaches; it
	// delivers that cluster's events only.
	ClusterID string
	// KUBECONFIG: outside the cluster only (dev); in it, the ServiceAccount.
	Kubeconfig string
	// TENANTRY_OPERATION_TIMEOUT: how long a delivered operation may wait for
	// the controller before it is Failed; default 10m (§12, decision 8).
	OperationTimeout time.Duration
}

// LoadProvisioning reads Provisioning and reports every invalid variable at once.
func LoadProvisioning() (Provisioning, error) {
	p := Provisioning{
		ClusterID:        os.Getenv("TENANTRY_CLUSTER_ID"),
		Kubeconfig:       os.Getenv("KUBECONFIG"),
		OperationTimeout: 10 * time.Minute,
	}
	var errs []error
	if p.ClusterID == "" {
		errs = append(errs, errors.New("TENANTRY_CLUSTER_ID is required"))
	}
	if v := os.Getenv("TENANTRY_OPERATION_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute {
			errs = append(errs, fmt.Errorf("TENANTRY_OPERATION_TIMEOUT=%q: want a duration of 1m or more, e.g. 10m", v))
		}
		p.OperationTimeout = d
	}
	return p, errors.Join(errs...)
}
