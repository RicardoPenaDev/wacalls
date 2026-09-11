<div align="center">

# 📞 WaCalls Chat

**Plataforma de atendimento WhatsApp multi-conexão em Go + React.**
Chat em tempo real, filas, contatos, conexões multi-número, relatórios e automação com construtor de fluxos.

<br/>

[![Go](https://img.shields.io/badge/Go-1.27+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black)](https://react.dev)
[![Vite](https://img.shields.io/badge/Vite-7-646CFF?style=for-the-badge&logo=vite&logoColor=white)](https://vitejs.dev)
[![Tailwind](https://img.shields.io/badge/Tailwind-4-06B6D4?style=for-the-badge&logo=tailwindcss&logoColor=white)](https://tailwindcss.com)
[![Docker](https://img.shields.io/badge/Docker-Enabled-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://docker.com)
[![whatsmeow](https://img.shields.io/badge/whatsmeow-multi--device-25D366?style=for-the-badge&logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![SQLite](https://img.shields.io/badge/SQLite-embedded-003B57?style=for-the-badge&logo=sqlite&logoColor=white)](https://sqlite.org)

</div>

---

## 🌟 Funcionalidades Principais

- 🔐 **Autenticação e Perfis**: Login seguro por e-mail e senha com controle de permissões e opção exclusiva para administradores alterarem suas credenciais de acesso diretamente no menu do perfil.
- 💬 **Chat em Tempo Real & Conexões Unificadas**:
  - **Todas as Conexões em 1 Chat**: Modo unificado para visualizar e atender conversas de todos os números conectados em uma única tela, com badges identificando a conexão WhatsApp de origem.
  - **Aceitar Todos em Lote**: Botão e opção para aceitar todos os atendimentos aguardando de uma só vez.
  - **Finalizar Todos em Lote**: Botão e opção para encerrar todos os atendimentos da fila em massa com 1 clique.
  - Envio e recebimento multi-agente de texto, áudio, imagens e documentos.
- 📱 **Conexões WhatsApp Multi-Dispositivo**: Pareamento rápido via QR Code baseado na biblioteca `whatsmeow`.
- 🔀 **Construtor de Fluxos (Flow Builder)**:
  - Respostas automáticas, menus interativos e ramificações condicionais.
  - **Suporte a Botões com Link (Redirecionamento)**: Suporte nativo ao protocolo WhatsApp Native Flow `cta_url`, permitindo criar botões que abrem links e sites diretamente no navegador do cliente.
  - Botões de resposta rápida e menus de opções.
  - Fallback automático para menus de texto em clientes legados.
- 🗂️ **Filas de Atendimento**: Distribuição de chamados e conversas por setor/departamento.
- 👥 **Gestão de Contatos**: Sincronização automática com agenda do WhatsApp, histórico e edição.
- 🏷️ **Tags e Organização**: Categorização visual de conversas para facilitar o fluxo de trabalho.
- 📊 **Relatórios**: Métricas de mensagens enviadas, atendimentos e desempenho.
- 🐳 **Pronto para Docker**: Deploy simples com Docker Compose e persistência de dados em volume.

---

## 🚀 Como Rodar Localmente com Docker

### Pré-requisitos
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) instalado e em execução.

### Passo a Passo

1. **Clone o repositório:**
```bash
git clone https://github.com/RicardoPenaDev/wacalls.git
cd wacalls
```

2. **Inicie os contêineres:**
```bash
docker compose up -d --build
```

3. **Acesse no Navegador:**
- **URL**: [http://localhost:8090](http://localhost:8090)

### 🔑 Credenciais Padrão do Administrador
- **E-mail**: `admin@admin.com` (também aceita `admin.admin.com`)
- **Senha**: `admin`

> 💡 **Dica:** O administrador pode alterar sua senha de acesso a qualquer momento clicando em seu avatar/perfil no cabeçalho superior e selecionando **Trocar senha**.

---

## 🛠️ Stack Tecnológica

| Componente | Tecnologia | Detalhes |
|---|---|---|
| **Backend** | Go (Golang) | HTTP API, WebSockets e motor de execução de fluxos |
| **WhatsApp Engine** | whatsmeow | Conexão direta multi-device (sem emuladores) |
| **Banco de Dados** | SQLite (pure-Go) | Embarcado sem necessidade de servidor externo (`modernc.org/sqlite`) |
| **Frontend** | React 19 + Vite 7 | SPA moderna e responsiva |
| **Estilização** | TailwindCSS + shadcn/ui | Interface limpa e customizável |
| **Containerização** | Docker Multi-Stage | Imagem leve baseada em Alpine Linux com `ffmpeg` |

---

## 📁 Estrutura do Projeto

```
├── client/              # Frontend React 19 + Vite
│   ├── src/
│   │   ├── components/  # Componentes reutilizáveis e telas de fluxo
│   │   ├── pages/       # Rotas e páginas da aplicação
│   │   ├── services/    # Clientes de API REST
│   │   └── stores/      # Gerenciamento de estado (Zustand)
├── cmd/server/          # Backend Go (servidor HTTP e rotas)
│   ├── flowbridge.go    # Integração de fluxos com whatsmeow (Native Flow buttons)
│   ├── flowexec_chat.go # Motor de execução de fluxos de chat
│   └── main.go          # Ponto de entrada do servidor
├── internal/            # Pacotes internos (storage, cache, whatsapp)
├── Dockerfile           # Multi-stage build (Node -> Go -> Alpine)
└── docker-compose.yml   # Configuração do serviço e volumes
```

---

## ⚙️ Comandos Úteis do Docker

- **Acompanhar logs:**
  ```bash
  docker compose logs -f
  ```
- **Parar o serviço:**
  ```bash
  docker compose stop
  ```
- **Reiniciar o serviço:**
  ```bash
  docker compose start
  ```
- **Recriar os contêineres:**
  ```bash
  docker compose up -d --build
  ```

---

## 🔒 Licença

Este projeto é disponibilizado para uso privado. Todos os direitos reservados.