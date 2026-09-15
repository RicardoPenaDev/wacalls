package tactical

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type tacticalContract interface {
	FindAgentByHostname(context.Context, string) (Agent, error)
	GetAgent(context.Context, string) (Agent, error)
}

var _ tacticalContract = (*Client)(nil)

func testConfig(baseURL string) Config {
	return Config{BaseURL: baseURL, APIKey: "test-api-key", Timeout: time.Second}
}

func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestNewValidatesConfig(t *testing.T) {
	valid := testConfig("https://tactical.invalid/api")
	if _, err := New(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	cfg := valid
	cfg.BaseURL = ""
	if _, err := New(cfg); !errors.Is(err, ErrConfig) {
		t.Fatalf("empty URL: expected ErrConfig, got %v", err)
	}
	cfg = valid
	cfg.APIKey = ""
	if _, err := New(cfg); !errors.Is(err, ErrConfig) {
		t.Fatalf("empty API key: expected ErrConfig, got %v", err)
	}
	for _, invalid := range []string{"ftp://tactical.invalid", "https://", "https://user:key@tactical.invalid", "https://tactical.invalid?q=key", "https://tactical.invalid#fragment"} {
		t.Run(invalid, func(t *testing.T) {
			cfg := valid
			cfg.BaseURL = invalid
			if _, err := New(cfg); !errors.Is(err, ErrConfig) {
				t.Fatalf("expected ErrConfig, got %v", err)
			}
		})
	}
	cfg = valid
	cfg.Timeout = -time.Second
	if _, err := New(cfg); !errors.Is(err, ErrConfig) {
		t.Fatalf("negative timeout: expected ErrConfig, got %v", err)
	}
}

func TestListAgentsAuthenticatesUsesSlashAndMaps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/agents/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-API-KEY"); got != "test-api-key" {
			t.Errorf("X-API-KEY = %q", got)
		}
		fmt.Fprint(w, `[{
			"agent_id":"agent-1","hostname":"sde-ars-rcp-02",
			"client_name":"Health","site_name":"Unit A","status":"online",
			"last_seen":"2026-09-12T12:00:00Z","monitoring_type":"server",
			"operating_system":"Windows 11","logged_username":"DOMAIN\\user",
			"local_ips":["10.0.0.4"],"serial_number":"SERIAL",
			"needs_reboot":true,"maintenance_mode":false,
			"unknown_future_field":{"ignored":true}
		}]`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL+"/api"))
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("agents = %+v", agents)
	}
	agent := agents[0]
	if agent.AgentID != "agent-1" || agent.Hostname != "sde-ars-rcp-02" || agent.ClientName != "Health" || agent.SiteName != "Unit A" || agent.LoggedUser != `DOMAIN\user` {
		t.Fatalf("unexpected mapping: %+v", agent)
	}
	if agent.LastSeen.Format(time.RFC3339) != "2026-09-12T12:00:00Z" || len(agent.LocalIPs) != 1 || !agent.NeedsReboot || agent.MaintenanceMode {
		t.Fatalf("unexpected optional mapping: %+v", agent)
	}
}

func TestGetAgentUsesDetailDTOAndMapsAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/agent-1/" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{
			"agent_id":"agent-1","hostname":"HOST-01","client":"Health Detail",
			"client_name":"wrong-list-alias","site_name":"Unit B","site":23,
			"status":"offline","last_seen":"2026-09-12T10:30:00Z",
			"monitoring_type":"workstation","operating_system":"Windows 10",
			"logged_in_username":"detail-user","logged_username":"wrong-list-user",
			"last_logged_in_user":"previous-user","local_ips":[],
			"serial_number":"DETAIL-SERIAL","needs_reboot":false,
			"maintenance_mode":true,"version":"2.9.0","plat":"windows"
		}`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	agent, err := client.GetAgent(context.Background(), "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if agent.ClientName != "Health Detail" || agent.LoggedUser != "detail-user" || agent.LastLoggedUser != "previous-user" || agent.SiteID != "23" {
		t.Fatalf("detail aliases mapped incorrectly: %+v", agent)
	}
	if agent.Version != "2.9.0" || agent.Plat != "windows" || !agent.MaintenanceMode {
		t.Fatalf("detail fields mapped incorrectly: %+v", agent)
	}
}

func TestGetAgentValidatesIDBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/agents/agent_ABC-123/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"agent_id":"agent_ABC-123","hostname":"HOST-01"}`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))

	if _, err := client.GetAgent(context.Background(), "agent_ABC-123"); err != nil {
		t.Fatalf("valid ID: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("valid ID requests = %d, want 1", got)
	}
	requests.Store(0)

	invalid := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"dot", "."},
		{"double dot", ".."},
		{"parent agents", "../agents"},
		{"forward slash", "abc/def"},
		{"backward slash", `abc\def`},
		{"encoded traversal", "%2e%2e"},
		{"encoded slash", "%2f"},
		{"query", "abc?x=1"},
		{"fragment", "abc#fragment"},
		{"spaces", " abc "},
		{"newline", "abc\n123"},
		{"excessive", strings.Repeat("a", maxAgentIDLength+1)},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.GetAgent(context.Background(), test.id); !errors.Is(err, ErrBadRequest) {
				t.Fatalf("GetAgent(%q): expected ErrBadRequest, got %v", test.id, err)
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid ID requests = %d, want 0", got)
	}
}

func TestOptionalFieldsMayBeAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"agent_id":"agent-1","hostname":"HOST-01"}]`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || !agents[0].LastSeen.IsZero() || agents[0].LocalIPs != nil {
		t.Fatalf("unexpected optional values: %+v", agents)
	}
}

func TestFindAgentExactMatchCardinality(t *testing.T) {
	tests := []struct {
		name string
		body string
		kind error
		want string
	}{
		{"exact", `[{"agent_id":"a1","hostname":" sde-ars-rcp-02 "},{"agent_id":"a2","hostname":"SDE-ARS-RCP-020"}]`, nil, "a1"},
		{"absent", `[{"agent_id":"a2","hostname":"SDE-ARS-RCP-020"}]`, ErrNotFound, ""},
		{"ambiguous", `[{"agent_id":"a1","hostname":"SDE-ARS-RCP-02"},{"agent_id":"a2","hostname":"sde-ars-rcp-02"}]`, ErrAmbiguous, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := mustClient(t, testConfig(server.URL))
			agent, err := client.FindAgentByHostname(context.Background(), "  Sde-Ars-Rcp-02 ")
			if tc.kind != nil {
				if !errors.Is(err, tc.kind) {
					t.Fatalf("expected %v, got %v", tc.kind, err)
				}
				return
			}
			if err != nil || agent.AgentID != tc.want {
				t.Fatalf("agent=%+v err=%v", agent, err)
			}
		})
	}
}

func TestHTTPStatusMatrix(t *testing.T) {
	tests := []struct {
		status int
		kind   error
	}{
		{400, ErrBadRequest}, {401, ErrAuth}, {403, ErrAuth}, {404, ErrNotFound},
		{409, ErrConflict}, {429, ErrRateLimited}, {500, ErrUnavailable}, {503, ErrUnavailable},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusTooManyRequests || tc.status == http.StatusServiceUnavailable {
					w.Header().Set("Retry-After", "11")
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			client := mustClient(t, testConfig(server.URL))
			_, err := client.GetAgent(context.Background(), "agent-1")
			if !errors.Is(err, tc.kind) {
				t.Fatalf("expected %v, got %v", tc.kind, err)
			}
			if tc.status == http.StatusTooManyRequests || tc.status == http.StatusServiceUnavailable {
				var apiErr *Error
				if !errors.As(err, &apiErr) || apiErr.RetryAfter != 11*time.Second {
					t.Fatalf("RetryAfter = %+v", apiErr)
				}
			}
		})
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"future", now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{"past", now.Add(-time.Second).Format(http.TimeFormat), 0},
		{"invalid", "not-a-date", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseRetryAfter(test.value, now); got != test.want {
				t.Fatalf("parseRetryAfter(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.Timeout = 20 * time.Millisecond
	client := mustClient(t, cfg)
	if _, err := client.ListAgents(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("HTTP client timeout: expected ErrUnavailable, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client = mustClient(t, testConfig(server.URL))
	if _, err := client.ListAgents(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation: expected context.Canceled, got %v", err)
	}

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer deadlineCancel()
	if _, err := client.ListAgents(deadlineCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline: expected context.DeadlineExceeded, got %v", err)
	}
}

func TestMalformedAndOversizedResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed", `[{"agent_id":`},
		{"oversized", strings.Repeat("x", maxAgentBodyBytes+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := mustClient(t, testConfig(server.URL))
			if _, err := client.ListAgents(context.Background()); !errors.Is(err, ErrBadResponse) {
				t.Fatalf("expected ErrBadResponse, got %v", err)
			}
		})
	}
}

func TestReadLimitedBoundary(t *testing.T) {
	body, err := readLimited(strings.NewReader(strings.Repeat("x", maxAgentBodyBytes)))
	if err != nil || len(body) != maxAgentBodyBytes {
		t.Fatalf("exact limit: len=%d err=%v", len(body), err)
	}
	if _, err := readLimited(strings.NewReader(strings.Repeat("x", maxAgentBodyBytes+1))); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("over limit: expected ErrBadResponse, got %v", err)
	}
}

func TestParseLastSeen(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantValid bool
		wantTime  time.Time
	}{
		{"rfc3339_utc", "2026-09-12T12:00:00Z", true, time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)},
		{"rfc3339_offset", "2026-09-12T09:00:00-03:00", true, time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)},
		{"rfc3339_fractional_seconds", "2026-09-15T01:51:51.544513Z", true, time.Date(2026, 9, 15, 1, 51, 51, 544513000, time.UTC)},
		{"legacy_tactical_format_timezone_unconfirmed", "09/14/2026 22:58:35", false, time.Time{}},
		{"surrounding_whitespace", "  2026-09-12T12:00:00Z  ", true, time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)},
		{"empty", "", true, time.Time{}},
		{"whitespace_only", "   ", true, time.Time{}},
		{"invalid_garbage", "not-a-time", false, time.Time{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseLastSeen(tc.value)
			if ok != tc.wantValid {
				t.Fatalf("valid: got %v, want %v (time=%v)", ok, tc.wantValid, got)
			}
			if tc.wantValid && !got.Equal(tc.wantTime) {
				t.Fatalf("time: got %v, want %v", got, tc.wantTime)
			}
			if !tc.wantValid && !got.IsZero() {
				t.Fatalf("expected zero time for invalid/unconfirmed input, got %v", got)
			}
		})
	}
}

// TestListAgentsToleratesOneInvalidLastSeen replaces the old
// TestInvalidLastSeenIsBadResponse: a single agent with an unparseable
// last_seen must no longer abort the whole tenant's agent list. This is a
// general defect (independently confirmed by code inspection, not tied to
// any specific incident): one malformed record used to make
// FindAgentByHostname fail for every hostname in the tenant. Whether this
// specific mechanism is what produced the T-007 Gate 4 missing_tactical
// result is unconfirmed — no raw Tactical response from that run survives
// to verify it (see docs/STATUS.md and D-022); it is guarded here on its
// own merits.
func TestListAgentsToleratesOneInvalidLastSeen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"agent_id":"a1","hostname":"GOOD-HOST","last_seen":"2026-09-12T12:00:00Z"},
			{"agent_id":"a2","hostname":"OTHER-HOST","last_seen":"09/14/2026 22:58:35"}
		]`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("expected ListAgents to tolerate one bad last_seen, got err: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	var good, other *Agent
	for i := range agents {
		switch agents[i].AgentID {
		case "a1":
			good = &agents[i]
		case "a2":
			other = &agents[i]
		}
	}
	if good == nil || !good.LastSeenValid || !good.LastSeen.Equal(time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected agent a1 with valid last_seen, got %+v", good)
	}
	if other == nil || other.LastSeenValid || !other.LastSeen.IsZero() {
		t.Fatalf("expected agent a2 recognized but LastSeenValid=false with zero time, got %+v", other)
	}
}

// TestFindAgentByHostnameToleratesOwnInvalidLastSeen covers the scenario
// originally suspected for T-007 Gate 4 (unconfirmed — see docs/STATUS.md):
// the target agent itself has an unparseable last_seen. It must still be
// located (foundTactical=true upstream in SupportService), just with
// LastSeenValid=false and a zero LastSeen.
func TestFindAgentByHostnameToleratesOwnInvalidLastSeen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"agent_id":"a1","hostname":"SDE-ARS-RCP-02","last_seen":"09/14/2026 22:58:35"}]`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	agent, err := client.FindAgentByHostname(context.Background(), "SDE-ARS-RCP-02")
	if err != nil {
		t.Fatalf("expected agent to be located despite invalid last_seen, got err: %v", err)
	}
	if agent.AgentID != "a1" {
		t.Fatalf("expected agent a1, got %+v", agent)
	}
	if agent.LastSeenValid {
		t.Fatalf("expected LastSeenValid=false for unparseable last_seen, got true")
	}
	if !agent.LastSeen.IsZero() {
		t.Fatalf("expected zero LastSeen for unparseable value, got %v", agent.LastSeen)
	}
}

func TestErrorsRedactAPIKeyBodyAndURL(t *testing.T) {
	const responseSecret = "raw-response-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, responseSecret)
	}))
	defer server.Close()
	cfg := testConfig(server.URL)
	cfg.APIKey = "private-api-key"
	client := mustClient(t, cfg)
	_, err := client.GetAgent(context.Background(), "sensitive-agent-id")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{cfg.APIKey, responseSecret, "sensitive-agent-id", server.URL} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %q", secret, err)
		}
	}
}

func TestCrossHostRedirectIsBlocked(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		fmt.Fprint(w, `[]`)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/secret?api_key=value", http.StatusFound)
	}))
	defer source.Close()
	client := mustClient(t, testConfig(source.URL))
	_, err := client.ListAgents(context.Background())
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got %v", err)
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetCalls.Load())
	}
	if strings.Contains(err.Error(), target.URL) || strings.Contains(err.Error(), "api_key") {
		t.Fatalf("redirect URL leaked: %v", err)
	}
}

func TestPublicOperationsAreReadOnlyGETs(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/agents/agent-1/" {
			fmt.Fprint(w, `{"agent_id":"agent-1","hostname":"HOST-01"}`)
			return
		}
		fmt.Fprint(w, `[{"agent_id":"agent-1","hostname":"HOST-01"}]`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	if _, err := client.ListAgents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetAgent(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.FindAgentByHostname(context.Background(), "host-01"); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /agents/", "GET /agents/agent-1/", "GET /agents/"}
	if strings.Join(methods, "|") != strings.Join(want, "|") {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
}
