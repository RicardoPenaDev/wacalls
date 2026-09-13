package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBuildGLPITicketWebURL_ConstrucaoCorretaEFormatos(t *testing.T) {
	cases := []struct {
		name        string
		baseURL     string
		ticketID    string
		expectedURL string
	}{
		{
			name:        "base sem barra final",
			baseURL:     "https://glpi.example.com",
			ticketID:    "42",
			expectedURL: "https://glpi.example.com/front/ticket.form.php?id=42",
		},
		{
			name:        "base com barra final",
			baseURL:     "https://glpi.example.com/",
			ticketID:    "42",
			expectedURL: "https://glpi.example.com/front/ticket.form.php?id=42",
		},
		{
			name:        "base com múltiplas barras finais",
			baseURL:     "https://glpi.example.com///",
			ticketID:    "1001",
			expectedURL: "https://glpi.example.com/front/ticket.form.php?id=1001",
		},
		{
			name:        "id pequeno",
			baseURL:     "https://glpi.example.com",
			ticketID:    "1",
			expectedURL: "https://glpi.example.com/front/ticket.form.php?id=1",
		},
		{
			name:        "id grande",
			baseURL:     "https://glpi.example.com",
			ticketID:    "999999999",
			expectedURL: "https://glpi.example.com/front/ticket.form.php?id=999999999",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildGLPITicketWebURL(tc.baseURL, tc.ticketID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expectedURL {
				t.Fatalf("expected %q, got %q", tc.expectedURL, got)
			}
		})
	}
}

func TestBuildGLPITicketWebURL_RejeitaHTTPInclusiveLocalhost(t *testing.T) {
	httpTargets := []string{
		"http://glpi.example.com",
		"http://glpi.example.com:80",
		"http://glpi.example.com:8080",
		"http://localhost",
		"http://localhost:80",
		"http://localhost:8080",
		"http://127.0.0.1",
		"http://127.0.0.1:80",
		"http://127.0.0.1:8080",
		"http://[::1]",
		"http://[::1]:80",
		"http://[::1]:8080",
	}

	for _, target := range httpTargets {
		t.Run("http_rejeitado_"+target, func(t *testing.T) {
			_, err := buildGLPITicketWebURL(target, "10")
			if err == nil {
				t.Fatalf("expected error for HTTP target %q, got nil", target)
			}
			if !strings.Contains(err.Error(), "must use https") {
				t.Fatalf("unexpected error message for %q: %v", target, err)
			}
		})
	}
}

func TestBuildGLPITicketWebURL_AusenciaComportamentoInsecure(t *testing.T) {
	// Mesmo com ambiente contendo WACALLS_GLPI_INSECURE_TLS=true, o código deve ignorar completamente
	t.Setenv("WACALLS_GLPI_INSECURE_TLS", "true")

	// HTTP continua categoricamente rejeitado
	_, err := buildGLPITicketWebURL("http://127.0.0.1:8080", "10")
	if err == nil {
		t.Fatal("expected error for http URL even if WACALLS_GLPI_INSECURE_TLS is set in env, got nil")
	}

	_, err = validateAndNormalizeGLPIWebBaseURL("http://localhost:8080", "https://localhost:8080")
	if err == nil {
		t.Fatal("expected error for validateAndNormalizeGLPIWebBaseURL with http, got nil")
	}
}

func TestBuildGLPITicketWebURL_ParametroIdExclusivo(t *testing.T) {
	rawURL, err := buildGLPITicketWebURL("https://glpi.example.com", "777")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse generated URL: %v", err)
	}

	if parsed.Scheme != "https" {
		t.Fatalf("expected scheme https, got %q", parsed.Scheme)
	}
	if parsed.Path != "/front/ticket.form.php" {
		t.Fatalf("expected path /front/ticket.form.php, got %q", parsed.Path)
	}

	q := parsed.Query()
	if len(q) != 1 {
		t.Fatalf("expected exactly 1 query parameter, got %d: %v", len(q), q)
	}
	if q.Get("id") != "777" {
		t.Fatalf("expected id=777, got %q", q.Get("id"))
	}
	if parsed.RawQuery != "id=777" {
		t.Fatalf("expected raw query 'id=777', got %q", parsed.RawQuery)
	}
	if parsed.Fragment != "" {
		t.Fatalf("expected no fragment, got %q", parsed.Fragment)
	}
}

func TestBuildGLPITicketWebURL_RejeitaUserinfoQueryFragment(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{"com userinfo", "https://user:password@glpi.example.com"},
		{"com username apenas", "https://admin@glpi.example.com"},
		{"com query param", "https://glpi.example.com?foo=bar"},
		{"com fragment", "https://glpi.example.com#anchor"},
		{"com userinfo e query", "https://u:p@glpi.example.com?a=1#sec"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildGLPITicketWebURL(tt.baseURL, "10")
			if err == nil {
				t.Fatalf("expected error for baseURL %q, got nil", tt.baseURL)
			}
		})
	}
}

func TestValidateAndNormalizeGLPIWebBaseURL_EquivalenciaPortaEOrigem(t *testing.T) {
	tests := []struct {
		name      string
		webRaw    string
		apiRaw    string
		expectErr bool
		errMsg    string
		expected  string
	}{
		{
			name:      "https sem porta e https :443 sao equivalentes (web sem porta, api :443)",
			webRaw:    "https://glpi.example.com",
			apiRaw:    "https://glpi.example.com:443/api.php/v2.3",
			expectErr: false,
			expected:  "https://glpi.example.com",
		},
		{
			name:      "https sem porta e https :443 sao equivalentes (web :443, api sem porta)",
			webRaw:    "https://glpi.example.com:443",
			apiRaw:    "https://glpi.example.com/api.php/v2.3",
			expectErr: false,
			expected:  "https://glpi.example.com:443",
		},
		{
			name:      "ambos com :443 explicito",
			webRaw:    "https://glpi.example.com:443/",
			apiRaw:    "https://glpi.example.com:443/api.php/v2.3",
			expectErr: false,
			expected:  "https://glpi.example.com:443",
		},
		{
			name:      "hostname case-insensitive",
			webRaw:    "https://GLPI.Example.Com/",
			apiRaw:    "https://glpi.example.com/api.php/v2.3",
			expectErr: false,
			expected:  "https://GLPI.Example.Com",
		},
		{
			name:      "portas nao padrao iguais",
			webRaw:    "https://glpi.example.com:8443",
			apiRaw:    "https://glpi.example.com:8443/api.php/v2.3",
			expectErr: false,
			expected:  "https://glpi.example.com:8443",
		},
		{
			name:      "portas nao padrao diferentes",
			webRaw:    "https://glpi.example.com:8443",
			apiRaw:    "https://glpi.example.com:9443/api.php/v2.3",
			expectErr: true,
			errMsg:    "port",
		},
		{
			name:      "porta nao padrao vs porta padrao",
			webRaw:    "https://glpi.example.com:8443",
			apiRaw:    "https://glpi.example.com/api.php/v2.3",
			expectErr: true,
			errMsg:    "port",
		},
		{
			name:      "host diferente rejeitado",
			webRaw:    "https://glpi-web.example.com",
			apiRaw:    "https://glpi-api.example.com/api.php/v2.3",
			expectErr: true,
			errMsg:    "host",
		},
		{
			name:      "tentativa de bypass por prefixo de hostname",
			webRaw:    "https://glpi.example.com.evil.com",
			apiRaw:    "https://glpi.example.com/api.php/v2.3",
			expectErr: true,
			errMsg:    "host",
		},
		{
			name:      "IPv6 equivalente com porta 443 implicita e explicita",
			webRaw:    "https://[2001:db8::1]",
			apiRaw:    "https://[2001:db8::1]:443/api.php/v2.3",
			expectErr: false,
			expected:  "https://[2001:db8::1]",
		},
		{
			name:      "IPv6 com mesma porta nao padrao",
			webRaw:    "https://[2001:db8::1]:8443",
			apiRaw:    "https://[2001:db8::1]:8443/api.php/v2.3",
			expectErr: false,
			expected:  "https://[2001:db8::1]:8443",
		},
		{
			name:      "IPv6 com portas divergentes",
			webRaw:    "https://[2001:db8::1]:8443",
			apiRaw:    "https://[2001:db8::1]:9443/api.php/v2.3",
			expectErr: true,
			errMsg:    "port",
		},
		{
			name:      "IPv6 com hosts divergentes",
			webRaw:    "https://[2001:db8::1]:8443",
			apiRaw:    "https://[2001:db8::2]:8443/api.php/v2.3",
			expectErr: true,
			errMsg:    "host",
		},
		{
			name:      "rejeicao de HTTP em webRaw",
			webRaw:    "http://glpi.example.com",
			apiRaw:    "http://glpi.example.com/api.php/v2.3",
			expectErr: true,
			errMsg:    "must use https",
		},
		{
			name:      "esquema divergente (web https vs api http)",
			webRaw:    "https://glpi.example.com",
			apiRaw:    "http://glpi.example.com/api.php/v2.3",
			expectErr: true,
			errMsg:    "scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			norm, err := validateAndNormalizeGLPIWebBaseURL(tt.webRaw, tt.apiRaw)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error for %s, got nil", tt.name)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for %s: %v", tt.name, err)
				}
				if norm != tt.expected {
					t.Fatalf("expected %q, got %q", tt.expected, norm)
				}
			}
		})
	}
}

func TestBuildGLPITicketWebURL_IDsInvalidos(t *testing.T) {
	invalidIDs := []string{
		"0",
		"-1",
		"-42",
		"01",
		"007",
		"abc",
		"1a",
		"1;DROP TABLE tickets",
		"1?redirect=evil.com",
		"1#section",
		"javascript:alert(1)",
		"data:text/html,evil",
		"",
		"   ",
		"1.5",
		"+1",
	}

	for _, badID := range invalidIDs {
		t.Run("id_"+badID, func(t *testing.T) {
			_, err := buildGLPITicketWebURL("https://glpi.example.com", badID)
			if err == nil {
				t.Fatalf("expected error for invalid ticket ID %q, got nil", badID)
			}
		})
	}
}

func TestSupportRequestPublicDTO_OmitAPIHrefEExpoeWebURL(t *testing.T) {
	ticketID := "1001"
	apiHref := "https://glpi.internal/api.php/v2.3/Assistance/Ticket/1001"
	req := &SupportRequest{
		ID:             "req-test-1",
		SessionID:      "sess-1",
		ChatJID:        "5511999999999@s.whatsapp.net",
		Title:          "Teste de Suporte",
		Description:    "Descrição de teste",
		RequesterName:  "Ricardo",
		SyncState:      StateSynced,
		GLPITicketID:   &ticketID,
		GLPITicketHref: &apiHref,
	}

	// Caso 1: Com webBaseURL configurada -> gera webUrl e omite apiHref
	dto := toPublicSupportRequestDTO(req, "https://glpi.example.com")
	if dto.WebURL == nil || *dto.WebURL != "https://glpi.example.com/front/ticket.form.php?id=1001" {
		t.Fatalf("expected WebURL 'https://glpi.example.com/front/ticket.form.php?id=1001', got %v", dto.WebURL)
	}

	rawJSON, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("failed to marshal DTO: %v", err)
	}
	jsonStr := string(rawJSON)

	// Validações estritas de segurança do JSON público:
	if !strings.Contains(jsonStr, `"webUrl":"https://glpi.example.com/front/ticket.form.php?id=1001"`) {
		t.Fatalf("expected json to contain webUrl, got %s", jsonStr)
	}
	if strings.Contains(jsonStr, "glpiTicketHref") {
		t.Fatalf("glpiTicketHref MUST NOT appear in public DTO JSON: %s", jsonStr)
	}
	if strings.Contains(jsonStr, apiHref) {
		t.Fatalf("internal API href MUST NOT appear anywhere in public JSON: %s", jsonStr)
	}

	// Caso 2: Sem webBaseURL -> webUrl é nil e não aparece no JSON
	dtoNoWeb := toPublicSupportRequestDTO(req, "")
	if dtoNoWeb.WebURL != nil {
		t.Fatalf("expected nil WebURL when webBaseURL is empty, got %v", dtoNoWeb.WebURL)
	}
	rawNoWebJSON, err := json.Marshal(dtoNoWeb)
	if err != nil {
		t.Fatalf("failed to marshal DTO: %v", err)
	}
	if strings.Contains(string(rawNoWebJSON), "webUrl") {
		t.Fatalf("webUrl should be omitted when nil: %s", string(rawNoWebJSON))
	}
	if strings.Contains(string(rawNoWebJSON), "glpiTicketHref") {
		t.Fatalf("glpiTicketHref MUST NOT appear in public DTO JSON: %s", string(rawNoWebJSON))
	}
}

func TestSupportConfig_ConfiguracaoAusente(t *testing.T) {
	t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
	t.Setenv("WACALLS_GLPI_BASE_URL", "https://glpi.example.com")
	t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
	t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
	t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
	t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
	t.Setenv("WACALLS_GLPI_WEB_BASE_URL", "") // Ausente

	cfg, err := loadSupportConfig()
	if err != nil {
		t.Fatalf("loadSupportConfig failed with empty WACALLS_GLPI_WEB_BASE_URL: %v", err)
	}
	if cfg.GLPIWebBaseURL != "" {
		t.Fatalf("expected empty GLPIWebBaseURL, got %q", cfg.GLPIWebBaseURL)
	}
}

func TestSupportAPI_WebURLNoEnvelopeEOmiteHref(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	setup.srv.supportCfg = &supportConfig{
		Enabled:        true,
		GLPIWebBaseURL: "https://glpi.example.com",
	}

	ticketID := "888"
	apiHref := "https://glpi.internal/api.php/v2.3/Assistance/Ticket/888"
	ctx := context.Background()
	in := sampleCreateInput(setup.user1ID, "idemp-test-weburl-1", strings.Repeat("a", 64))
	in.SessionID = setup.sess1ID
	in.ChatJID = setup.chat1JID
	req, err := setup.store.CreateTicketRequest(ctx, in)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	err = setup.store.FinishProcessing(ctx, FinishProcessingInput{
		ID:              req.ID,
		TenantID:        req.TenantID,
		ProcessingToken: req.ProcessingToken,
		TargetState:     StateSynced,
		GLPITicketID:    &ticketID,
		GLPITicketHref:  &apiHref,
		ActorType:       ActorTypeUser,
		ActorUserID:     &in.ActorUserID,
	})
	if err != nil {
		t.Fatalf("failed to finish processing: %v", err)
	}

	rec := doRequest(setup.srv.routes(), "GET", "/api/support/requests/"+req.ID, setup.user1Token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"webUrl":"https://glpi.example.com/front/ticket.form.php?id=888"`) {
		t.Fatalf("expected body to contain webUrl, got %s", body)
	}
	if strings.Contains(body, "glpiTicketHref") {
		t.Fatalf("expected body NOT to contain glpiTicketHref, got %s", body)
	}
	if strings.Contains(body, apiHref) {
		t.Fatalf("expected body NOT to contain internal api href, got %s", body)
	}
}
