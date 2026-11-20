// Package config reads the environment shared by every backend binary, so a
// typo in a variable fails at start-up instead of silently using a default.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL string     // DATABASE_URL, required
	Addr        string     // TENANTRY_ADDR, API listen address; default :8080
	DevAuth     bool       // TENANTRY_AUTH_DEV_HEADER; never true outside local dev
	LogLevel    slog.Level // TENANTRY_LOG_LEVEL: debug, info (default), warn, error
	LogFormat   string     // TENANTRY_LOG_FORMAT: json (default) or text
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
	if v := os.Getenv("TENANTRY_AUTH_DEV_HEADER"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("TENANTRY_AUTH_DEV_HEADER=%q: want true or false", v))
		}
		c.DevAuth = b
	}
	if v := os.Getenv("TENANTRY_LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("TENANTRY_LOG_LEVEL=%q: want debug, info, warn or error", v))
		}
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
