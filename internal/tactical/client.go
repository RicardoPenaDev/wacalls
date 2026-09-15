// Package tactical implements the minimal read-only Tactical RMM API client
// used by WACalls. It intentionally exposes no remote-action operations.
package tactical

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxAgentBodyBytes = 2 << 20
	// maxAgentIDLength bounds one Tactical path segment. Real agent IDs are
	// substantially shorter; 128 bytes leaves room for future ID formats.
	maxAgentIDLength = 128
)

var errCrossHostRedirect = errors.New("cross-host redirect blocked")

// Client is safe for concurrent read operations.
type Client struct {
	baseURL *url.URL
	apiKey  string
	http    *http.Client
	now     func() time.Time
}

// New validates cfg and applies a same-origin redirect policy. The default
// transport keeps standard TLS certificate verification enabled.
func New(cfg Config) (*Client, error) {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	if cfg.BaseURL == "" {
		return nil, configError("WACALLS_TACTICAL_BASE_URL")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, configError("WACALLS_TACTICAL_API_KEY")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, configError("WACALLS_TACTICAL_BASE_URL")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return nil, configError("WACALLS_TACTICAL_TIMEOUT_SECONDS")
	}
	httpClient := &http.Client{}
	if cfg.HTTPClient != nil {
		clone := *cfg.HTTPClient
		httpClient = &clone
	}
	httpClient.Timeout = timeout
	previousRedirectPolicy := httpClient.CheckRedirect
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !sameOrigin(baseURL, req.URL) || (len(via) > 0 && via[0].Method != http.MethodGet) {
			return errCrossHostRedirect
		}
		if previousRedirectPolicy != nil {
			return previousRedirectPolicy(req, via)
		}
		return nil
	}
	return &Client{baseURL: baseURL, apiKey: cfg.APIKey, http: httpClient, now: time.Now}, nil
}

func configError(field string) error {
	return &Error{Op: "config", Kind: ErrConfig, Field: field}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// ListAgents returns the mapped stable subset from GET /agents/. A single
// agent with an unparseable last_seen no longer aborts the whole listing
// (see mapListAgent/parseLastSeen): that agent is still returned, with
// LastSeenValid=false, so one bad timestamp elsewhere in the tenant can't
// hide every other agent from hostname lookups.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	body, err := c.get(ctx, "/agents/", "list agents")
	if err != nil {
		return nil, err
	}
	var wire []listAgentDTO
	if err := decodeJSON(body, &wire); err != nil {
		return nil, &Error{Op: "list agents", Kind: ErrBadResponse}
	}
	agents := make([]Agent, 0, len(wire))
	for _, item := range wire {
		agents = append(agents, mapListAgent(item))
	}
	return agents, nil
}

// GetAgent returns the mapped stable subset from GET /agents/{agent_id}/.
func (c *Client) GetAgent(ctx context.Context, agentID string) (Agent, error) {
	if !validAgentID(agentID) {
		return Agent{}, &Error{Op: "get agent", Kind: ErrBadRequest}
	}
	body, err := c.get(ctx, "/agents/"+agentID+"/", "get agent")
	if err != nil {
		return Agent{}, err
	}
	var wire detailAgentDTO
	if err := decodeJSON(body, &wire); err != nil {
		return Agent{}, &Error{Op: "get agent", Kind: ErrBadResponse}
	}
	return mapDetailAgent(wire), nil
}

func validAgentID(agentID string) bool {
	if len(agentID) == 0 || len(agentID) > maxAgentIDLength {
		return false
	}
	for i := 0; i < len(agentID); i++ {
		character := agentID[i]
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

// FindAgentByHostname lists agents and selects only an exact normalized match.
func (c *Client) FindAgentByHostname(ctx context.Context, hostname string) (Agent, error) {
	normalized := normalizeHostname(hostname)
	if normalized == "" {
		return Agent{}, &Error{Op: "find agent", Kind: ErrBadRequest}
	}
	agents, err := c.ListAgents(ctx)
	if err != nil {
		return Agent{}, err
	}
	var match Agent
	matches := 0
	for _, agent := range agents {
		if normalizeHostname(agent.Hostname) == normalized {
			match = agent
			matches++
		}
	}
	switch matches {
	case 0:
		return Agent{}, &Error{Op: "find agent", Kind: ErrNotFound}
	case 1:
		return match, nil
	default:
		return Agent{}, &Error{Op: "find agent", Kind: ErrAmbiguous}
	}
}

func (c *Client) get(ctx context.Context, path, op string) ([]byte, error) {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	endpoint.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, &Error{Op: op, Kind: ErrBadRequest}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-KEY", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		// errors.Is against the *url.Error returned by http.Client.Do
		// correctly detects both a caller-supplied context deadline/cancel
		// AND the client's own configured Timeout firing (net/http wraps
		// the latter so it also satisfies context.DeadlineExceeded) — it is
		// the single reliable check, unlike inspecting ctx.Err() alone,
		// which misses the Timeout-fired-with-no-caller-deadline case
		// (T-007 7.4-R2: this previously fell through to the generic
		// ErrUnavailable, indistinguishable from a connection failure).
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return nil, &Error{Op: op, Kind: ErrTimeout}
		case errors.Is(err, context.Canceled):
			return nil, &Error{Op: op, Kind: ErrCanceled}
		case errors.Is(err, errCrossHostRedirect):
			return nil, &Error{Op: op, Kind: ErrBadResponse}
		default:
			return nil, &Error{Op: op, Kind: ErrUnavailable}
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(op, resp.StatusCode, resp.Header, c.now())
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return nil, &Error{Op: op, Kind: ErrBadResponse}
	}
	return body, nil
}

func readLimited(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxAgentBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAgentBodyBytes {
		return nil, ErrBadResponse
	}
	return data, nil
}

func decodeJSON(data []byte, out any) error {
	if len(data) == 0 {
		return io.ErrUnexpectedEOF
	}
	return json.Unmarshal(data, out)
}

func statusError(op string, status int, header http.Header, now time.Time) error {
	kind := ErrBadResponse
	switch status {
	case http.StatusBadRequest:
		kind = ErrBadRequest
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = ErrAuth
	case http.StatusNotFound:
		kind = ErrNotFound
	case http.StatusConflict:
		kind = ErrConflict
	case http.StatusTooManyRequests:
		kind = ErrRateLimited
	default:
		if status >= 500 && status <= 599 {
			kind = ErrUnavailable
		}
	}
	return &Error{Op: op, Kind: kind, StatusCode: status, RetryAfter: parseRetryAfter(header.Get("Retry-After"), now)}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		const maxSeconds = int64((time.Duration(1<<63 - 1)) / time.Second)
		if seconds <= 0 || seconds > maxSeconds {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

// normalizeHostname deliberately duplicates only the T-003 comparison rule.
// T-005 must revalidate matches in the support domain before persistence.
func normalizeHostname(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

type listAgentDTO struct {
	AgentID         string   `json:"agent_id"`
	Hostname        string   `json:"hostname"`
	ClientName      string   `json:"client_name"`
	SiteName        string   `json:"site_name"`
	Status          string   `json:"status"`
	LastSeen        string   `json:"last_seen"`
	MonitoringType  string   `json:"monitoring_type"`
	OperatingSystem string   `json:"operating_system"`
	LoggedUsername  string   `json:"logged_username"`
	LocalIPs        []string `json:"local_ips"`
	SerialNumber    string   `json:"serial_number"`
	NeedsReboot     bool     `json:"needs_reboot"`
	MaintenanceMode bool     `json:"maintenance_mode"`
}

type detailAgentDTO struct {
	AgentID          string      `json:"agent_id"`
	Hostname         string      `json:"hostname"`
	Client           string      `json:"client"`
	SiteName         string      `json:"site_name"`
	Site             json.Number `json:"site"`
	Status           string      `json:"status"`
	LastSeen         string      `json:"last_seen"`
	MonitoringType   string      `json:"monitoring_type"`
	OperatingSystem  string      `json:"operating_system"`
	LoggedInUsername string      `json:"logged_in_username"`
	LastLoggedInUser string      `json:"last_logged_in_user"`
	LocalIPs         []string    `json:"local_ips"`
	SerialNumber     string      `json:"serial_number"`
	NeedsReboot      bool        `json:"needs_reboot"`
	MaintenanceMode  bool        `json:"maintenance_mode"`
	Version          string      `json:"version"`
	Plat             string      `json:"plat"`
}

func mapListAgent(wire listAgentDTO) Agent {
	lastSeen, ok := parseLastSeen(wire.LastSeen)
	return Agent{
		AgentID: wire.AgentID, Hostname: wire.Hostname, ClientName: wire.ClientName,
		SiteName: wire.SiteName, Status: wire.Status, LastSeen: lastSeen, LastSeenValid: ok,
		MonitoringType: wire.MonitoringType, OperatingSystem: wire.OperatingSystem,
		LoggedUser: wire.LoggedUsername, LocalIPs: wire.LocalIPs,
		SerialNumber: wire.SerialNumber, NeedsReboot: wire.NeedsReboot,
		MaintenanceMode: wire.MaintenanceMode,
	}
}

func mapDetailAgent(wire detailAgentDTO) Agent {
	lastSeen, ok := parseLastSeen(wire.LastSeen)
	return Agent{
		AgentID: wire.AgentID, Hostname: wire.Hostname, ClientName: wire.Client,
		SiteName: wire.SiteName, SiteID: wire.Site.String(), Status: wire.Status,
		LastSeen: lastSeen, LastSeenValid: ok, MonitoringType: wire.MonitoringType,
		OperatingSystem: wire.OperatingSystem, LoggedUser: wire.LoggedInUsername,
		LastLoggedUser: wire.LastLoggedInUser, LocalIPs: wire.LocalIPs,
		SerialNumber: wire.SerialNumber, NeedsReboot: wire.NeedsReboot,
		MaintenanceMode: wire.MaintenanceMode, Version: wire.Version, Plat: wire.Plat,
	}
}

// tacticalLegacyLastSeenLayout is an older Tactical RMM last_seen shape,
// observed without any UTC offset (e.g. "09/14/2026 22:58:35"). Its
// timezone is not documented anywhere in the Tactical API, config, or this
// repo's docs. A live read-only check against the homologation tenant
// (2026-09-15) found every agent reporting the newer RFC3339 "...Z" shape
// instead, so the legacy shape could not be cross-checked against a real
// "online" timestamp. Until a live occurrence confirms it, values in this
// shape are recognized (so they are not confused with garbage) but treated
// as untrusted: parseLastSeen returns a zero time and ok=false rather than
// silently guessing UTC or local time.
const tacticalLegacyLastSeenLayout = "01/02/2006 15:04:05"

// parseLastSeen tolerates every last_seen shape observed from Tactical RMM
// plus the documented RFC3339 (time.Parse already accepts an optional
// fractional-second component and either "Z" or a numeric offset against
// that single layout). It never returns an error: a missing, malformed, or
// timezone-unconfirmed value degrades to a zero time with ok=false instead
// of failing the whole agent record (see mapListAgent/mapDetailAgent) or
// aborting ListAgents for every other agent in the tenant.
func parseLastSeen(value string) (t time.Time, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	if _, err := time.Parse(tacticalLegacyLastSeenLayout, value); err == nil {
		return time.Time{}, false
	}
	return time.Time{}, false
}
