package main

// defaultWelcomeMessage e a saudacao enviada para o proprio numero assim que
// ele e pareado pelo QR Code. Serve como "recibo" de que a conexao funcionou e
// como credito de quem distribui esta versao.
//
// Personalize sem recompilar com a variavel WACALLS_WELCOME_MESSAGE, ou
// desligue com WACALLS_WELCOME=off.
const defaultWelcomeMessage = "✅ *WhatsApp conectado com sucesso!*\n" +
	"\n" +
	"Você está usando o *WaCalls*, distribuído pelo canal *Vem Fazer* " +
	"(youtube.com/@vemfazer) em parceria com o *EquipeChat*.\n" +
	"\n" +
	"💚 *O sistema é gratuito e open source* — pode usar à vontade.\n" +
	"\n" +
	"💻 *O instalador local para Windows é o serviço pago: R$ 39,90/mês*\n" +
	"Com ele o WaCalls roda no seu próprio computador e você *não paga VPS " +
	"nem domínio* — economia que já cobre a mensalidade no primeiro mês. " +
	"Instalação em poucos cliques, atualização e suporte inclusos.\n" +
	"\n" +
	"🤖 *Automação de WhatsApp sob medida*\n" +
	"Fale com a gente: *81 99588-5670*\n" +
	"\n" +
	"⭐ *Precisa de mais recursos?*\n" +
	"A versão completa — mais conexões, campanhas, chamadas e IA — está em " +
	"*vozzap.com.br*\n" +
	"\n" +
	"_Mensagem automática, enviada uma única vez, quando este número foi conectado._"

// welcomeEnabled permite desligar a saudacao pelo .env.
func welcomeEnabled() bool {
	return false
}

// welcomeMessageText devolve o texto configurado ou o padrao. Na variavel de
// ambiente, "\n" literal vira quebra de linha (facilita escrever no .env).
func welcomeMessageText() string {
	return ""
}

// sendWelcomeToSelf envia a saudacao para a conversa do proprio numero, uma
// unica vez por pareamento. Desabilitado conforme solicitacao.
func (s *Session) sendWelcomeToSelf() {
	return
}

