// Package glpi implements the minimal GLPI 11 High-Level REST API v2.3 client
// used by WACalls. It does not support the legacy apirest.php API.
package glpi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tokenPath           = "/api.php/token"
	computerPath        = "/api.php/v2.3/Assets/Computer"
	ticketPath          = "/api.php/v2.3/Assistance/Ticket"
	computerSearchLimit = 2
	maxBodyBytes        = 2 << 20
)

var errCrossHostRedirect = errors.New("cross-host redirect blocked")

// Client is safe for concurrent use.
type Client struct {
	baseURL *url.URL
	http    *http.Client

	clientID     string
	clientSecret string
	username     string
	password     string

	entityID        string
	profileID       string
	entityRecursive *bool
	acceptLanguage  string

	now func() time.Time

	tokenMu         sync.Mutex
	tokenWait       *tokenFlight
	accessToken     string
	tokenValidUntil time.Time
}

type tokenFlight struct {
	done chan struct{}
	err  error
}

// New validates cfg and constructs a client with secure TLS defaults and a
// same-origin redirect policy.
func New(cfg Config) (*Client, error) {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.Username = strings.TrimSpace(cfg.Username)

	for _, required := range []struct {
		name  string
		value string
	}{
		{"WACALLS_GLPI_BASE_URL", cfg.BaseURL},
		{"WACALLS_GLPI_CLIENT_ID", cfg.ClientID},
		{"WACALLS_GLPI_CLIENT_SECRET", cfg.ClientSecret},
		{"WACALLS_GLPI_USERNAME", cfg.Username},
		{"WACALLS_GLPI_PASSWORD", cfg.Password},
	} {
		if strings.TrimSpace(required.value) == "" {
			return nil, configError(required.name)
		}
	}

	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, configError("WACALLS_GLPI_BASE_URL")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return nil, configError("WACALLS_GLPI_TIMEOUT_SECONDS")
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

	return &Client{
		baseURL:         baseURL,
		http:            httpClient,
		clientID:        cfg.ClientID,
		clientSecret:    cfg.ClientSecret,
		username:        cfg.Username,
		password:        cfg.Password,
		entityID:        strings.TrimSpace(cfg.EntityID),
		profileID:       strings.TrimSpace(cfg.ProfileID),
		entityRecursive: cfg.EntityRecursive,
		acceptLanguage:  strings.TrimSpace(cfg.AcceptLanguage),
		now:             time.Now,
	}, nil
}

func configError(field string) error {
	return &Error{Op: "config", Kind: ErrConfig, Field: field}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// FindComputerByHostname searches by the API's name field, then independently
// enforces exact equality after the T-003 normalization rule.
func (c *Client) FindComputerByHostname(ctx context.Context, hostname string) (Computer, error) {
	normalized := normalizeHostname(hostname)
	if normalized == "" {
		return Computer{}, &Error{Op: "find computer", Kind: ErrBadRequest}
	}
	query := make(url.Values, 2)
	query.Set("filter", "name=="+quoteRSQL(normalized))
	query.Set("limit", strconv.Itoa(computerSearchLimit))

	body, err := c.get(ctx, computerPath, query)
	if err != nil {
		return Computer{}, err
	}
	var wire []computerDTO
	if err := decodeJSON(body, &wire); err != nil {
		return Computer{}, &Error{Op: "find computer", Kind: ErrBadResponse}
	}
	matches := make([]Computer, 0, len(wire))
	for _, item := range wire {
		if normalizeHostname(item.Name) != normalized {
			continue
		}
		mapped, err := mapComputer(item)
		if err != nil {
			return Computer{}, &Error{Op: "find computer", Kind: ErrBadResponse}
		}
		matches = append(matches, mapped)
	}
	switch len(matches) {
	case 0:
		return Computer{}, &Error{Op: "find computer", Kind: ErrNotFound}
	case 1:
		return matches[0], nil
	default:
		return Computer{}, &Error{Op: "find computer", Kind: ErrAmbiguous}
	}
}

// GetComputer returns one Computer by its permanent GLPI ID.
func (c *Client) GetComputer(ctx context.Context, id string) (Computer, error) {
	id = strings.TrimSpace(id)
	numericID, parseErr := strconv.ParseInt(id, 10, 64)
	if parseErr != nil || numericID <= 0 {
		return Computer{}, &Error{Op: "get computer", Kind: ErrBadRequest}
	}
	body, err := c.get(ctx, computerPath+"/"+id, nil)
	if err != nil {
		return Computer{}, err
	}
	var wire computerDTO
	if err := decodeJSON(body, &wire); err != nil {
		return Computer{}, &Error{Op: "get computer", Kind: ErrBadResponse}
	}
	computer, err := mapComputer(wire)
	if err != nil {
		return Computer{}, &Error{Op: "get computer", Kind: ErrBadResponse}
	}
	return computer, nil
}

// CreateTicket performs exactly one ticket POST. It never retries after an HTTP
// response or an ambiguous transport failure.
func (c *Client) CreateTicket(ctx context.Context, in TicketInput) (CreatedTicket, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Content) == "" ||
		invalidIDReference(in.Location) || invalidIDReference(in.Category) ||
		invalidIDReference(in.RequestType) || invalidIDReference(in.UserRecipient) ||
		(in.Entity != nil && strings.TrimSpace(in.Entity.CompleteName) == "") {
		return CreatedTicket{}, &Error{Op: "create ticket", Kind: ErrBadRequest}
	}
	body, err := c.authorized(ctx, http.MethodPost, ticketPath, nil, in, false, http.StatusCreated)
	if err != nil {
		return CreatedTicket{}, err
	}
	var wire createdTicketDTO
	if err := decodeJSON(body, &wire); err != nil {
		return CreatedTicket{}, &Error{Op: "create ticket", Kind: ErrBadResponse}
	}
	id, err := requiredNumber(wire.ID)
	if err != nil || strings.TrimSpace(wire.Href) == "" {
		return CreatedTicket{}, &Error{Op: "create ticket", Kind: ErrBadResponse}
	}
	return CreatedTicket{ID: id, Href: wire.Href}, nil
}

// GetTicket returns one Ticket by its permanent GLPI ID.
func (c *Client) GetTicket(ctx context.Context, id string) (Ticket, error) {
	id = strings.TrimSpace(id)
	numericID, parseErr := strconv.ParseInt(id, 10, 64)
	if parseErr != nil || numericID <= 0 {
		return Ticket{}, &Error{Op: "get ticket", Kind: ErrBadRequest}
	}
	body, err := c.get(ctx, ticketPath+"/"+id, nil)
	if err != nil {
		return Ticket{}, err
	}
	var wire ticketDTO
	if err := decodeJSON(body, &wire); err != nil {
		return Ticket{}, &Error{Op: "get ticket", Kind: ErrBadResponse}
	}
	ticket, err := mapTicket(wire, id)
	if err != nil {
		return Ticket{}, &Error{Op: "get ticket", Kind: ErrBadResponse}
	}
	return ticket, nil
}

func invalidIDReference(reference *IDReference) bool {
	return reference != nil && reference.ID <= 0
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.authorized(ctx, http.MethodGet, path, query, nil, true, http.StatusOK, http.StatusPartialContent)
}

func (c *Client) authorized(ctx context.Context, method, path string, query url.Values, input any, retryGET401 bool, accepted ...int) ([]byte, error) {
	var payload []byte
	if input != nil {
		var err error
		payload, err = json.Marshal(input)
		if err != nil {
			return nil, &Error{Op: operation(method, path), Kind: ErrBadRequest}
		}
	}

	for attempt := 0; ; attempt++ {
		token, err := c.token(ctx)
		if err != nil {
			return nil, err
		}
		endpoint := c.endpoint(path, query)
		req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, &Error{Op: operation(method, path), Kind: ErrBadRequest}
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		c.setContextHeaders(req.Header)

		status, header, body, err := c.perform(ctx, operation(method, path), req)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized && retryGET401 && method == http.MethodGet && attempt == 0 {
			c.invalidateToken(token)
			continue
		}
		if !containsStatus(accepted, status) {
			return nil, statusError(operation(method, path), status, header, c.now())
		}
		return body, nil
	}
}

func (c *Client) token(ctx context.Context) (string, error) {
	for {
		c.tokenMu.Lock()
		if c.accessToken != "" && c.now().Before(c.tokenValidUntil) {
			token := c.accessToken
			c.tokenMu.Unlock()
			return token, nil
		}
		if flight := c.tokenWait; flight != nil {
			c.tokenMu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-flight.done:
				if ctxErr := ctx.Err(); ctxErr != nil {
					return "", ctxErr
				}
				if flight.err != nil {
					return "", flight.err
				}
				continue
			}
		}
		flight := &tokenFlight{done: make(chan struct{})}
		c.tokenWait = flight
		c.tokenMu.Unlock()

		token, validUntil, err := c.requestToken(ctx)

		c.tokenMu.Lock()
		if err == nil {
			c.accessToken = token
			c.tokenValidUntil = validUntil
		}
		flight.err = err
		c.tokenWait = nil
		close(flight.done)
		c.tokenMu.Unlock()
		return token, err
	}
}

func (c *Client) invalidateToken(used string) {
	c.tokenMu.Lock()
	if c.accessToken == used {
		c.accessToken = ""
		c.tokenValidUntil = time.Time{}
	}
	c.tokenMu.Unlock()
}

func (c *Client) requestToken(ctx context.Context) (string, time.Time, error) {
	form := make(url.Values, 6)
	form.Set("grant_type", "password")
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("username", c.username)
	form.Set("password", c.password)
	form.Set("scope", "api")

	endpoint := c.endpoint(tokenPath, nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, &Error{Op: "token", Kind: ErrBadRequest}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, header, body, err := c.perform(ctx, "token", req)
	if err != nil {
		return "", time.Time{}, err
	}
	if status != http.StatusOK {
		if status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden {
			return "", time.Time{}, &Error{Op: "token", Kind: ErrAuth, StatusCode: status}
		}
		return "", time.Time{}, statusError("token", status, header, c.now())
	}
	var response tokenDTO
	if err := decodeJSON(body, &response); err != nil {
		return "", time.Time{}, &Error{Op: "token", Kind: ErrBadResponse}
	}
	if response.AccessToken == "" || !strings.EqualFold(response.TokenType, "Bearer") || response.ExpiresIn <= 0 || response.ExpiresIn > 365*24*60*60 {
		return "", time.Time{}, &Error{Op: "token", Kind: ErrBadResponse}
	}
	lifetime := time.Duration(response.ExpiresIn) * time.Second
	margin := 30 * time.Second
	if tenth := lifetime / 10; tenth < margin {
		margin = tenth
	}
	return response.AccessToken, c.now().Add(lifetime - margin), nil
}

func (c *Client) setContextHeaders(header http.Header) {
	if c.entityID != "" {
		header.Set("GLPI-Entity", c.entityID)
	}
	if c.profileID != "" {
		header.Set("GLPI-Profile", c.profileID)
	}
	if c.entityRecursive != nil {
		header.Set("GLPI-Entity-Recursive", strconv.FormatBool(*c.entityRecursive))
	}
	if c.acceptLanguage != "" {
		header.Set("Accept-Language", c.acceptLanguage)
	}
}

func (c *Client) endpoint(path string, query url.Values) *url.URL {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	endpoint.RawPath = ""
	endpoint.RawQuery = query.Encode()
	return &endpoint
}

func (c *Client) perform(ctx context.Context, op string, req *http.Request) (int, http.Header, []byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, nil, nil, ctxErr
		}
		kind := ErrUnavailable
		if errors.Is(err, errCrossHostRedirect) {
			kind = ErrBadResponse
		}
		return 0, nil, nil, &Error{Op: op, Kind: kind}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, resp.Header, nil, nil
	}
	body, err := readLimited(resp.Body)
	if err != nil {
		return 0, nil, nil, &Error{Op: op, Kind: ErrBadResponse}
	}
	return resp.StatusCode, resp.Header, body, nil
}

func readLimited(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
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

func containsStatus(statuses []int, status int) bool {
	for _, accepted := range statuses {
		if status == accepted {
			return true
		}
	}
	return false
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

func operation(method, path string) string {
	switch {
	case method == http.MethodGet && path == computerPath:
		return "find computer"
	case method == http.MethodGet && strings.HasPrefix(path, computerPath+"/"):
		return "get computer"
	case method == http.MethodGet && strings.HasPrefix(path, ticketPath+"/"):
		return "get ticket"
	case method == http.MethodPost && path == ticketPath:
		return "create ticket"
	default:
		return strings.ToLower(method)
	}
}

func quoteRSQL(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}

func normalizeHostname(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

type tokenDTO struct {
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	AccessToken string `json:"access_token"`
}

type computerDTO struct {
	ID     json.Number `json:"id"`
	Name   string      `json:"name"`
	Serial string      `json:"serial"`
	Entity *struct {
		ID           json.Number `json:"id"`
		CompleteName string      `json:"completename"`
	} `json:"entity"`
}

type createdTicketDTO struct {
	ID   json.Number `json:"id"`
	Href string      `json:"href"`
}

type ticketDTO struct {
	ID         json.Number `json:"id"`
	Href       string      `json:"href"`
	ExternalID string      `json:"external_id"`
	Links      []struct {
		Rel  string `json:"rel"`
		Href string `json:"href"`
	} `json:"links"`
}

func mapTicket(wire ticketDTO, fallbackID string) (Ticket, error) {
	id := ""
	if wire.ID != "" {
		var err error
		id, err = requiredNumber(wire.ID)
		if err != nil {
			return Ticket{}, err
		}
	} else if fallbackID != "" {
		id = fallbackID
	} else {
		return Ticket{}, fmt.Errorf("missing ticket id")
	}

	href := strings.TrimSpace(wire.Href)
	if href == "" {
		for _, l := range wire.Links {
			if strings.EqualFold(l.Rel, "self") && strings.TrimSpace(l.Href) != "" {
				href = strings.TrimSpace(l.Href)
				break
			}
		}
	}
	if href == "" {
		href = ticketPath + "/" + id
	}

	return Ticket{
		ID:         id,
		Href:       href,
		ExternalID: strings.TrimSpace(wire.ExternalID),
	}, nil
}

func mapComputer(wire computerDTO) (Computer, error) {
	id, err := requiredNumber(wire.ID)
	if err != nil {
		return Computer{}, err
	}
	computer := Computer{ID: id, Name: wire.Name, SerialNumber: wire.Serial}
	if wire.Entity != nil {
		entityID := ""
		if wire.Entity.ID != "" {
			entityID = wire.Entity.ID.String()
		}
		computer.Entity = &Reference{ID: entityID, Name: wire.Entity.CompleteName}
	}
	return computer, nil
}

func requiredNumber(number json.Number) (string, error) {
	if number == "" {
		return "", fmt.Errorf("missing id")
	}
	if _, err := number.Int64(); err != nil {
		return "", fmt.Errorf("invalid id: %w", err)
	}
	return number.String(), nil
}
