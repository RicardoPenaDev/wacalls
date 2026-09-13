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
