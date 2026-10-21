// Package config loads runtime configuration from environment variables.
//
// Every option has a default so that `docker run` with one volume and the
// address of an ONLYOFFICE Document Server works out of the box.
// See docs/configuration.md.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Host           string
	Port           int
	BaseURL        string
	DataDir        string
	DBPath         string
	FilesDir       string // built-in storage: FilesDir/<user id>/
	LogLevel       string
	LogFormat      string // text | json
	TrustProxy     bool
	MetricsEnabled bool
	MaxUploadMB    int

	AuthMode      string // local | none
	SessionTTL    time.Duration
	SecureCookies bool
	PasswordLogin bool
	AdminEmail    string
	AdminPassword string
	AdminName     string

	// Sign in with an OpenID Connect provider. On a box with Pocket Core
	// none of these are needed: the app uses the client Core issued.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCName         string
	OIDCAutoCreate   bool

	// Pocket Core: registration, service credentials (Nextcloud) and app
	// links (Pocket Documents) for each person.
	CoreURL             string
	CoreRegistrationKey string
	CoreAppURL          string
	CorePublicURL       string

	// ONLYOFFICE Document Server, the editing engine. These are the initial
	// values; an admin can change them in Settings → Document Server.
	OnlyOfficeURL         string // what browsers load the editor from
	OnlyOfficeInternalURL string // how this server reaches it (conversion); defaults to OnlyOfficeURL
	OnlyOfficeJWTSecret   string
	OnlyOfficeJWTHeader   string
	// CallbackURL is how Document Server reaches this app to fetch files and
	// report saves. Defaults to CORE_APP_URL, then BASE_URL.
	CallbackURL string

	// Collabora Online (CODE) as a second engine, reached over WOPI.
	CollaboraURL         string // what browsers load the editor from
	CollaboraInternalURL string // how this server fetches hosting/discovery; defaults to CollaboraURL
	// WOPIURL is how Collabora reaches this app AND the origin the browser
	// embeds Collabora from (Collabora only allows the WOPI host's origin to
	// frame it), so it defaults to BASE_URL rather than CALLBACK_URL.
	WOPIURL string
	// DefaultEngine is the box's default editor: onlyoffice | collabora
	// (an admin can change it in Settings; each person can override it).
	DefaultEngine string

	// FolderRoots are server folders people may attach as a source
	// (comma separated absolute paths). Empty = none.
	FolderRoots []string
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(env(key, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envInt(key string, def int) int {
	if v := env(key, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := env(key, ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	dataDir := env("DATA_DIR", "./data")
	c := &Config{
		Host:           env("HOST", "0.0.0.0"),
		Port:           envInt("PORT", 8080),
		BaseURL:        strings.TrimRight(env("BASE_URL", ""), "/"),
		DataDir:        dataDir,
		DBPath:         env("DB_PATH", dataDir+"/pocket-office.db"),
		FilesDir:       env("FILES_DIR", dataDir+"/files"),
		LogLevel:       strings.ToLower(env("LOG_LEVEL", "info")),
		LogFormat:      strings.ToLower(env("LOG_FORMAT", "text")),
		TrustProxy:     envBool("TRUST_PROXY", true),
		MetricsEnabled: envBool("METRICS_ENABLED", true),
		MaxUploadMB:    envInt("MAX_UPLOAD_MB", 200),

		AuthMode:      strings.ToLower(env("AUTH_MODE", "local")),
		SessionTTL:    envDuration("SESSION_TTL", 720*time.Hour),
		SecureCookies: envBool("SECURE_COOKIES", false),
		PasswordLogin: envBool("PASSWORD_LOGIN", true),
		AdminEmail:    strings.ToLower(env("ADMIN_EMAIL", "")),
		AdminPassword: env("ADMIN_PASSWORD", ""),
		AdminName:     env("ADMIN_NAME", "Admin"),

		OIDCIssuer:       strings.TrimRight(env("OIDC_ISSUER", ""), "/"),
		OIDCClientID:     env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("OIDC_CLIENT_SECRET", ""),
		OIDCName:         env("OIDC_NAME", "single sign-on"),
		OIDCAutoCreate:   envBool("OIDC_AUTO_CREATE", true),

		CoreURL:             strings.TrimRight(env("CORE_URL", ""), "/"),
		CoreRegistrationKey: env("CORE_REGISTRATION_KEY", ""),
		CoreAppURL:          strings.TrimRight(env("CORE_APP_URL", ""), "/"),
		CorePublicURL:       strings.TrimRight(env("CORE_PUBLIC_URL", ""), "/"),

		OnlyOfficeURL:         strings.TrimRight(env("ONLYOFFICE_URL", ""), "/"),
		OnlyOfficeInternalURL: strings.TrimRight(env("ONLYOFFICE_INTERNAL_URL", ""), "/"),
		OnlyOfficeJWTSecret:   env("ONLYOFFICE_JWT_SECRET", ""),
		OnlyOfficeJWTHeader:   env("ONLYOFFICE_JWT_HEADER", "AuthorizationJwt"),
		CallbackURL:           strings.TrimRight(env("CALLBACK_URL", ""), "/"),
		CollaboraURL:          strings.TrimRight(env("COLLABORA_URL", ""), "/"),
		CollaboraInternalURL:  strings.TrimRight(env("COLLABORA_INTERNAL_URL", ""), "/"),
		WOPIURL:               strings.TrimRight(env("WOPI_URL", ""), "/"),
		DefaultEngine:         strings.ToLower(env("DEFAULT_ENGINE", "onlyoffice")),
	}
	if c.CoreAppURL == "" {
		c.CoreAppURL = c.BaseURL
	}
	if c.CallbackURL == "" {
		c.CallbackURL = c.CoreAppURL
	}
	if c.OnlyOfficeInternalURL == "" {
		c.OnlyOfficeInternalURL = c.OnlyOfficeURL
	}
	for _, p := range strings.Split(env("FOLDER_ROOTS", ""), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("FOLDER_ROOTS must be absolute paths (got %q)", p)
		}
		c.FolderRoots = append(c.FolderRoots, filepath.Clean(p))
	}
	switch c.AuthMode {
	case "local", "none":
	default:
		return nil, fmt.Errorf("AUTH_MODE must be local or none (got %q)", c.AuthMode)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return nil, fmt.Errorf("PORT out of range: %d", c.Port)
	}
	if c.OIDCIssuer != "" && (c.OIDCClientID == "" || c.BaseURL == "") {
		return nil, fmt.Errorf("OIDC_ISSUER needs OIDC_CLIENT_ID and BASE_URL (the callback is BASE_URL/api/v1/auth/oidc/callback)")
	}
	if c.MaxUploadMB < 1 {
		c.MaxUploadMB = 1
	}
	return c, nil
}

// Addr returns host:port.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }
