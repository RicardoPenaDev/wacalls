package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Monta uma licença assinada de verdade, do jeito que o Worker emite.
func licencaDeTeste(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, fp string) string {
	t.Helper()
	return licencaComValidade(t, pub, priv, fp, licenseNow().Add(33*24*time.Hour).Unix())
}

func licencaComValidade(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, fp string, expiraEm int64) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"v":           1,
		"codigo":      "WACL-TEST-TEST-TEST",
		"fingerprint": fp,
		"cliente":     "Cliente de Teste",
		"plano":       "mensal",
		"emitidoEm":   licenseNow().Unix(),
		"expiraEm":    expiraEm,
	})
	if err != nil {
		t.Fatalf("montando payload: %v", err)
	}
	corpo := base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(corpo))
	return "WACL1." + corpo + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func chaves(t *testing.T) (string, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("gerando chaves: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(pub), pub, priv
}

// O PowerShell 5.1 grava UTF-8 COM BOM por padrão. O BOM não é espaço para o
// strings.TrimSpace e o json.Unmarshal o rejeita — na prática o serviço recusava
// uma licença perfeitamente válida com "formato de licenca desconhecido".
func TestVerifyLicenseAceitaJSONComBOM(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	fp := machineFingerprint()
	lic := licencaDeTeste(t, pub, priv, fp)

	arquivo, err := json.Marshal(map[string]string{
		"licenca":     lic,
		"codigo":      "WACL-TEST-TEST-TEST",
		"fingerprint": fp,
	})
	if err != nil {
		t.Fatalf("montando licenca.json: %v", err)
	}

	for _, caso := range []struct {
		nome    string
		prefixo []byte
	}{
		{"sem BOM", nil},
		{"com BOM", []byte{0xEF, 0xBB, 0xBF}},
		{"com BOM e quebra de linha", []byte{0xEF, 0xBB, 0xBF, '\r', '\n'}},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			caminho := filepath.Join(t.TempDir(), "licenca.json")
			if err := os.WriteFile(caminho, append(caso.prefixo, arquivo...), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			dados, err := verifyLicense(caminho, pubB64)
			if err != nil {
				t.Fatalf("licenca valida foi recusada: %v", err)
			}
			if dados.Codigo != "WACL-TEST-TEST-TEST" {
				t.Fatalf("codigo lido = %q", dados.Codigo)
			}
		})
	}
}

// A licença crua (sem o JSON em volta) também é aceita, com ou sem BOM.
func TestVerifyLicenseAceitaLicencaCrua(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	lic := licencaDeTeste(t, pub, priv, machineFingerprint())

	caminho := filepath.Join(t.TempDir(), "licenca.txt")
	if err := os.WriteFile(caminho, append([]byte{0xEF, 0xBB, 0xBF}, []byte(lic+"\r\n")...), 0o600); err != nil {
		t.Fatalf("gravando: %v", err)
	}
	if _, err := verifyLicense(caminho, pubB64); err != nil {
		t.Fatalf("licenca crua recusada: %v", err)
	}
}

// Uma licença assinada por outra chave não pode passar - é o que impede alguém
// de emitir as proprias licenças.
func TestVerifyLicenseRecusaOutroEmissor(t *testing.T) {
	pubB64, _, _ := chaves(t)
	_, outroPub, outroPriv := chaves(t)
	lic := licencaDeTeste(t, outroPub, outroPriv, machineFingerprint())

	caminho := filepath.Join(t.TempDir(), "licenca.txt")
	if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
		t.Fatalf("gravando: %v", err)
	}
	_, err := verifyLicense(caminho, pubB64)
	if err == nil {
		t.Fatal("licenca de outro emissor foi aceita")
	}
	if !strings.Contains(err.Error(), "assinatura invalida") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

// Licença emitida para outra máquina não vale nesta.
func TestVerifyLicenseRecusaOutraMaquina(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	lic := licencaDeTeste(t, pub, priv, "00000000000000000000000000000000")

	caminho := filepath.Join(t.TempDir(), "licenca.txt")
	if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
		t.Fatalf("gravando: %v", err)
	}
	if _, err := verifyLicense(caminho, pubB64); err == nil {
		t.Fatal("licenca de outra maquina foi aceita")
	}
}

// Helpers para controle de estado global nos testes.
// Testes que mutam licenseNow ou licenseFlagPath NÃO devem usar t.Parallel(),
// pois alteram variáveis de pacote compartilhadas (além do t.Setenv proibir t.Parallel).

func mockLicenseClock(t *testing.T, nowFn func() time.Time) {
	t.Helper()
	antes := licenseNow
	licenseNow = nowFn
	t.Cleanup(func() {
		licenseNow = antes
	})
}

func mockLicenseFlagPath(t *testing.T, path string) {
	t.Helper()
	antes := licenseFlagPath
	licenseFlagPath = path
	t.Cleanup(func() {
		licenseFlagPath = antes
	})
}

// A faixa de aviso do painel vive deste estado. O que importa aqui é a
// fronteira: 7 dias de tolerância depois do vencimento o sistema ainda roda,
// e no oitavo não roda mais — é exatamente o que o cliente vê.
func TestLicenseStatusFaixasDeVencimento(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	t.Setenv("WACALLS_LICENSE_PUBKEY", pubB64)
	t.Setenv("WACALLS_LICENSE_REQUIRED", "1")

	fixo := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	mockLicenseClock(t, func() time.Time { return fixo })

	dia := int64(24 * 60 * 60)
	casos := []struct {
		nome         string
		expiraEm     int64
		valida       bool
		tolerancia   bool
		diasEsperado int
	}{
		{"com folga", fixo.Unix() + 20*dia, true, false, 20},
		{"vence em 2 dias", fixo.Unix() + 2*dia, true, false, 2},
		{"venceu ha 3 dias", fixo.Unix() - 3*dia, true, true, -3},
		{"venceu ha 10 dias", fixo.Unix() - 10*dia, false, false, -10},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			caminho := filepath.Join(t.TempDir(), "licenca.json")
			lic := licencaComValidade(t, pub, priv, machineFingerprint(), c.expiraEm)
			if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			mockLicenseFlagPath(t, caminho)

			st := licenseStatus()
			if !st.Exigida {
				t.Fatal("a licenca deveria estar exigida")
			}
			if st.Valida != c.valida {
				t.Fatalf("valida = %v, esperado %v (motivo: %s)", st.Valida, c.valida, st.Motivo)
			}
			if st.EmTolerancia != c.tolerancia {
				t.Fatalf("emTolerancia = %v, esperado %v", st.EmTolerancia, c.tolerancia)
			}
			if st.DiasParaVencer != c.diasEsperado {
				t.Fatalf("diasParaVencer = %d, esperado %d", st.DiasParaVencer, c.diasEsperado)
			}
			if c.tolerancia && st.DiasTolerancia != licenseGraceDays+c.diasEsperado {
				t.Fatalf("diasTolerancia = %d", st.DiasTolerancia)
			}
			if st.Suporte != licenseSupportPhone {
				t.Fatalf("suporte = %q", st.Suporte)
			}
		})
	}
}

// Testa exatamente os limites de cada faixa e um instante antes e depois.
func TestLicenseStatusLimitesEInstantes(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	t.Setenv("WACALLS_LICENSE_PUBKEY", pubB64)
	t.Setenv("WACALLS_LICENSE_REQUIRED", "1")

	fixo := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	mockLicenseClock(t, func() time.Time { return fixo })

	const dia = int64(86400)
	now := fixo.Unix()

	casos := []struct {
		nome           string
		expiraEm       int64
		validaEsperada bool
		emTolerancia   bool
		diasParaVencer int
		diasTolerancia int
	}{
		{"exatamente no segundo da expiracao", now, true, false, 0, 0},
		{"1 segundo antes de expirar", now + 1, true, false, 0, 0},
		{"1 segundo apos expirar entra em tolerancia", now - 1, true, true, 0, 7},
		{"exatamente 1 dia para vencer", now + dia, true, false, 1, 0},
		{"1 segundo a menos que 1 dia para vencer", now + dia - 1, true, false, 0, 0},
		{"1 segundo a mais que 1 dia para vencer", now + dia + 1, true, false, 1, 0},
		{"exatamente 7 dias para vencer janela renovacao", now + 7*dia, true, false, 7, 0},
		{"exatamente 7 dias vencida limite tolerancia", now - 7*dia, true, true, -7, 0},
		{"1 segundo antes de esgotar 7 dias de tolerancia", now - 7*dia + 1, true, true, -6, 1},
		{"1 segundo apos 7 dias vencida ainda no 7o dia", now - 7*dia - 1, true, true, -7, 0},
		{"exatamente 8 dias vencida tolerancia esgotada", now - 8*dia, false, false, -8, 0},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			caminho := filepath.Join(t.TempDir(), "licenca.json")
			lic := licencaComValidade(t, pub, priv, machineFingerprint(), c.expiraEm)
			if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			mockLicenseFlagPath(t, caminho)

			st := licenseStatus()
			if st.Valida != c.validaEsperada {
				t.Fatalf("valida = %v, esperado %v (motivo: %s)", st.Valida, c.validaEsperada, st.Motivo)
			}
			if st.EmTolerancia != c.emTolerancia {
				t.Fatalf("emTolerancia = %v, esperado %v", st.EmTolerancia, c.emTolerancia)
			}
			if st.DiasParaVencer != c.diasParaVencer {
				t.Fatalf("diasParaVencer = %d, esperado %d", st.DiasParaVencer, c.diasParaVencer)
			}
			if c.emTolerancia && st.DiasTolerancia != c.diasTolerancia {
				t.Fatalf("diasTolerancia = %d, esperado %d", st.DiasTolerancia, c.diasTolerancia)
			}
		})
	}
}

// Testa a estabilidade do cálculo através de viradas de dia, mês, ano e anos bissextos.
func TestLicenseStatusViradaDeDiaEData(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	t.Setenv("WACALLS_LICENSE_PUBKEY", pubB64)
	t.Setenv("WACALLS_LICENSE_REQUIRED", "1")

	const dia = int64(86400)
	instantes := []struct {
		nome string
		now  time.Time
	}{
		{"23:59:59 fim do dia", time.Date(2026, 9, 13, 23, 59, 59, 0, time.UTC)},
		{"00:00:00 meia-noite", time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)},
		{"00:00:01 inicio do dia", time.Date(2026, 9, 14, 0, 0, 1, 0, time.UTC)},
		{"virada de mes 28 de fev", time.Date(2026, 2, 28, 23, 59, 59, 0, time.UTC)},
		{"virada de mes 01 de mar", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"virada de ano 31 de dez", time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)},
		{"virada de ano 01 de jan", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"ano bissexto 28 de fev", time.Date(2028, 2, 28, 12, 0, 0, 0, time.UTC)},
		{"ano bissexto 29 de fev", time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC)},
	}

	for _, inst := range instantes {
		t.Run(inst.nome, func(t *testing.T) {
			mockLicenseClock(t, func() time.Time { return inst.now })

			caminho := filepath.Join(t.TempDir(), "licenca.json")
			expira := inst.now.Unix() + 5*dia
			lic := licencaComValidade(t, pub, priv, machineFingerprint(), expira)
			if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			mockLicenseFlagPath(t, caminho)

			st := licenseStatus()
			if !st.Valida {
				t.Fatalf("esperava licenca valida na transicao %s (motivo: %s)", inst.nome, st.Motivo)
			}
			if st.DiasParaVencer != 5 {
				t.Fatalf("esperava 5 dias para vencer em %s, obteve %d", inst.nome, st.DiasParaVencer)
			}
		})
	}
}

// Testa a consistência determinística sob fusos UTC e America/Sao_Paulo.
func TestLicenseStatusTimezonesUTCeSP(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	t.Setenv("WACALLS_LICENSE_PUBKEY", pubB64)
	t.Setenv("WACALLS_LICENSE_REQUIRED", "1")

	spLoc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("carregando timezone America/Sao_Paulo: %v", err)
	}

	const dia = int64(86400)
	baseUTC := time.Date(2026, 9, 13, 15, 0, 0, 0, time.UTC)
	baseSP := baseUTC.In(spLoc) // Mesmo instante absoluto (12:00:00 UTC-3)

	for _, c := range []struct {
		nome string
		now  time.Time
	}{
		{"fuso UTC", baseUTC},
		{"fuso America/Sao_Paulo", baseSP},
	} {
		t.Run(c.nome, func(t *testing.T) {
			mockLicenseClock(t, func() time.Time { return c.now })

			caminho := filepath.Join(t.TempDir(), "licenca.json")
			expira := c.now.Unix() + 3*dia
			lic := licencaComValidade(t, pub, priv, machineFingerprint(), expira)
			if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			mockLicenseFlagPath(t, caminho)

			st := licenseStatus()
			if !st.Valida {
				t.Fatalf("licenca deveria ser valida em %s: %s", c.nome, st.Motivo)
			}
			if st.DiasParaVencer != 3 {
				t.Fatalf("diasParaVencer = %d, esperado 3 em %s", st.DiasParaVencer, c.nome)
			}
		})
	}
}

// TestLicenseStatusConsultaRelogioUmaUnicaVez comprova deterministicamente que licenseStatus
// captura o relógio uma única vez por operação (eliminando janela de inconsistência).
func TestLicenseStatusConsultaRelogioUmaUnicaVez(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	t.Setenv("WACALLS_LICENSE_PUBKEY", pubB64)
	t.Setenv("WACALLS_LICENSE_REQUIRED", "1")

	casos := []struct {
		nome     string
		expiraEm func(time.Time) int64
	}{
		{"licenca valida", func(b time.Time) int64 { return b.Unix() + 5*86400 }},
		{"licenca em tolerancia", func(b time.Time) int64 { return b.Unix() - 2*86400 }},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			base := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
			chamadas := 0
			mockLicenseClock(t, func() time.Time {
				chamadas++
				return base.Add(time.Duration(chamadas) * time.Second)
			})

			caminho := filepath.Join(t.TempDir(), "licenca.json")
			lic := licencaComValidade(t, pub, priv, machineFingerprint(), c.expiraEm(base))
			if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			mockLicenseFlagPath(t, caminho)

			// Zera o contador logo antes de chamar licenseStatus para medir exclusivamente
			// as consultas ao relógio efetuadas por esta operação composta.
			chamadas = 0
			st := licenseStatus()
			if !st.Valida {
				t.Fatalf("licenca deveria ser valida: %s", st.Motivo)
			}
			if chamadas != 1 {
				t.Fatalf("licenseStatus consultou licenseNow %d vezes, esperado exatamente 1", chamadas)
			}
		})
	}
}

// Sem arquivo nenhum a API não pode explodir nem mentir que está válida.
func TestLicenseStatusSemArquivo(t *testing.T) {
	t.Setenv("WACALLS_LICENSE_REQUIRED", "0")
	mockLicenseFlagPath(t, filepath.Join(t.TempDir(), "nao-existe.json"))

	// Sem arquivo o licensePath cai na pasta de trabalho; garante que ali
	// tambem nao ha licenca.
	if _, err := os.Stat("licenca.json"); err == nil {
		t.Skip("ha um licenca.json na pasta do teste")
	}
	st := licenseStatus()
	if st.Valida {
		t.Fatal("sem arquivo a licenca nao pode ser valida")
	}
	if st.Motivo == "" {
		t.Fatal("deveria explicar o motivo")
	}
}
