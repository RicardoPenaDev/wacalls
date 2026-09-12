package tactical

import (
	"net/http"
	"time"
)

const DefaultTimeout = 15 * time.Second

// Config contains the Tactical read-only client configuration.
type Config struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Agent is the stable, deliberately small DTO exposed to the support domain.
type Agent struct {
	AgentID         string
	Hostname        string
	ClientName      string
	SiteName        string
	SiteID          string
	Status          string
	LastSeen        time.Time
	MonitoringType  string
	OperatingSystem string
	LoggedUser      string
	LastLoggedUser  string
	LocalIPs        []string
	SerialNumber    string
	NeedsReboot     bool
	MaintenanceMode bool
	Version         string
	Plat            string
}
