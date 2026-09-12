package glpi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type glpiContract interface {
	FindComputerByHostname(context.Context, string) (Computer, error)
	GetComputer(context.Context, string) (Computer, error)
	CreateTicket(context.Context, TicketInput) (CreatedTicket, error)
}

var _ glpiContract = (*Client)(nil)

func testConfig(baseURL string) Config {
	return Config{
		BaseURL: baseURL, ClientID: "test-client", ClientSecret: "test-secret",
		Username: "test-user", Password: "test-password", Timeout: time.Second,
	}
}

func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func writeToken(w http.ResponseWriter, token string, expires int64) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"token_type":"Bearer","expires_in":%d,"access_token":%q}`, expires, token)
}

func TestNewValidatesConfig(t *testing.T) {
	valid := testConfig("https://glpi.invalid/base")
	if _, err := New(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	missing := []struct {
		name   string
		mutate func(*Config)
	}{
		{"base URL", func(c *Config) { c.BaseURL = "" }},
		{"client ID", func(c *Config) { c.ClientID = "" }},
		{"client secret", func(c *Config) { c.ClientSecret = "" }},
		{"username", func(c *Config) { c.Username = "" }},
		{"password", func(c *Config) { c.Password = "" }},
	}
	for _, tc := range missing {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			if _, err := New(cfg); !errors.Is(err, ErrConfig) {
				t.Fatalf("expected ErrConfig, got %v", err)
			}
		})
	}

	for _, invalid := range []string{"ftp://glpi.invalid", "https://", "https://user:pass@glpi.invalid", "https://glpi.invalid?q=secret", "https://glpi.invalid#fragment"} {
		t.Run(invalid, func(t *testing.T) {
			cfg := valid
			cfg.BaseURL = invalid
			if _, err := New(cfg); !errors.Is(err, ErrConfig) {
				t.Fatalf("expected ErrConfig, got %v", err)
			}
		})
	}
	cfg := valid
	cfg.Timeout = -time.Second
	if _, err := New(cfg); !errors.Is(err, ErrConfig) {
		t.Fatalf("negative timeout: expected ErrConfig, got %v", err)
	}
}

func TestFindComputerAuthenticatesFiltersAndMaps(t *testing.T) {
	recursive := true
	var tokenCalls, computerCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tokenPath:
			tokenCalls++
			if r.Method != http.MethodPost {
				t.Errorf("token method = %s", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Errorf("token content type = %q", got)
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			want := map[string]string{
				"grant_type": "password", "client_id": "test-client",
				"client_secret": "test-secret", "username": "test-user",
				"password": "test-password", "scope": "api",
			}
			for key, value := range want {
				if got := r.Form.Get(key); got != value {
					t.Errorf("form %s = %q, want %q", key, got, value)
				}
			}
			writeToken(w, "access-token", 3600)
		case computerPath:
			computerCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.URL.Query().Get("filter"); got != "name=='SDE-ARS-RCP-02'" {
				t.Errorf("filter = %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "2" {
				t.Errorf("limit = %q", got)
			}
			if r.Header.Get("GLPI-Entity") != "12" || r.Header.Get("GLPI-Profile") != "4" || r.Header.Get("GLPI-Entity-Recursive") != "true" || r.Header.Get("Accept-Language") != "pt_BR" {
				t.Errorf("context headers missing: %v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":42,"name":"sde-ars-rcp-02","serial":"SER-42","entity":{"id":7,"completename":"Root > Unit"}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.EntityID = "12"
	cfg.ProfileID = "4"
	cfg.EntityRecursive = &recursive
	cfg.AcceptLanguage = "pt_BR"
	client := mustClient(t, cfg)
	computer, err := client.FindComputerByHostname(context.Background(), "  Sde-Ars-Rcp-02 ")
	if err != nil {
		t.Fatal(err)
	}
	if computer.ID != "42" || computer.Name != "sde-ars-rcp-02" || computer.SerialNumber != "SER-42" {
		t.Fatalf("unexpected computer: %+v", computer)
	}
	if computer.Entity == nil || computer.Entity.ID != "7" || computer.Entity.Name != "Root > Unit" {
		t.Fatalf("unexpected entity: %+v", computer.Entity)
	}
	if tokenCalls != 1 || computerCalls != 1 {
		t.Fatalf("calls token=%d computer=%d", tokenCalls, computerCalls)
	}
}

func TestTokenCacheExpiryAndRenewal(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			call := tokenCalls.Add(1)
			writeToken(w, fmt.Sprintf("token-%d", call), 100)
			return
		}
		fmt.Fprint(w, `{"id":1,"name":"HOST"}`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	now := time.Unix(1_700_000_000, 0)
	client.now = func() time.Time { return now }

	for range 2 {
		if _, err := client.GetComputer(context.Background(), "1"); err != nil {
			t.Fatal(err)
		}
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("before expiry token calls = %d", got)
	}
	now = now.Add(91 * time.Second)
	if _, err := client.GetComputer(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	if got := tokenCalls.Load(); got != 2 {
		t.Fatalf("after safety-margin expiry token calls = %d", got)
	}
}

func TestConcurrentRequestsShareTokenAcquisition(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			tokenCalls.Add(1)
			time.Sleep(25 * time.Millisecond)
			writeToken(w, "shared-token", 3600)
			return
		}
		if r.Header.Get("Authorization") != "Bearer shared-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"id":1,"name":"HOST"}`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))

	const workers = 24
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			_, err := client.GetComputer(context.Background(), "1")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls = %d, want 1", got)
	}
}

func TestConcurrentRequestsShareTokenFailure(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tokenPath {
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		tokenCalls.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))

	const workers = 24
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			_, err := client.GetComputer(context.Background(), "1")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var shared error
	for err := range errs {
		if !errors.Is(err, ErrAuth) {
			t.Fatalf("expected ErrAuth, got %v", err)
		}
		if shared == nil {
			shared = err
		} else if err != shared {
			t.Fatalf("waiters received different error instances: %p != %p", err, shared)
		}
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("group token calls = %d, want 1", got)
	}

	if _, err := client.GetComputer(context.Background(), "1"); !errors.Is(err, ErrAuth) {
		t.Fatalf("later call: expected ErrAuth, got %v", err)
	}
	if got := tokenCalls.Load(); got != 2 {
		t.Fatalf("token calls after later retry = %d, want 2", got)
	}
}

func TestTokenWaiterCancellationIsIndividual(t *testing.T) {
	var tokenCalls atomic.Int32
	tokenStarted := make(chan struct{})
	releaseToken := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseToken) }) }
	defer release()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		close(tokenStarted)
		<-releaseToken
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))

	ownerErr := make(chan error, 1)
	go func() {
		_, err := client.GetComputer(context.Background(), "1")
		ownerErr <- err
	}()
	<-tokenStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GetComputer(ctx, "1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter: expected context.Canceled, got %v", err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls while waiter canceled = %d, want 1", got)
	}

	release()
	if err := <-ownerErr; !errors.Is(err, ErrAuth) {
		t.Fatalf("owner: expected ErrAuth, got %v", err)
	}
}

func TestGETRenewsOnceAfter401(t *testing.T) {
	var tokenCalls, getCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			tokenCalls++
			writeToken(w, fmt.Sprintf("token-%d", tokenCalls), 3600)
			return
		}
		getCalls++
		if r.Header.Get("Authorization") == "Bearer token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"id":1,"name":"HOST"}`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	if _, err := client.GetComputer(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 2 || getCalls != 2 {
		t.Fatalf("calls token=%d get=%d, want 2/2", tokenCalls, getCalls)
	}
}

func TestCreateTicketNeverRetries(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var tokenCalls, postCalls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tokenPath {
					tokenCalls++
					writeToken(w, "token", 3600)
					return
				}
				postCalls++
				w.WriteHeader(status)
			}))
			defer server.Close()
			client := mustClient(t, testConfig(server.URL))
			_, err := client.CreateTicket(context.Background(), TicketInput{Name: "Title", Content: "Body"})
			if err == nil {
				t.Fatal("expected error")
			}
			if tokenCalls != 1 || postCalls != 1 {
				t.Fatalf("calls token=%d post=%d, want 1/1", tokenCalls, postCalls)
			}
		})
	}
}

func TestCreateTicketDoesNotFollowRedirect(t *testing.T) {
	var postCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			writeToken(w, "token", 3600)
			return
		}
		postCalls++
		w.Header().Set("Location", ticketPath+"/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	_, err := client.CreateTicket(context.Background(), TicketInput{Name: "Title", Content: "Body"})
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got %v", err)
	}
	if postCalls != 1 {
		t.Fatalf("ticket POST calls = %d, want 1", postCalls)
	}
}

func TestFindComputerMatchCardinality(t *testing.T) {
	tests := []struct {
		name string
		body string
		kind error
	}{
		{"absent", `[]`, ErrNotFound},
		{"non-exact", `[{"id":1,"name":"SDE-ARS-RCP-020"}]`, ErrNotFound},
		{"ambiguous", `[{"id":1,"name":"SDE-ARS-RCP-02"},{"id":2,"name":" sde-ars-rcp-02 "}]`, ErrAmbiguous},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tokenPath {
					writeToken(w, "token", 3600)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := mustClient(t, testConfig(server.URL))
			_, err := client.FindComputerByHostname(context.Background(), "SDE-ARS-RCP-02")
			if !errors.Is(err, tc.kind) {
				t.Fatalf("expected %v, got %v", tc.kind, err)
			}
		})
	}
}

func TestFindComputerEscapesRSQLValue(t *testing.T) {
	const hostname = `A'B\C;D`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			writeToken(w, "token", 3600)
			return
		}
		if got := r.URL.Query().Get("filter"); got != `name=='A\'B\\C;D'` {
			t.Errorf("filter = %q", got)
		}
		fmt.Fprint(w, `[{"id":1,"name":"A'B\\C;D"}]`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	if _, err := client.FindComputerByHostname(context.Background(), hostname); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTicketUsesDirectV23Schema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			writeToken(w, "token", 3600)
			return
		}
		if r.URL.Path != ticketPath || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if _, legacy := body["input"]; legacy {
			t.Error("legacy input envelope sent")
		}
		if body["name"] != "Printer offline" || body["content"] != "HOST / Computer 42" || body["external_id"] != "request-7" {
			t.Errorf("unexpected ticket body: %#v", body)
		}
		if category, ok := body["category"].(map[string]any); !ok || category["id"] != float64(9) {
			t.Errorf("category = %#v", body["category"])
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":77,"href":"/api.php/v2.3/Assistance/Ticket/77"}`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	created, err := client.CreateTicket(context.Background(), TicketInput{
		Name: "Printer offline", Content: "HOST / Computer 42", Urgency: 3,
		Category: &IDReference{ID: 9}, ExternalID: "request-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "77" || created.Href != "/api.php/v2.3/Assistance/Ticket/77" {
		t.Fatalf("unexpected response: %+v", created)
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
		t.Run(strconvItoa(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tokenPath {
					writeToken(w, "token", 3600)
					return
				}
				if tc.status == http.StatusTooManyRequests || tc.status == http.StatusServiceUnavailable {
					w.Header().Set("Retry-After", "17")
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			client := mustClient(t, testConfig(server.URL))
			_, err := client.GetComputer(context.Background(), "1")
			if !errors.Is(err, tc.kind) {
				t.Fatalf("expected %v, got %v", tc.kind, err)
			}
			if tc.status == http.StatusTooManyRequests || tc.status == http.StatusServiceUnavailable {
				var apiErr *Error
				if !errors.As(err, &apiErr) || apiErr.RetryAfter != 17*time.Second {
					t.Fatalf("RetryAfter = %v", apiErr)
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
		if r.URL.Path == tokenPath {
			writeToken(w, "token", 3600)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.Timeout = 20 * time.Millisecond
	client := mustClient(t, cfg)
	if _, err := client.GetComputer(context.Background(), "1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("HTTP client timeout: expected ErrUnavailable, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client = mustClient(t, testConfig(server.URL))
	if _, err := client.GetComputer(ctx, "1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation: expected context.Canceled, got %v", err)
	}

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer deadlineCancel()
	if _, err := client.GetComputer(deadlineCtx, "1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline: expected context.DeadlineExceeded, got %v", err)
	}
}

func TestMalformedAndOversizedResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed", `{"id":`},
		{"oversized", strings.Repeat("x", maxBodyBytes+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tokenPath {
					writeToken(w, "token", 3600)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := mustClient(t, testConfig(server.URL))
			if _, err := client.GetComputer(context.Background(), "1"); !errors.Is(err, ErrBadResponse) {
				t.Fatalf("expected ErrBadResponse, got %v", err)
			}
		})
	}
}

func TestReadLimitedBoundary(t *testing.T) {
	body, err := readLimited(strings.NewReader(strings.Repeat("x", maxBodyBytes)))
	if err != nil || len(body) != maxBodyBytes {
		t.Fatalf("exact limit: len=%d err=%v", len(body), err)
	}
	if _, err := readLimited(strings.NewReader(strings.Repeat("x", maxBodyBytes+1))); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("over limit: expected ErrBadResponse, got %v", err)
	}
}

func TestMalformedTokenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token_type":"Bearer","expires_in":0,"access_token":""}`)
	}))
	defer server.Close()
	client := mustClient(t, testConfig(server.URL))
	if _, err := client.GetComputer(context.Background(), "1"); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got %v", err)
	}
}

func TestErrorsRedactSecretsBodiesAndURLs(t *testing.T) {
	const bodySecret = "raw-response-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			writeToken(w, "bearer-secret", 3600)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, bodySecret)
	}))
	defer server.Close()
	cfg := testConfig(server.URL)
	cfg.ClientSecret = "client-secret-value"
	cfg.Password = "password-value"
	client := mustClient(t, cfg)
	_, err := client.FindComputerByHostname(context.Background(), "sensitive-hostname")
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	for _, secret := range []string{cfg.ClientSecret, cfg.Password, "bearer-secret", bodySecret, "sensitive-hostname", server.URL} {
		if strings.Contains(text, secret) {
			t.Fatalf("error leaked %q: %q", secret, text)
		}
	}
}

func TestCrossHostRedirectIsBlocked(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		fmt.Fprint(w, `{"id":1,"name":"HOST"}`)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			writeToken(w, "token", 3600)
			return
		}
		http.Redirect(w, r, target.URL+"/secret?credential=value", http.StatusFound)
	}))
	defer source.Close()
	client := mustClient(t, testConfig(source.URL))
	_, err := client.GetComputer(context.Background(), "1")
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got %v", err)
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetCalls.Load())
	}
	if strings.Contains(err.Error(), target.URL) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("redirect URL leaked: %v", err)
	}
}

func strconvItoa(value int) string {
	return fmt.Sprintf("%d", value)
}
