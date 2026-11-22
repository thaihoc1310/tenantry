// Package config reads the environment shared by every backend binary, so a
// typo in a variable fails at start-up instead of silently using a default.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL string     // DATABASE_URL, required
	Addr        string     // TENANTRY_ADDR, API listen address; default :8080
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
