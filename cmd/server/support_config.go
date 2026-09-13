package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"wacalls/internal/glpi"
	"wacalls/internal/tactical"
)

type supportConfig struct {
	Enabled               bool
	CreateTimeoutSeconds  int
	RecoveryMarginSeconds int
	GLPIConfig            glpi.Config
	GLPIWebBaseURL        string
	TacticalConfig        *tactical.Config
}

func parseEnvBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "on", "true", "sim", "yes":
		return true
	}
	return false
}

func parseEnvInt(key string, defaultVal int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultVal, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid %s: must be non-negative integer", key)
	}
	return v, nil
}

func loadSupportConfig() (supportConfig, error) {
	var cfg supportConfig
	cfg.Enabled = parseEnvBool("WACALLS_SUPPORT_ENABLED")
	if !cfg.Enabled {
		return cfg, nil
	}

	var err error
	cfg.CreateTimeoutSeconds, err = parseEnvInt("WACALLS_SUPPORT_CREATE_TIMEOUT_SECONDS", 20)
	if err != nil {
		return cfg, err
	}
	cfg.RecoveryMarginSeconds, err = parseEnvInt("WACALLS_SUPPORT_RECOVERY_MARGIN_SECONDS", 10)
	if err != nil {
		return cfg, err
	}

	// GLPI Configuration is mandatory when support is enabled.
	glpiBaseURL := strings.TrimSpace(os.Getenv("WACALLS_GLPI_BASE_URL"))
	if glpiBaseURL == "" {
		return cfg, errors.New("WACALLS_GLPI_BASE_URL is required when support is enabled")
	}
	u, err := url.Parse(glpiBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return cfg, errors.New("WACALLS_GLPI_BASE_URL must be a valid http or https URL")
	}

	clientID := strings.TrimSpace(os.Getenv("WACALLS_GLPI_CLIENT_ID"))
	if clientID == "" {
		return cfg, errors.New("WACALLS_GLPI_CLIENT_ID is required when support is enabled")
	}
	clientSecret := strings.TrimSpace(os.Getenv("WACALLS_GLPI_CLIENT_SECRET"))
	if clientSecret == "" {
		return cfg, errors.New("WACALLS_GLPI_CLIENT_SECRET is required when support is enabled")
	}
	username := strings.TrimSpace(os.Getenv("WACALLS_GLPI_USERNAME"))
	if username == "" {
		return cfg, errors.New("WACALLS_GLPI_USERNAME is required when support is enabled")
	}
	password := strings.TrimSpace(os.Getenv("WACALLS_GLPI_PASSWORD"))
	if password == "" {
		return cfg, errors.New("WACALLS_GLPI_PASSWORD is required when support is enabled")
	}

	glpiTimeoutSec, err := parseEnvInt("WACALLS_GLPI_TIMEOUT_SECONDS", 20)
	if err != nil {
		return cfg, err
	}

	var entityRecursive *bool
	if rawRec := strings.TrimSpace(os.Getenv("WACALLS_GLPI_ENTITY_RECURSIVE")); rawRec != "" {
		rec := parseEnvBool("WACALLS_GLPI_ENTITY_RECURSIVE")
		entityRecursive = &rec
	}

	cfg.GLPIConfig = glpi.Config{
		BaseURL:         glpiBaseURL,
		ClientID:        clientID,
		ClientSecret:    clientSecret,
		Username:        username,
		Password:        password,
		EntityID:        strings.TrimSpace(os.Getenv("WACALLS_GLPI_ENTITY_ID")),
		ProfileID:       strings.TrimSpace(os.Getenv("WACALLS_GLPI_PROFILE_ID")),
		EntityRecursive: entityRecursive,
		AcceptLanguage:  strings.TrimSpace(os.Getenv("WACALLS_GLPI_ACCEPT_LANGUAGE")),
		Timeout:         time.Duration(glpiTimeoutSec) * time.Second,
	}

	// GLPI Web Base URL is optional. If unset, web links to GLPI tickets are disabled.
	glpiWebBaseURL := strings.TrimSpace(os.Getenv("WACALLS_GLPI_WEB_BASE_URL"))
	if glpiWebBaseURL != "" {
		normURL, err := validateAndNormalizeGLPIWebBaseURL(glpiWebBaseURL, cfg.GLPIConfig.BaseURL)
		if err != nil {
			return cfg, err
		}
		cfg.GLPIWebBaseURL = normURL
	}

	// Tactical RMM Configuration is optional.
	tacticalBaseURL := strings.TrimSpace(os.Getenv("WACALLS_TACTICAL_BASE_URL"))
	tacticalAPIKey := strings.TrimSpace(os.Getenv("WACALLS_TACTICAL_API_KEY"))

	if tacticalBaseURL == "" && tacticalAPIKey == "" {
		// Both empty: Tactical capability is disabled.
		cfg.TacticalConfig = nil
	} else if tacticalBaseURL == "" || tacticalAPIKey == "" {
		// Partial configuration: invalid, abort startup.
		return cfg, errors.New("incomplete Tactical RMM configuration: both WACALLS_TACTICAL_BASE_URL and WACALLS_TACTICAL_API_KEY must be provided")
	} else {
		tu, err := url.Parse(tacticalBaseURL)
		if err != nil || (tu.Scheme != "http" && tu.Scheme != "https") || tu.Host == "" {
			return cfg, errors.New("WACALLS_TACTICAL_BASE_URL must be a valid http or https URL")
		}
		tacticalTimeoutSec, err := parseEnvInt("WACALLS_TACTICAL_TIMEOUT_SECONDS", 15)
		if err != nil {
			return cfg, err
		}
		cfg.TacticalConfig = &tactical.Config{
			BaseURL: tacticalBaseURL,
			APIKey:  tacticalAPIKey,
			Timeout: time.Duration(tacticalTimeoutSec) * time.Second,
		}
	}

	return cfg, nil
}

func matchGLPIOrigins(webU *url.URL, apiRaw string) error {
	if apiRaw == "" {
		return nil
	}
	apiU, err := url.Parse(apiRaw)
	if err != nil || apiU.Host == "" {
		return nil
	}

	webScheme := strings.ToLower(webU.Scheme)
	apiScheme := strings.ToLower(apiU.Scheme)
	if webScheme != apiScheme {
		return fmt.Errorf("WACALLS_GLPI_WEB_BASE_URL scheme (%s) does not match WACALLS_GLPI_BASE_URL scheme (%s)", webScheme, apiScheme)
	}

	webHost := strings.ToLower(webU.Hostname())
	apiHost := strings.ToLower(apiU.Hostname())
	if webHost != apiHost {
		return fmt.Errorf("WACALLS_GLPI_WEB_BASE_URL host (%s) does not match WACALLS_GLPI_BASE_URL host (%s)", webHost, apiHost)
	}

	webPort := webU.Port()
	if webPort == "" {
		if webScheme == "https" {
			webPort = "443"
		} else if webScheme == "http" {
			webPort = "80"
		}
	}

	apiPort := apiU.Port()
	if apiPort == "" {
		if apiScheme == "https" {
			apiPort = "443"
		} else if apiScheme == "http" {
			apiPort = "80"
		}
	}

	if webPort != apiPort {
		return fmt.Errorf("WACALLS_GLPI_WEB_BASE_URL port (%s) does not match WACALLS_GLPI_BASE_URL port (%s)", webPort, apiPort)
	}

	return nil
}

func validateAndNormalizeGLPIWebBaseURL(webRaw, apiRaw string) (string, error) {
	trimmed := strings.TrimSpace(webRaw)
	if trimmed == "" {
		return "", errors.New("WACALLS_GLPI_WEB_BASE_URL is empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return "", errors.New("WACALLS_GLPI_WEB_BASE_URL must be a valid URL")
	}
	if u.User != nil {
		return "", errors.New("WACALLS_GLPI_WEB_BASE_URL must not contain user credentials (userinfo)")
	}
	if u.RawQuery != "" {
		return "", errors.New("WACALLS_GLPI_WEB_BASE_URL must not contain query parameters")
	}
	if u.Fragment != "" {
		return "", errors.New("WACALLS_GLPI_WEB_BASE_URL must not contain fragment")
	}

	if strings.ToLower(u.Scheme) != "https" {
		return "", errors.New("WACALLS_GLPI_WEB_BASE_URL must use https")
	}

	if err := matchGLPIOrigins(u, apiRaw); err != nil {
		return "", err
	}

	normalized := strings.TrimRight(trimmed, "/")
	return normalized, nil
}

func buildGLPITicketWebURL(webBaseURL, ticketID string) (string, error) {
	if strings.TrimSpace(webBaseURL) == "" {
		return "", errors.New("GLPI web base URL is empty")
	}
	trimmedID := strings.TrimSpace(ticketID)
	if trimmedID == "" {
		return "", errors.New("ticket ID is empty")
	}
	if len(trimmedID) == 0 || trimmedID[0] < '1' || trimmedID[0] > '9' {
		return "", errors.New("ticket ID must be a positive decimal integer")
	}
	for i := 1; i < len(trimmedID); i++ {
		if trimmedID[i] < '0' || trimmedID[i] > '9' {
			return "", errors.New("ticket ID must be a positive decimal integer")
		}
	}
	idNum, err := strconv.ParseInt(trimmedID, 10, 64)
	if err != nil || idNum <= 0 || strconv.FormatInt(idNum, 10) != trimmedID {
		return "", errors.New("ticket ID must be a positive decimal integer")
	}

	base, err := url.Parse(strings.TrimSpace(webBaseURL))
	if err != nil || base.Host == "" {
		return "", errors.New("invalid GLPI web base URL")
	}
	if base.User != nil {
		return "", errors.New("GLPI web base URL must not contain user credentials")
	}
	if base.RawQuery != "" || base.Fragment != "" {
		return "", errors.New("GLPI web base URL must not contain query or fragment")
	}

	if strings.ToLower(base.Scheme) != "https" {
		return "", errors.New("GLPI web base URL must use https")
	}

	q := url.Values{}
	q.Set("id", trimmedID)

	target := &url.URL{
		Scheme:   base.Scheme,
		Host:     base.Host,
		Path:     "/front/ticket.form.php",
		RawQuery: q.Encode(),
	}

	finalStr := target.String()

	parsed, err := url.Parse(finalStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse generated URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", errors.New("generated URL scheme must be https")
	}
	if parsed.Host != base.Host {
		return "", errors.New("generated URL host does not match base host")
	}
	if parsed.User != nil {
		return "", errors.New("generated URL contains credentials")
	}
	if parsed.Path != "/front/ticket.form.php" {
		return "", errors.New("generated URL path is not /front/ticket.form.php")
	}
	vals := parsed.Query()
	if len(vals) != 1 || vals.Get("id") != trimmedID {
		return "", errors.New("generated URL query must contain only the valid ticket id")
	}
	if parsed.Fragment != "" {
		return "", errors.New("generated URL must not contain fragment")
	}

	lower := strings.ToLower(finalStr)
	if strings.Contains(lower, "javascript:") ||
		strings.Contains(lower, "data:") ||
		strings.Contains(lower, "vbscript:") ||
		strings.Contains(lower, "@") {
		return "", errors.New("generated URL contains forbidden scheme or characters")
	}

	return finalStr, nil
}
