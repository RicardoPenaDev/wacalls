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
	AgentID    string
	Hostname   string
	ClientName string
	SiteName   string
	SiteID     string
	Status     string
	LastSeen   time.Time
	// LastSeenValid is false when the upstream last_seen value was empty,
	// malformed, or in a recognized-but-timezone-unconfirmed legacy format
	// (see parseLastSeen). The agent itself is still returned normally;
	// LastSeen is left zero and callers must not treat that as "just seen".
	// Internal-only: tagged json:"-" because Agent is serialized verbatim
	// into the public GET /api/support/devices/{id} response.
	LastSeenValid   bool `json:"-"`
	MonitoringType  string
	OperatingSystem string
	LoggedUser      string
	LastLoggedUser  string
	LocalIPs        []string
	// LocalIPsValid is false when the upstream local_ips value had a shape
	// that could not be interpreted at all (T-007 7.4-R3: number, boolean,
	// object, or an array containing a non-string element — the live
	// homologation tenant was observed sending a JSON string instead of an
	// array, which is tolerated, not one of these invalid shapes). LocalIPs
	// is left empty in that case; the agent itself, AgentID, Hostname, and
	// every other field are still returned normally. Internal-only: tagged
	// json:"-" for the same reason as LastSeenValid.
	LocalIPsValid   bool `json:"-"`
	SerialNumber    string
	NeedsReboot     bool
	MaintenanceMode bool
	Version         string
	Plat            string
}
