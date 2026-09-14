package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wacalls/internal/glpi"
	"wacalls/internal/tactical"
	"wacalls/internal/testdb"

	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func newValidE2ETestDir(t *testing.T) (runDir, dbPath string) {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "wacalls-e2e-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir, filepath.Join(dir, "wacalls-e2e-suite.db")
}

func TestE2EMode_SeedSolicitadoSemModoE2EAborta(t *testing.T) {
	cases := []struct {
		name string
		cfg  e2eConfig
	}{
		{"session_id_sem_e2e", e2eConfig{Enabled: false, SessionID: "sess-1"}},
		{"own_jid_sem_e2e", e2eConfig{Enabled: false, OwnJID: "5511999990001"}},
		{"chat_jid_sem_e2e", e2eConfig{Enabled: false, ChatJID: "5511999990002"}},
		{"hostname_sem_e2e", e2eConfig{Enabled: false, Hostname: "PC-01"}},
		{"glpi_computer_sem_e2e", e2eConfig{Enabled: false, GLPIComputerID: "10"}},
		{"tactical_agent_sem_e2e", e2eConfig{Enabled: false, TacticalAgentID: "agent-10"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateE2EPreconditions(e2ePreconditionInput{Cfg: tc.cfg, Addr: "127.0.0.1:8080", DBPath: "temp.db", Support: supportConfig{Enabled: true}})
			if err == nil {
				t.Fatalf("esperado erro ao fornecer flags de seed sem -e2e-mode, obteve nil")
			}
			if !strings.Contains(err.Error(), "não podem ser fornecidas sem -e2e-mode") {
				t.Fatalf("mensagem inesperada de erro: %v", err)
			}
		})
	}
}

func TestE2EMode_EnderecoNaoLoopbackAborta(t *testing.T) {
	invalidAddrs := []string{
		":8080",
		"0.0.0.0:8080",
		"192.168.1.100:8080",
		"10.0.0.1:8080",
		"8.8.8.8:80",
		"example.com:8080",
		"[::]:8080",
		"::",
		"0.0.0.0",
	}

	runDir, validDB := newValidE2ETestDir(t)
	cfg := e2eConfig{Enabled: true, RunDir: runDir}
	supCfg := supportConfig{
		Enabled:    true,
		GLPIConfig: glpi.Config{BaseURL: "https://127.0.0.1:9000"},
	}

	for _, addr := range invalidAddrs {
		t.Run(addr, func(t *testing.T) {
			err := validateE2EPreconditions(e2ePreconditionInput{Cfg: cfg, Addr: addr, DBPath: validDB, Support: supCfg})
			if err == nil {
				t.Fatalf("esperava erro para endereço não-loopback %q, obteve nil", addr)
			}
			if !strings.Contains(err.Error(), "escutar exclusivamente em loopback") {
				t.Fatalf("mensagem inesperada: %v", err)
			}
		})
	}

	validAddrs := []string{
		"127.0.0.1:8080",
		"localhost:8080",
		"[::1]:8080",
		"127.0.0.1:0",
	}
	for _, addr := range validAddrs {
		t.Run("valido_"+addr, func(t *testing.T) {
			err := validateE2EPreconditions(e2ePreconditionInput{Cfg: cfg, Addr: addr, DBPath: validDB, Support: supCfg})
			if err != nil {
				t.Fatalf("esperava sucesso para loopback %q, obteve erro: %v", addr, err)
			}
		})
	}
}

func TestPrecheckE2EBoot_DatabaseValidation(t *testing.T) {
	setBaseSupportEnv := func(t *testing.T) {
		t.Helper()
		clearE2EEnv(t)
		t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://127.0.0.1:9000")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
		t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
		t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
	}

	t.Run("caminho_valido_dentro_do_runDir", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		supCfg, err := precheckE2EBoot(cfg, "127.0.0.1:8080", dbPath)
		if err != nil {
			t.Fatalf("esperava sucesso para caminho válido dentro do runDir, obteve: %v", err)
		}
		if !supCfg.Enabled {
			t.Fatal("esperava supCfg.Enabled=true")
		}
	})

	t.Run("banco_producao_real", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		prodPath := filepath.Join("C:\\", "Users", "banco_producao_real.db")
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", prodPath)
		if err == nil || !strings.Contains(err.Error(), "deve estar dentro do runDir") {
			t.Fatalf("esperava erro de banco fora do runDir, obteve: %v", err)
		}
	})

	t.Run("caminho_fora_de_os_TempDir", func(t *testing.T) {
		setBaseSupportEnv(t)
		outsideDir := filepath.Join("C:\\", "wacalls-e2e-outside")
		cfg := e2eConfig{Enabled: true, RunDir: outsideDir}
		dbPath := filepath.Join(outsideDir, "wacalls-e2e.db")
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", dbPath)
		if err == nil {
			t.Fatal("esperava rejeição para runDir fora de os.TempDir(), obteve nil")
		}
	})

	t.Run("banco_preexistente", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		if err := os.WriteFile(dbPath, []byte("pre-existing database content"), 0o600); err != nil {
			t.Fatalf("write existing db: %v", err)
		}
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", dbPath)
		if err == nil || !strings.Contains(err.Error(), "banco preexistente rejeitado") {
			t.Fatalf("esperava erro de banco preexistente rejeitado, obteve: %v", err)
		}
	})

	t.Run("traversal", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		traversalDB := filepath.Join(runDir, "..", "wacalls-e2e-escape.db")
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", traversalDB)
		if err == nil || !strings.Contains(err.Error(), "deve estar dentro do runDir") {
			t.Fatalf("esperava erro de traversal fora do runDir, obteve: %v", err)
		}
	})

	t.Run("caminho_irmao_prefixo_semelhante", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		siblingDir, err := os.MkdirTemp(os.TempDir(), "wacalls-e2e-sibling-")
		if err != nil {
			t.Fatalf("mkdir sibling: %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(siblingDir) })
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		siblingDB := filepath.Join(siblingDir, "wacalls-e2e.db")
		_, err = precheckE2EBoot(cfg, "127.0.0.1:8080", siblingDB)
		if err == nil || !strings.Contains(err.Error(), "deve estar dentro do runDir") {
			t.Fatalf("esperava erro para arquivo em diretório irmão, obteve: %v", err)
		}
	})

	t.Run("symlink_junction_quando_suportado", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		otherDir := t.TempDir()
		symDir := filepath.Join(runDir, "wacalls-e2e-symdir")
		if err := os.Symlink(otherDir, symDir); err != nil {
			t.Skip("os.Symlink não suportado no ambiente atual sem privilégios elevados")
		}
		defer os.Remove(symDir)
		dbInSymlink := filepath.Join(symDir, "wacalls-e2e.db")
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", dbInSymlink)
		if err == nil || !strings.Contains(err.Error(), "deve estar dentro do runDir") {
			t.Fatalf("esperava rejeição para symlink apontando fora do runDir, obteve: %v", err)
		}
	})

	t.Run("wacalls_db", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", filepath.Join(runDir, "wacalls.db"))
		if err == nil || !strings.Contains(err.Error(), "wacalls.db não é permitido") {
			t.Fatalf("esperava rejeição para wacalls.db, obteve: %v", err)
		}
	})

	t.Run("runDir_vazio", func(t *testing.T) {
		setBaseSupportEnv(t)
		cfg := e2eConfig{Enabled: true, RunDir: ""}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", "custom-e2e.db")
		if err == nil || (!strings.Contains(err.Error(), "runDir não pode ser vazio") && !strings.Contains(err.Error(), "-e2e-run-dir é obrigatório")) {
			t.Fatalf("esperava erro para runDir vazio, obteve: %v", err)
		}
	})

	t.Run("nome_sem_prefixo_e2e_rejeitado", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", filepath.Join(runDir, "custom.db"))
		if err == nil || !strings.Contains(err.Error(), "prefixo exclusivo 'wacalls-e2e-'") {
			t.Fatalf("esperava rejeição de nome sem prefixo wacalls-e2e-, obteve: %v", err)
		}
	})

	t.Run("extensao_invalida_rejeitada", func(t *testing.T) {
		setBaseSupportEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", filepath.Join(runDir, "wacalls-e2e.sqlite"))
		if err == nil || !strings.Contains(err.Error(), "extensão .db") {
			t.Fatalf("esperava rejeição de extensão que não é .db, obteve: %v", err)
		}
	})
}

func TestE2EMode_GLPINaoLoopbackAborta(t *testing.T) {
	invalidURLs := []string{
		"https://glpi.empresa.com",
		"https://192.168.1.50:9000",
		"http://10.0.0.1:80",
	}

	runDir, validDB := newValidE2ETestDir(t)
	cfg := e2eConfig{Enabled: true, RunDir: runDir}

	for _, u := range invalidURLs {
		t.Run("glpi_"+u, func(t *testing.T) {
			supCfg := supportConfig{
				Enabled:    true,
				GLPIConfig: glpi.Config{BaseURL: u},
			}
			err := validateE2EPreconditions(e2ePreconditionInput{Cfg: cfg, Addr: "127.0.0.1:8080", DBPath: validDB, Support: supCfg})
			if err == nil {
				t.Fatalf("esperava erro para GLPI URL não-loopback %q, obteve nil", u)
			}
			if !strings.Contains(err.Error(), "WACALLS_GLPI_BASE_URL deve apontar exclusivamente para loopback") {
				t.Fatalf("mensagem inesperada: %v", err)
			}
		})
	}
}

func TestE2EMode_TacticalNaoLoopbackAborta(t *testing.T) {
	invalidURLs := []string{
		"https://tactical.empresa.com",
		"http://192.168.1.60:8080",
	}

	runDir, validDB := newValidE2ETestDir(t)
	cfg := e2eConfig{Enabled: true, RunDir: runDir}

	for _, u := range invalidURLs {
		t.Run("tactical_"+u, func(t *testing.T) {
			supCfg := supportConfig{
				Enabled:        true,
				GLPIConfig:     glpi.Config{BaseURL: "https://127.0.0.1:9000"},
				TacticalConfig: &tactical.Config{BaseURL: u},
			}
			err := validateE2EPreconditions(e2ePreconditionInput{Cfg: cfg, Addr: "127.0.0.1:8080", DBPath: validDB, Support: supCfg})
			if err == nil {
				t.Fatalf("esperava erro para Tactical URL não-loopback %q, obteve nil", u)
			}
			if !strings.Contains(err.Error(), "WACALLS_TACTICAL_BASE_URL deve apontar exclusivamente para loopback") {
				t.Fatalf("mensagem inesperada: %v", err)
			}
		})
	}
}

func TestE2EMode_GLPIWebBaseURLNaoLoopbackAborta(t *testing.T) {
	runDir, validDB := newValidE2ETestDir(t)
	cfg := e2eConfig{Enabled: true, RunDir: runDir}
	supCfg := supportConfig{
		Enabled:        true,
		GLPIConfig:     glpi.Config{BaseURL: "https://127.0.0.1:9000"},
		GLPIWebBaseURL: "https://glpi.externo.com",
	}
	err := validateE2EPreconditions(e2ePreconditionInput{Cfg: cfg, Addr: "127.0.0.1:8080", DBPath: validDB, Support: supCfg})
	if err == nil {
		t.Fatal("esperava erro para GLPIWebBaseURL externa, obteve nil")
	}
	if !strings.Contains(err.Error(), "WACALLS_GLPI_WEB_BASE_URL deve apontar exclusivamente para loopback") {
		t.Fatalf("mensagem inesperada: %v", err)
	}
}

func TestE2EMode_SupportDesabilitadoAborta(t *testing.T) {
	runDir, validDB := newValidE2ETestDir(t)
	cfg := e2eConfig{Enabled: true, RunDir: runDir}
	supCfg := supportConfig{Enabled: false}

	err := validateE2EPreconditions(e2ePreconditionInput{Cfg: cfg, Addr: "127.0.0.1:8080", DBPath: validDB, Support: supCfg})
	if err == nil {
		t.Fatal("esperava erro quando support está desabilitado no modo E2E, obteve nil")
	}
	if !strings.Contains(err.Error(), "WACALLS_SUPPORT_ENABLED deve estar ativo") {
		t.Fatalf("mensagem inesperada: %v", err)
	}
}

func TestE2EMode_ModoDesligadoNaoCriaSessao(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.OpenTestDB(t, testdb.BackendSQLite)

	sessStore, err := newSessionStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	container := sqlstore.NewWithDB(tdb.DB, "sqlite", waLog.Noop)
	_ = container.Upgrade(ctx)

	mgr := newSessionManager(ctx, container, NewBroker(), sessStore, waLog.Noop, slog.Default(), 0)
	srv := &server{sessions: mgr}

	cfg := e2eConfig{Enabled: false, SessionID: "e2e-sess"}
	err = srv.setupE2ESyntheticSession(ctx, cfg, "admin@test.local")
	if err != nil {
		t.Fatalf("esperava nil quando disabled, obteve: %v", err)
	}
	if len(mgr.infos()) != 0 {
		t.Fatalf("esperava zero sessões registradas, obteve %d", len(mgr.infos()))
	}
}

func TestE2EMode_SessaoSinteticaEmMemoria_E_UserCanAccessSession(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.OpenTestDB(t, testdb.BackendSQLite)

	auth, err := newAuthStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("auth store: %v", err)
	}
	sessStore, err := newSessionStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	chatMeta, err := newChatMetaStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("chat meta store: %v", err)
	}
	msgStore, err := newMessageStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("message store: %v", err)
	}
	bStore, err := newDeviceBindingStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("device binding store: %v", err)
	}

	container := sqlstore.NewWithDB(tdb.DB, "sqlite", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatalf("container upgrade: %v", err)
	}

	mgr := newSessionManager(ctx, container, NewBroker(), sessStore, waLog.Noop, slog.Default(), 0)
	mgr.messages = msgStore
	mgr.chatMeta = chatMeta

	adminEmail := "admin@e2e-test.local"
	created, err := auth.SeedAdmin(ctx, adminEmail, "admin-password-123")
	if err != nil || !created {
		t.Fatalf("seed admin: created=%v, err=%v", created, err)
	}
	users, err := auth.ListUsers(ctx)
	if err != nil || len(users) == 0 {
		t.Fatalf("list users: %v", err)
	}
	adminUser := &users[0]

	mgr.UserTenantFn = func(userID string) string {
		if userID == "" {
			return ""
		}
		pid, err := auth.ParentOf(ctx, userID)
		if err == nil && pid != "" {
			return pid
		}
		return userID
	}
	mgr.IsAdminRoleFn = func(userID string) bool {
		ok, _ := auth.HasRole(ctx, userID, RoleAdmin)
		return ok
	}

	srv := &server{
		db:       tdb.DB,
		auth:     auth,
		sessions: mgr,
		chatMeta: chatMeta,
		messages: msgStore,
		bindings: bStore,
		log:      slog.Default(),
	}

	cfg := e2eConfig{
		Enabled:         true,
		SessionID:       "e2e-session-valid-01",
		SessionName:     "E2E WhatsApp Line",
		OwnJID:          "5511999990001",
		ChatJID:         "5511999990002",
		ChatName:        "Cliente E2E",
		Hostname:        "DESKTOP-E2E-TEST",
		GLPIComputerID:  "500",
		TacticalAgentID: "agent-500",
	}

	err = srv.setupE2ESyntheticSession(ctx, cfg, adminEmail)
	if err != nil {
		t.Fatalf("setupE2ESyntheticSession falhou: %v", err)
	}

	// 1. Sessão deve existir em memória no SessionManager
	sess, ok := mgr.Get(cfg.SessionID)
	if !ok || sess == nil {
		t.Fatalf("sessão sintética %q não encontrada no SessionManager", cfg.SessionID)
	}
	info := sess.info()
	if !info.Paired {
		t.Fatalf("esperava info.Paired=true, obteve %v", info.Paired)
	}
	if info.JID != "5511999990001@s.whatsapp.net" {
		t.Fatalf("esperava JID próprio normalizado, obteve %q", info.JID)
	}

	// 2. userCanAccessSession deve reconhecer a sessão
	currUser := &currentUser{
		ID:    adminUser.ID,
		Email: adminUser.Email,
		Roles: []string{RoleAdmin},
	}
	if !srv.userCanAccessSession(currUser, cfg.SessionID) {
		t.Fatalf("userCanAccessSession retornou false para o admin dono da sessão sintética")
	}

	// 3. Sessão NÃO deve estar persistida no banco SQLite de sessões WhatsApp
	var sessRowCount int
	err = tdb.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ?", cfg.SessionID).Scan(&sessRowCount)
	if err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if sessRowCount != 0 {
		t.Fatalf("sessão WhatsApp foi persistida na tabela 'sessions' (count=%d), violando o contrato estrito em memória", sessRowCount)
	}

	// 4. Dispositivo whatsmeow NÃO deve estar persistido (Device.Save() não foi chamado)
	ownJIDParsed, _ := types.ParseJID("5511999990001@s.whatsapp.net")
	persistedDevice, err := container.GetDevice(ctx, ownJIDParsed)
	if err == nil && persistedDevice != nil {
		t.Fatalf("dispositivo whatsmeow foi persistido no sqlstore, violando o contrato")
	}

	// 5. Conversas sintéticas devem aparecer no ListChats
	chats, err := msgStore.ListChats(ctx, cfg.SessionID)
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}
	if len(chats) < 1 {
		t.Fatalf("esperava ao menos 1 chat no ListChats, obteve %d", len(chats))
	}

	// 6. Metadados do chat devem existir
	meta, found, err := chatMeta.Get(ctx, cfg.SessionID, "5511999990002@s.whatsapp.net")
	if err != nil || !found {
		t.Fatalf("chat meta não encontrado: found=%v, err=%v", found, err)
	}
	if meta.Name != "Cliente E2E" {
		t.Fatalf("esperava nome 'Cliente E2E', obteve %q", meta.Name)
	}

	// 7. Device binding deve estar cadastrado no bStore
	binding, found, err := bStore.FindByHostname(ctx, adminUser.ID, "DESKTOP-E2E-TEST")
	if err != nil || !found {
		t.Fatalf("device binding não encontrado para DESKTOP-E2E-TEST: found=%v, err=%v", found, err)
	}
	if binding.GLPIComputerID != "500" || binding.TacticalAgentID != "agent-500" {
		t.Fatalf("IDs incorretos no device binding: %+v", binding)
	}
	if binding.MatchStatus != "matched" {
		t.Fatalf("MatchStatus inesperado: %q", binding.MatchStatus)
	}
}

func TestE2EMode_MissingOwnOrChatJID_ReturnsError(t *testing.T) {
	ctx := context.Background()
	srv := &server{}

	cases := []struct {
		name string
		cfg  e2eConfig
	}{
		{"missing_own_jid", e2eConfig{Enabled: true, SessionID: "s1", ChatJID: "5511999990002"}},
		{"missing_chat_jid", e2eConfig{Enabled: true, SessionID: "s1", OwnJID: "5511999990001"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := srv.setupE2ESyntheticSession(ctx, tc.cfg, "admin@test.local")
			if err == nil {
				t.Fatalf("esperava erro para %s, obteve nil", tc.name)
			}
			if !strings.Contains(err.Error(), "são obrigatórias quando -e2e-session-id é fornecido") {
				t.Fatalf("mensagem inesperada: %v", err)
			}
		})
	}
}

// clearE2EEnv resets every WACALLS_* variable this suite cares about to an
// explicit empty string for the duration of the subtest (t.Setenv restores
// automatically), so results never depend on the host/developer shell.
func clearE2EEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"WACALLS_SUPPORT_ENABLED", "WACALLS_GLPI_BASE_URL", "WACALLS_GLPI_CLIENT_ID",
		"WACALLS_GLPI_CLIENT_SECRET", "WACALLS_GLPI_USERNAME", "WACALLS_GLPI_PASSWORD",
		"WACALLS_GLPI_WEB_BASE_URL", "WACALLS_GLPI_CA_FILE",
		"WACALLS_TACTICAL_BASE_URL", "WACALLS_TACTICAL_API_KEY",
	} {
		t.Setenv(k, "")
	}
}

// TestPrecheckE2EBoot_RealEnvDrivenRejections calls precheckE2EBoot — the
// EXACT function main() calls, in the exact order (load support config from
// real environment variables, then validate) — before any database or
// network I/O. Unlike the table-driven tests above (which construct
// e2eConfig/supportConfig by hand to pin down individual
// validateE2EPreconditions branches), this test drives the entire real boot
// pipeline through os environment variables via t.Setenv, proving the two
// functions are wired together exactly as production wires them — there is
// no gap between what is tested and what main() executes.
//
// GLPI Web externo is intentionally NOT included here: matchGLPIOrigins
// (support_config.go) rejects any WACALLS_GLPI_WEB_BASE_URL whose origin
// differs from WACALLS_GLPI_BASE_URL before validateE2EPreconditions ever
// runs, so "GLPI loopback + GLPI Web externo" is not a reachable
// environment-variable combination in the real pipeline. That branch is
// covered directly by TestE2EMode_GLPIWebBaseURLNaoLoopbackAborta above,
// which documents exactly why.
func TestPrecheckE2EBoot_RealEnvDrivenRejections(t *testing.T) {
	t.Run("seed_sem_e2e_mode", func(t *testing.T) {
		clearE2EEnv(t)
		cfg := e2eConfig{Enabled: false, SessionID: "sess-1"}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", "temp.db")
		if err == nil || !strings.Contains(err.Error(), "não podem ser fornecidas sem -e2e-mode") {
			t.Fatalf("esperava erro de seed sem -e2e-mode, obteve: %v", err)
		}
	})

	t.Run("bind_0.0.0.0", func(t *testing.T) {
		clearE2EEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "0.0.0.0:8080", dbPath)
		if err == nil || !strings.Contains(err.Error(), "escutar exclusivamente em loopback") {
			t.Fatalf("esperava erro para bind 0.0.0.0, obteve: %v", err)
		}
	})

	t.Run("bind_vazio_universal", func(t *testing.T) {
		clearE2EEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, ":8080", dbPath)
		if err == nil || !strings.Contains(err.Error(), "escutar exclusivamente em loopback") {
			t.Fatalf("esperava erro para bind vazio/universal, obteve: %v", err)
		}
	})

	t.Run("bind_ipv6_universal_rejeitado", func(t *testing.T) {
		clearE2EEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "[::]:8080", dbPath)
		if err == nil || !strings.Contains(err.Error(), "escutar exclusivamente em loopback") {
			t.Fatalf("esperava erro para bind [::]:8080, obteve: %v", err)
		}
	})

	t.Run("bind_ipv6_sem_porta_rejeitado", func(t *testing.T) {
		clearE2EEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "::", dbPath)
		if err == nil || !strings.Contains(err.Error(), "escutar exclusivamente em loopback") {
			t.Fatalf("esperava erro para bind ::, obteve: %v", err)
		}
	})

	t.Run("bind_0.0.0.0_sem_porta_rejeitado", func(t *testing.T) {
		clearE2EEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "0.0.0.0", dbPath)
		if err == nil || !strings.Contains(err.Error(), "escutar exclusivamente em loopback") {
			t.Fatalf("esperava erro para bind 0.0.0.0 sem porta, obteve: %v", err)
		}
	})

	t.Run("bind_ipv6_loopback_aceito", func(t *testing.T) {
		clearE2EEnv(t)
		t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://127.0.0.1:9000")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
		t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
		t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "[::1]:8080", dbPath)
		if err != nil {
			t.Fatalf("esperava sucesso para bind [::1]:8080, obteve erro: %v", err)
		}
	})

	t.Run("bind_ipv4_loopback_aceito", func(t *testing.T) {
		clearE2EEnv(t)
		t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://127.0.0.1:9000")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
		t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
		t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:8080", dbPath)
		if err != nil {
			t.Fatalf("esperava sucesso para bind 127.0.0.1:8080, obteve erro: %v", err)
		}
	})

	t.Run("bind_localhost_aceito", func(t *testing.T) {
		clearE2EEnv(t)
		t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://127.0.0.1:9000")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
		t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
		t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "localhost:8080", dbPath)
		if err != nil {
			t.Fatalf("esperava sucesso para bind localhost:8080, obteve erro: %v", err)
		}
	})

	t.Run("banco_padrao", func(t *testing.T) {
		clearE2EEnv(t)
		runDir, _ := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:0", "wacalls.db")
		if err == nil || !strings.Contains(err.Error(), "banco de dados deve ser temporário") {
			t.Fatalf("esperava erro para banco padrão, obteve: %v", err)
		}
	})

	t.Run("support_desativado", func(t *testing.T) {
		clearE2EEnv(t)
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:0", dbPath)
		if err == nil || !strings.Contains(err.Error(), "WACALLS_SUPPORT_ENABLED deve estar ativo") {
			t.Fatalf("esperava erro de support desativado, obteve: %v", err)
		}
	})

	t.Run("glpi_externo", func(t *testing.T) {
		clearE2EEnv(t)
		t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://glpi.real.example.com")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
		t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
		t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:0", dbPath)
		if err == nil || !strings.Contains(err.Error(), "WACALLS_GLPI_BASE_URL deve apontar exclusivamente para loopback") {
			t.Fatalf("esperava erro de GLPI externo, obteve: %v", err)
		}
	})

	t.Run("tactical_externo", func(t *testing.T) {
		clearE2EEnv(t)
		t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://127.0.0.1:9000")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
		t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
		t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
		t.Setenv("WACALLS_TACTICAL_BASE_URL", "https://tactical.real.example.com")
		t.Setenv("WACALLS_TACTICAL_API_KEY", "key-1")
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		_, err := precheckE2EBoot(cfg, "127.0.0.1:0", dbPath)
		if err == nil || !strings.Contains(err.Error(), "WACALLS_TACTICAL_BASE_URL deve apontar exclusivamente para loopback") {
			t.Fatalf("esperava erro de Tactical externo, obteve: %v", err)
		}
	})

	t.Run("tudo_valido_aceita", func(t *testing.T) {
		clearE2EEnv(t)
		t.Setenv("WACALLS_SUPPORT_ENABLED", "1")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://127.0.0.1:9000")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-1")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "secret-1")
		t.Setenv("WACALLS_GLPI_USERNAME", "user-1")
		t.Setenv("WACALLS_GLPI_PASSWORD", "pass-1")
		runDir, dbPath := newValidE2ETestDir(t)
		cfg := e2eConfig{Enabled: true, RunDir: runDir}
		supCfg, err := precheckE2EBoot(cfg, "127.0.0.1:0", dbPath)
		if err != nil {
			t.Fatalf("esperava sucesso com configuração válida, obteve erro: %v", err)
		}
		if !supCfg.Enabled {
			t.Fatal("esperava supCfg.Enabled=true")
		}
	})
}
