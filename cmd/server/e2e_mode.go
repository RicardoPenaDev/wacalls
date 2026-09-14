package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// e2eConfig holds all flags and parameters for the dedicated E2E test harness.
// This mode is disabled by default and must never be enabled in production.
type e2eConfig struct {
	Enabled         bool
	RunDir          string
	SessionID       string
	SessionName     string
	OwnJID          string
	ChatJID         string
	ChatName        string
	Hostname        string
	GLPIComputerID  string
	TacticalAgentID string
}

// isLoopbackHost checks whether a hostname or IP string resolves to a loopback address.
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackAddr verifies that a listen address binds strictly to loopback (never 0.0.0.0 or public interfaces).
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		// e.g. ":8080" binds all interfaces (INADDR_ANY), not loopback
		return false
	}
	return isLoopbackHost(host)
}

// isLoopbackURL verifies that a parsed URL has a host pointing strictly to loopback.
func isLoopbackURL(rawURL string) (bool, error) {
	if rawURL == "" {
		return true, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, err
	}
	host := u.Hostname()
	if host == "" {
		return false, errors.New("empty host in URL")
	}
	return isLoopbackHost(host), nil
}

// e2ePreconditionInput bundles every value validateE2EPreconditions needs so
// the function has exactly one call site in production (precheckE2EBoot,
// called once from main()) and exactly one shape in tests.
type e2ePreconditionInput struct {
	Cfg     e2eConfig
	Addr    string
	DBPath  string
	Support supportConfig
}

// validateE2EDatabasePath enforces a secure, structural contract ensuring that
// the SQLite database used in E2E mode is truly temporary and strictly contained
// inside the dedicated runDir created by the runner.
func validateE2EDatabasePath(runDir, dbPath string) error {
	trimmedRunDir := strings.TrimSpace(runDir)
	if trimmedRunDir == "" {
		return errors.New("e2e: -e2e-run-dir é obrigatório no modo E2E")
	}

	absRunDir, err := filepath.Abs(trimmedRunDir)
	if err != nil {
		return fmt.Errorf("e2e: caminho inválido de runDir: %w", err)
	}
	fi, err := os.Stat(absRunDir)
	if err != nil {
		return fmt.Errorf("e2e: runDir não existe ou não pôde ser acessado: %w", err)
	}
	if !fi.IsDir() {
		return errors.New("e2e: runDir deve ser um diretório")
	}

	canRunDir, err := filepath.EvalSymlinks(absRunDir)
	if err != nil {
		canRunDir = absRunDir
	}
	canRunDir = filepath.Clean(canRunDir)

	tempDir := os.TempDir()
	absTemp, err := filepath.Abs(tempDir)
	if err != nil {
		return fmt.Errorf("e2e: erro ao resolver os.TempDir(): %w", err)
	}
	canTemp, err := filepath.EvalSymlinks(absTemp)
	if err != nil {
		canTemp = absTemp
	}
	canTemp = filepath.Clean(canTemp)

	// Exigir que runDir esteja estritamente dentro de os.TempDir()
	relToTemp, err := filepath.Rel(canTemp, canRunDir)
	if err != nil || relToTemp == "." || strings.HasPrefix(relToTemp, "..") || filepath.IsAbs(relToTemp) {
		return errors.New("e2e: runDir deve estar estritamente dentro de os.TempDir()")
	}

	// Exigir prefixo exclusivo wacalls-e2e- no diretório de execução
	runDirBase := filepath.Base(canRunDir)
	if !strings.HasPrefix(runDirBase, "wacalls-e2e-") {
		return errors.New("e2e: runDir deve ter o prefixo exclusivo 'wacalls-e2e-'")
	}

	trimmedDB := strings.TrimSpace(dbPath)
	if trimmedDB == "" || strings.EqualFold(trimmedDB, "wacalls.db") {
		return errors.New("e2e: o banco de dados deve ser temporário e fornecido explicitamente pelo runner (não pode ser wacalls.db)")
	}

	absDB, err := filepath.Abs(trimmedDB)
	if err != nil {
		return fmt.Errorf("e2e: caminho inválido de banco de dados: %w", err)
	}

	// Canonicalizar diretório pai do banco de dados
	dbDir := filepath.Dir(absDB)
	canDBDir, err := filepath.EvalSymlinks(dbDir)
	if err != nil {
		canDBDir = dbDir
	}
	canDBDir = filepath.Clean(canDBDir)

	// O diretório pai canônico deve ser exatamente o canRunDir
	if !strings.EqualFold(canDBDir, canRunDir) {
		return errors.New("e2e: o arquivo de banco deve estar dentro do runDir")
	}

	// Prevenir traversal como runDir/../outro.db
	relToRun, err := filepath.Rel(canRunDir, filepath.Join(canDBDir, filepath.Base(absDB)))
	if err != nil || relToRun == "." || strings.HasPrefix(relToRun, "..") || filepath.Dir(relToRun) != "." {
		return errors.New("e2e: o arquivo de banco deve estar diretamente dentro do runDir (sem traversal)")
	}

	dbBase := filepath.Base(absDB)
	if strings.EqualFold(dbBase, "wacalls.db") {
		return errors.New("e2e: wacalls.db não é permitido")
	}

	// Exigir nome de banco exclusivamente E2E (prefixo wacalls-e2e- ou wacalls-e2e. e extensão .db)
	if (!strings.HasPrefix(dbBase, "wacalls-e2e-") && !strings.HasPrefix(dbBase, "wacalls-e2e.")) || !strings.HasSuffix(dbBase, ".db") {
		return errors.New("e2e: nome do banco deve possuir o prefixo exclusivo 'wacalls-e2e-' e extensão .db")
	}

	// Rejeitar arquivo de banco preexistente
	canonicalTarget := filepath.Join(canRunDir, dbBase)
	if _, err := os.Stat(canonicalTarget); err == nil {
		return errors.New("e2e: banco preexistente rejeitado; deve ser criado limpo pelo servidor")
	}

	return nil
}

// validateE2EPreconditions validates all required safeguards before allowing E2E mode or synthetic sessions:
//   - -e2e-mode must be active if any seed flag is supplied.
//   - server listen address must be strictly loopback.
//   - database path must be temporary, uncreated, and strictly contained inside runDir within os.TempDir().
//   - WACALLS_SUPPORT_ENABLED must be active.
//   - WACALLS_GLPI_BASE_URL, WACALLS_GLPI_WEB_BASE_URL, and WACALLS_TACTICAL_BASE_URL must point to loopback.
//
// This is the single, real safeguard function: it is called exactly once, by
// precheckE2EBoot, which is itself the exact function main() calls before
// booting the server. There is no duplicate/parallel validation anywhere
// else in the codebase — do not reintroduce one.
func validateE2EPreconditions(in e2ePreconditionInput) error {
	cfg, addr, dbPath, supCfg := in.Cfg, in.Addr, in.DBPath, in.Support
	if !cfg.Enabled {
		if cfg.RunDir != "" || cfg.SessionID != "" || cfg.OwnJID != "" || cfg.ChatJID != "" || cfg.Hostname != "" || cfg.GLPIComputerID != "" || cfg.TacticalAgentID != "" {
			return errors.New("e2e: flags de seed E2E não podem ser fornecidas sem -e2e-mode")
		}
		return nil
	}

	// 1. Listen address strictly loopback
	if !isLoopbackAddr(addr) {
		return errors.New("e2e: o servidor deve escutar exclusivamente em loopback")
	}

	// 2. Database path must be temporary, strictly inside runDir within os.TempDir()
	if err := validateE2EDatabasePath(cfg.RunDir, dbPath); err != nil {
		return err
	}

	// 3. Support enabled
	if !supCfg.Enabled {
		return errors.New("e2e: WACALLS_SUPPORT_ENABLED deve estar ativo")
	}

	// 4. GLPI base URL strictly loopback
	if ok, err := isLoopbackURL(supCfg.GLPIConfig.BaseURL); err != nil || !ok {
		return errors.New("e2e: WACALLS_GLPI_BASE_URL deve apontar exclusivamente para loopback")
	}

	// 5. Tactical base URL (when present) strictly loopback
	if supCfg.TacticalConfig != nil && supCfg.TacticalConfig.BaseURL != "" {
		if ok, err := isLoopbackURL(supCfg.TacticalConfig.BaseURL); err != nil || !ok {
			return errors.New("e2e: WACALLS_TACTICAL_BASE_URL deve apontar exclusivamente para loopback")
		}
	}

	// 6. GLPI web base URL (when present) strictly loopback
	if supCfg.GLPIWebBaseURL != "" {
		if ok, err := isLoopbackURL(supCfg.GLPIWebBaseURL); err != nil || !ok {
			return errors.New("e2e: WACALLS_GLPI_WEB_BASE_URL deve apontar exclusivamente para loopback")
		}
	}

	return nil
}

// precheckE2EBoot is the exact function main() calls, in this exact order,
// before any database or network I/O: load the support configuration purely
// from environment variables (no side effects beyond an optional local CA
// file read), then validate every E2E safeguard against the real addr/dbPath
// the process is about to bind/open. e2e_mode_test.go calls this same
// function so test coverage can never diverge from what production runs.
func precheckE2EBoot(cfg e2eConfig, addr, dbPath string) (supportConfig, error) {
	supCfg, err := loadSupportConfigWithE2E(cfg.Enabled)
	if err != nil {
		return supCfg, err
	}
	if err := validateE2EPreconditions(e2ePreconditionInput{Cfg: cfg, Addr: addr, DBPath: dbPath, Support: supCfg}); err != nil {
		return supCfg, err
	}
	return supCfg, nil
}

// setupE2ESyntheticSession sets up an in-memory-only WhatsApp session, initial chat metadata,
// and linked device binding for the E2E runner.
//
// Mandatory invariants:
//   - Exists strictly in memory;
//   - Never calls Device.Save(), PutDevice(), or startPairing();
//   - Is never persisted in the WhatsApp database tables;
//   - Uses only synthetic JIDs and names;
//   - Is registered via the minimal path so userCanAccessSession succeeds;
//   - Initiates zero external network traffic.
func (s *server) setupE2ESyntheticSession(ctx context.Context, cfg e2eConfig, defaultAdminEmail string) error {
	if !cfg.Enabled || cfg.SessionID == "" {
		return nil
	}

	if cfg.OwnJID == "" || cfg.ChatJID == "" {
		return errors.New("e2e: flags -e2e-own-jid e -e2e-chat-jid são obrigatórias quando -e2e-session-id é fornecido")
	}

	var ownerID string
	if defaultAdminEmail != "" && s.auth != nil {
		users, err := s.auth.ListUsers(ctx)
		if err == nil {
			target := strings.ToLower(strings.TrimSpace(defaultAdminEmail))
			for _, u := range users {
				if strings.ToLower(strings.TrimSpace(u.Email)) == target {
					ownerID = u.ID
					break
				}
			}
		}
	}

	ownJIDStr := strings.TrimSpace(cfg.OwnJID)
	if !strings.Contains(ownJIDStr, "@") {
		ownJIDStr += "@" + types.DefaultUserServer
	}
	parsedOwnJID, err := types.ParseJID(ownJIDStr)
	if err != nil {
		return fmt.Errorf("e2e: own JID inválido: %w", err)
	}

	chatJIDStr := strings.TrimSpace(cfg.ChatJID)
	if !strings.Contains(chatJIDStr, "@") {
		chatJIDStr += "@" + types.DefaultUserServer
	}
	if _, err := types.ParseJID(chatJIDStr); err != nil {
		return fmt.Errorf("e2e: chat JID inválido: %w", err)
	}

	sessionName := cfg.SessionName
	if sessionName == "" {
		sessionName = "E2E WhatsApp"
	}

	// 1. Device in memory only: container.NewDevice() does NOT call Save().
	device := s.sessions.container.NewDevice()
	device.ID = &parsedOwnJID
	device.PushName = sessionName
	client := whatsmeow.NewClient(device, waLog.Noop)

	// 2. Session in memory only:
	sess := newSession(s.sessions, cfg.SessionID, sessionName, client)
	sess.ownerID = ownerID
	sess.auth = AuthSnapshot{State: "open", Paired: true}

	// 3. Register in SessionManager in-memory map
	s.sessions.register(sess)

	// 4. Seed initial chat messages & metadata in the test SQLite DB so the chat list shows them
	chatName := cfg.ChatName
	if chatName == "" {
		chatName = "E2E Customer"
	}

	extraJids := []string{
		"5511999990002@s.whatsapp.net",
		"5511999990003@s.whatsapp.net",
		"5511999990004@s.whatsapp.net",
		"5511999990005@s.whatsapp.net",
		"5511999990006@s.whatsapp.net",
		"5511999990007@s.whatsapp.net",
		"5511999990008@s.whatsapp.net",
	}

	chatsToSeed := []struct {
		jid  string
		name string
	}{
		{chatJIDStr, chatName},
	}
	for i, j := range extraJids {
		if j != chatJIDStr {
			chatsToSeed = append(chatsToSeed, struct {
				jid  string
				name string
			}{j, fmt.Sprintf("Cliente E2E %d", i+2)})
		}
	}

	now := time.Now().UnixMilli()
	for idx, c := range chatsToSeed {
		if s.messages != nil {
			_ = s.messages.Insert(ctx, MessageRow{
				ID:         fmt.Sprintf("e2e-init-msg-%d", idx+1),
				SessionID:  cfg.SessionID,
				ChatJID:    c.jid,
				SenderJID:  c.jid,
				FromMe:     false,
				Ts:         now - int64(idx*1000),
				Kind:       "text",
				Body:       "Chamado de teste E2E",
				SenderName: c.name,
			})
		}
		if s.chatMeta != nil {
			_ = s.chatMeta.Upsert(ctx, ChatMeta{
				SessionID: cfg.SessionID,
				ChatJID:   c.jid,
				Name:      c.name,
				Status:    "open",
			})
		}
	}

	// 5. Seed device binding if hostname provided
	if cfg.Hostname != "" && s.bindings != nil {
		bindingID := "e2e-seed-" + strings.ToLower(strings.ReplaceAll(cfg.Hostname, " ", "-"))
		normHost := strings.ToUpper(strings.TrimSpace(cfg.Hostname))
		nowSec := time.Now().Unix()
		_, _ = s.bindings.Upsert(ctx, DeviceBinding{
			ID:                 bindingID,
			OwnerID:            ownerID,
			TenantID:           ownerID,
			Hostname:           cfg.Hostname,
			HostnameNormalized: normHost,
			TacticalAgentID:    cfg.TacticalAgentID,
			GLPIComputerID:     cfg.GLPIComputerID,
			MatchStatus:        "matched",
			LastVerifiedAt:     nowSec,
			CreatedAt:          nowSec,
			UpdatedAt:          nowSec,
		})
	}

	return nil
}
