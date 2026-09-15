package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type EvolutionAlertConfig struct {
	Enabled      bool   `json:"enabled"`
	ApiURL       string `json:"apiUrl"`
	ApiKey       string `json:"apiKey"`
	InstanceName string `json:"instanceName"`
	Destination  string `json:"destination"`
}

type alertTracker struct {
	mu        sync.Mutex
	lastState map[string]string
	lastSent  map[string]time.Time
}

var globalAlertTracker = &alertTracker{
	lastState: make(map[string]string),
	lastSent:  make(map[string]time.Time),
}

func (s *server) registerEvolutionAlertRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/evolution-alert", s.requireAuth(s.handleGetEvolutionAlert))
	mux.HandleFunc("PUT /api/settings/evolution-alert", s.requireAuth(s.handleSetEvolutionAlert))
	mux.HandleFunc("POST /api/settings/evolution-alert/test", s.requireAuth(s.handleTestEvolutionAlert))
}

func (s *server) getEvolutionAlertConfig(ctx context.Context) (EvolutionAlertConfig, error) {
	var cfg EvolutionAlertConfig
	if s.settings == nil {
		return cfg, nil
	}
	v, err := s.settings.getKV(ctx, "evolution_alert")
	if err != nil || v == "" {
		return cfg, err
	}
	_ = json.Unmarshal([]byte(v), &cfg)
	return cfg, nil
}

func (s *server) setEvolutionAlertConfig(ctx context.Context, cfg EvolutionAlertConfig) error {
	if s.settings == nil {
		return fmt.Errorf("settings store unavailable")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.settings.setKV(ctx, "evolution_alert", string(data))
}

func (s *server) sendEvolutionAlert(ctx context.Context, cfg EvolutionAlertConfig, text string) error {
	if !cfg.Enabled || cfg.ApiURL == "" || cfg.InstanceName == "" || cfg.Destination == "" {
		return nil
	}

	cleanURL := strings.TrimRight(strings.TrimSpace(cfg.ApiURL), "/")
	if !strings.HasPrefix(cleanURL, "http://") && !strings.HasPrefix(cleanURL, "https://") {
		cleanURL = "http://" + cleanURL
	}
	endpoint := fmt.Sprintf("%s/message/sendText/%s", cleanURL, url.PathEscape(strings.TrimSpace(cfg.InstanceName)))

	dest := strings.TrimSpace(cfg.Destination)
	payload := map[string]any{
		"number": dest,
		"text":   text,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.ApiKey != "" {
		req.Header.Set("apikey", strings.TrimSpace(cfg.ApiKey))
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("erro ao conectar na Evolution API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("evolution api respondeu status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (s *server) handleGetEvolutionAlert(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.getEvolutionAlertConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *server) handleSetEvolutionAlert(w http.ResponseWriter, r *http.Request) {
	var cfg EvolutionAlertConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.setEvolutionAlertConfig(r.Context(), cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *server) handleTestEvolutionAlert(w http.ResponseWriter, r *http.Request) {
	var cfg EvolutionAlertConfig
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&cfg)
	}
	if cfg.ApiURL == "" && cfg.InstanceName == "" {
		saved, err := s.getEvolutionAlertConfig(r.Context())
		if err == nil {
			cfg = saved
		}
	}
	cfg.Enabled = true

	if cfg.ApiURL == "" || cfg.InstanceName == "" || cfg.Destination == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Preencha a URL, a instância e o grupo/número de destino para testar."})
		return
	}

	loc, _ := time.LoadLocation("America/Sao_Paulo")
	now := time.Now()
	if loc != nil {
		now = now.In(loc)
	}
	nowStr := now.Format("02/01/2006 15:04:05")

	testMsg := fmt.Sprintf("🔔 *Teste de Alerta - WaCalls*\n\nEsta é uma mensagem de teste enviada através da Evolution API.\n⏰ *Data/Hora:* %s\n✅ Seus dados estão configurados e funcionando perfeitamente!", nowStr)

	if err := s.sendEvolutionAlert(r.Context(), cfg, testMsg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Mensagem de teste enviada com sucesso!"})
}

func (s *server) handleSessionStateChange(sess *Session, newState, jid string) {
	if !s.alertsReady.Load() {
		// Suppress burst notifications while the server is initializing/restoring sessions
		return
	}

	globalAlertTracker.mu.Lock()
	prevState := globalAlertTracker.lastState[sess.id]

	var isConnected bool
	if newState == "open" {
		if prevState == "open" {
			globalAlertTracker.mu.Unlock()
			return
		}
		isConnected = true
	} else if (newState == "logged_out" || newState == "disconnected") && prevState == "open" {
		isConnected = false
	} else {
		// Irrelevant state transition (e.g. connecting, or already logged out)
		globalAlertTracker.lastState[sess.id] = newState
		globalAlertTracker.mu.Unlock()
		return
	}

	globalAlertTracker.lastState[sess.id] = newState

	now := time.Now()
	if last, ok := globalAlertTracker.lastSent[sess.id]; ok && now.Sub(last) < 10*time.Second {
		globalAlertTracker.mu.Unlock()
		return
	}
	globalAlertTracker.lastSent[sess.id] = now
	globalAlertTracker.mu.Unlock()

	go func(sessionName, sessionID, sessionJID string, connected bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		cfg, err := s.getEvolutionAlertConfig(ctx)
		if err != nil || !cfg.Enabled {
			return
		}

		loc, _ := time.LoadLocation("America/Sao_Paulo")
		t := time.Now()
		if loc != nil {
			t = t.In(loc)
		}
		nowStr := t.Format("02/01/2006 15:04:05")

		phoneDisplay := sessionJID
		if phoneDisplay == "" {
			phoneDisplay = "Sem número vinculado"
		} else {
			phoneDisplay = strings.Split(phoneDisplay, "@")[0]
			phoneDisplay = strings.Split(phoneDisplay, ":")[0]
		}

		var msg string
		if connected {
			msg = fmt.Sprintf("✅ *Alerta WaCalls: Instância Conectada!*\n\n📱 *Instância:* %s\n📞 *Número:* %s\n⏰ *Data/Hora:* %s\n🟢 *Status:* Conectado com sucesso.", sessionName, phoneDisplay, nowStr)
		} else {
			msg = fmt.Sprintf("⚠️ *Alerta WaCalls: Instância Desconectada!*\n\n📱 *Instância:* %s\n📞 *Número:* %s\n⏰ *Data/Hora:* %s\n🚨 *Status:* Desconectado / Sessão encerrada.\n⚠️ *Aviso:* Verifique a conexão no painel do WaCalls.", sessionName, phoneDisplay, nowStr)
		}

		if err := s.sendEvolutionAlert(ctx, cfg, msg); err != nil {
			s.log.Warn("falha ao enviar alerta evolution", "session", sessionID, "err", err)
		} else {
			s.log.Info("alerta evolution enviado com sucesso", "session", sessionID, "connected", connected)
		}
	}(sess.name, sess.id, jid, isConnected)
}
