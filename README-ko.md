# AgentRQ ── 에이전트-휴먼 협업 플랫폼

<p align="center">
  <a href="README.md">English</a> | <a href="README.zh-CN.md">简体中文</a>
  <br />
  <br />
  <a href="https://www.youtube.com/watch?v=GBAoSpuCzrU">YouTube에서 HD로 보기</a>
  <br />
  <br />
  <a href="https://discord.gg/xFSMaEA2b2">
    <img src="https://img.shields.io/badge/Discord-Join%20Community-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Discord" />
  </a>
</p>

AgentRQ는 휴먼 오퍼레이터와 AI 에이전트가 매끄럽게 협업하도록 설계된 현대적인 고성능 플랫폼입니다. **Model Context Protocol(MCP)** 을 활용해 Claude 같은 AI 모델이 워크스페이스의 태스크 관리 시스템과 직접 상호작용할 수 있게 합니다.

## 🚀 개요

AgentRQ는 사람과 AI 에이전트가 함께 일하는 공유 작업 공간이라고 생각하면 됩니다. 복잡한 목표를 관리 가능한 태스크로 나누고, 그 작업을 AI 에이전트에게 직접 위임할 수 있습니다.

에이전트는 MCP를 통해 워크스페이스 상태를 "볼" 수 있으므로, 자신에게 할당된 태스크를 가져오고, 상태를 갱신하고, 민감한 작업에 대한 권한을 요청하고, 사용자와 소통할 수 있습니다. 이 모든 내용은 플랫폼 전체에 실시간으로 동기화됩니다.

## 🏛 아키텍처

AgentRQ는 분리된 서비스 지향 아키텍처를 따릅니다.

### 백엔드(Go / Fiber)
- **API 서버**: 워크스페이스와 태스크 관리를 위한 Fiber 기반 REST API입니다.
- **MCP 서버**: AI 모델에 도구와 리소스를 노출하는 통합 `mcp-go` SSE 서버입니다.
- **CoreMCP(Supervisor)**: 전체 플랫폼의 모든 워크스페이스, 태스크, 통계를 에이전트가 관리할 수 있게 하는 전역 MCP 서버입니다.
- **데이터 계층**: 사용자 범위의 영속 저장소를 위해 GORM과 SQLite를 사용합니다.
- **인증**: Google OAuth2 연동과 JWT 기반 세션 관리를 사용합니다.
- **이벤트 버스**: 실시간 SSE 알림을 위한 내부 pub/sub 시스템입니다.

### 프론트엔드(Vue.js 3 / Vite)
- **현대적인 UI**: Vue 3, Pinia, Tailwind CSS로 구성되어 있습니다.
- **글래스모피즘**: 부드러운 전환과 실시간 업데이트를 갖춘 세련된 디자인 언어를 사용합니다.
- **반응형 상태**: SSE 이벤트를 통해 백엔드와 동기화됩니다.

## 🛠 시작하기

### 사전 요구 사항
- **Go** 1.21 이상
- **Node.js** 18 이상(npm 포함)
- **Google Cloud Console**: OAuth2 Client ID와 Secret

### 설정
1. `backend` 디렉터리에 `_config/base.yaml` 또는 `development.yaml`을 만듭니다.
2. Google OAuth2 자격 증명을 입력합니다.

```yaml
auth:
  google:
    client_id: "your-google-client-id"
    client_secret: "your-google-client-secret"
```

### 로컬 실행
제공된 `Makefile`로 전체 스택을 실행합니다.

```bash
# 1. 모든 의존성 설치
make install

# 2. 프론트엔드와 백엔드 실행
make dev
```

프론트엔드는 `http://localhost:5173`에서 사용할 수 있습니다.

### 셀프 호스팅(Docker)
미리 빌드된 Docker 이미지로 프로덕션 또는 개발 스택을 실행하려면 [셀프 호스팅 설정 가이드](SETUP.md)를 참고하세요.

> [!NOTE]
> **에이전트 / AI 어시스턴트:** Docker로 로컬 셀프 호스팅 AgentRQ 인스턴스를 설정, 구성, 실행, 진단해야 한다면 [SETUP.md](SETUP.md)를 참고하세요. 단계별 절차, Docker 실행 명령, 환경 변수 구성이 포함되어 있습니다.

## 🤖 Claude Code 및 AI 연동

AgentRQ는 **Claude Channel**로 매끄럽게 연동되도록 설계되었습니다. AI 에이전트는 자신에게 할당된 태스크를 보고 Claude 세션 안에서 직접 응답할 수 있습니다.

각 워크스페이스는 자체 MCP URL과 토큰을 가집니다. 이 정보는 워크스페이스 설정 모달에서 확인할 수 있습니다. 프로덕션에서는 `https://WORKSPACE_ID.mcp.agentrq.com/` 형식을 따릅니다.

### Step 1 — `.mcp.json`

로컬 프로젝트 디렉터리에 `.mcp.json` 파일을 만듭니다. 앞의 점은 필요합니다. 프로젝트마다 자체 파일을 사용하므로 Claude 인스턴스가 워크스페이스별로 격리됩니다. 아래의 `YOUR_MCP_URL`을 설정 모달에 표시되는 전체 URL로 바꿉니다. 예: `https://WORKSPACE_ID.mcp.agentrq.com/?token=TOKEN`

```json
{
  "mcpServers": {
    "agentrq-WORKSPACE_ID": {
      "type": "http",
      "url": "YOUR_MCP_URL"
    }
  }
}
```

### Step 2 — `.claude/settings.local.json`

같은 프로젝트 디렉터리에 `.claude/settings.local.json` 파일을 추가해 AgentRQ 도구를 미리 승인하면, 매 작업마다 권한 프롬프트가 뜨는 것을 줄일 수 있습니다.

```json
{
  "permissions": {
    "allow": [
      "mcp__agentrq-WORKSPACE_ID__updateTaskStatus",
      "mcp__agentrq-WORKSPACE_ID__getWorkspace",
      "mcp__agentrq-WORKSPACE_ID__reply",
      "mcp__agentrq-WORKSPACE_ID__createTask",
      "mcp__agentrq-WORKSPACE_ID__downloadAttachment",
      "mcp__agentrq-WORKSPACE_ID__getTaskMessages",
      "mcp__agentrq-WORKSPACE_ID__getNextTask"
    ]
  },
  "enableAllProjectMcpServers": true,
  "enabledMcpjsonServers": ["agentrq-WORKSPACE_ID"]
}
```

### Step 3 — Claude 시작

두 파일을 준비한 뒤 해당 프로젝트 디렉터리에서 Claude Code를 실행합니다.

```bash
claude --dangerously-load-development-channels server:agentrq-WORKSPACE_ID
```

> **팁:** 워크스페이스 ID, 토큰이 포함된 전체 MCP URL, 바로 붙여 넣을 수 있는 설정 조각은 각 AgentRQ 워크스페이스의 **Setup** 모달에서 확인할 수 있습니다.

### 사용 가능한 MCP 도구
연결되면 AI 에이전트는 다음 도구를 사용할 수 있습니다.
- `createTask`: 휴먼 사용자에게 태스크를 할당합니다. 반복 작업용 `cron_schedule`도 선택적으로 지원합니다.
- `updateTaskStatus`: 태스크 상태를 `notstarted`, `ongoing`, `blocked`, `completed` 사이에서 변경합니다.
- `reply`: AgentRQ 대시보드로 메시지를 실시간 전송합니다.
- `getWorkspace`: 워크스페이스 이름, 미션 설명, 태스크 통계를 가져옵니다.
- `getTaskMessages`: 커서 기반 페이지네이션으로 태스크 채팅 기록을 읽습니다.
- `getNextTask`: 에이전트에게 할당된 다음 "not started" 태스크를 효율적으로 가져옵니다.
- `downloadAttachment`: ID로 첨부파일을 가져옵니다.
- **실시간 알림**: 휴먼이 태스크와 상호작용할 때마다 에이전트는 `notifications/claude/channel` 프로토콜로 알림을 받습니다.

## 🌉 ACP Gateway(ACP 에이전트용 브리지)

Claude Code는 `claude/notifications`를 네이티브로 지원하지만, **Gemini CLI** 같은 다른 에이전트는 AgentRQ의 실시간 태스크 알림을 받기 위해 브리지가 필요합니다. `@agentrq/acp-gateway`는 [Agent Client Protocol(ACP)](https://agentclientprotocol.com)과 MCP를 연결합니다.

### 설치

```bash
npm install -g @agentrq/acp-gateway
```

### 사용법

1. 프로젝트 루트에 [`.mcp.json`](#step-1--mcpjson)이 있는지 확인합니다.
2. 게이트웨이 뒤에 에이전트의 ACP 명령을 붙여 실행합니다.

```bash
# Gemini CLI 사용 예
acp-gateway -- gemini --acp
```

게이트웨이는 자동으로 다음 작업을 수행합니다.
- `.mcp.json`의 URL로 AgentRQ 워크스페이스에 연결합니다.
- 에이전트 하위 프로세스를 실행하고 표준 입출력을 브리지합니다.
- 태스크 할당, 메시지, 권한 요청을 실시간으로 전달합니다.

## 🌌 Codex Gateway(OpenAI Codex용 브리지)

ACP Gateway와 비슷하게 `@agentrq/codex-gateway`는 Model Context Protocol(MCP)과 Codex app-server 프로토콜을 브리지해 [OpenAI Codex](https://github.com/openai/codex)를 AgentRQ 워크스페이스에 연결합니다.

### 설치

```bash
npm install -g @agentrq/codex-gateway@latest
```

### 설정

**1. Codex용 agentrq MCP 서버 설정(프로젝트 단위)**

Codex는 프로젝트 단위 MCP 서버 설정을 `.codex/config.toml`에서 읽습니다. 이 파일을 만들어 Codex 에이전트가 작업 실행 중 agentrq 도구를 직접 사용할 수 있게 합니다. `<WORKSPACEID>`와 `<TOKEN>`은 agentrq 대시보드의 값으로 바꿉니다.

```bash
mkdir -p .codex
cat >> .codex/config.toml << 'EOF'

[mcp_servers.agentrq-workspace]
url = "https://<WORKSPACEID>.mcp.agentrq.com/?token=<TOKEN>"

[mcp_servers.agentrq-<ID>.tools.updateTaskStatus]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.getWorkspace]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.reply]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.createTask]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.downloadAttachment]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.getTaskMessages]
approval_mode = "approve"

[mcp_servers.agentrq-<ID>.tools.getNextTask]
approval_mode = "approve"
EOF
```

**2. 게이트웨이의 agentrq 연결 설정**

`codex-gateway`가 같은 agentrq 워크스페이스에 연결할 수 있도록 프로젝트 루트에 `.mcp.json`을 만듭니다.

```json
{
  "mcpServers": {
    "agentrq": {
      "type": "http",
      "url": "https://<WORKSPACEID>.mcp.agentrq.com/mcp?token=<TOKEN>"
    }
  }
}
```

> **참고:** `.mcp.json`은 `codex-gateway`가 태스크를 받기 위해 사용합니다. `.codex/config.toml`은 Codex 에이전트가 실행 중 `reply`, `updateTaskStatus` 같은 agentrq 도구를 호출하는 데 사용합니다.

### 사용법

`.mcp.json`이 있는 agentrq 워크스페이스 루트에서 `codex-gateway`를 실행합니다.

```bash
# 기본값: `codex app-server` 실행
codex-gateway

# 사용자 지정 codex 명령
codex-gateway -- codex app-server
```

## 👑 Supervisor(CoreMCP)

개별 워크스페이스가 특정 프로젝트에 대한 범위 제한 뷰를 제공한다면, **Supervisor(CoreMCP)** 는 전체 AgentRQ 계정에 대한 조감도와 관리 기능을 에이전트에게 제공하는 전역 MCP 서버입니다.

Supervisor는 `https://mcp.agentrq.com/mcp`에서 접근할 수 있습니다. 최신 AI 도구가 안전하게 연결할 수 있도록 **OAuth2** 인증을 사용합니다.

### Supervisor를 사용하는 이유
- **다중 워크스페이스 관리**: 워크스페이스를 나열하고, 만들고, 갱신합니다.
- **전역 태스크 보기**: `listAllTasks` 한 번으로 모든 워크스페이스의 태스크를 가져옵니다.
- **관리 제어**: 태스크 할당, 상태, 우선순위를 전역으로 관리합니다.
- **통합 통계**: 모든 워크스페이스의 상세 통계와 상태 지표에 접근합니다.

### 사용 가능한 Supervisor 도구
Supervisor는 전역 관리를 위한 포괄적인 도구를 제공합니다. 필요한 경우 `workspaceId` 파라미터를 사용합니다.

**워크스페이스 관리**
- `listWorkspaces`: 활성 및 보관된 모든 워크스페이스 개요입니다.
- `createWorkspace`: 새 프로젝트 환경을 시작합니다.
- `getWorkspace`: ID로 특정 워크스페이스 상세 정보를 가져옵니다.
- `updateWorkspace`: 워크스페이스 설정과 메타데이터를 수정합니다.
- `getWorkspaceStats`: 워크스페이스의 상위 분석 및 성능 데이터를 가져옵니다.

**태스크 관리**
- `listAllTasks`: 전체 플랫폼의 태스크를 검색하고 필터링합니다.
- `listTasks`: 특정 워크스페이스의 태스크를 나열합니다.
- `createTask`: 특정 워크스페이스에 새 태스크를 만듭니다.
- `getTask`: 특정 태스크의 상세 정보를 가져옵니다.
- `updateTaskStatus`: 태스크 상태를 변경합니다.
- `updateTaskOrder`: 목록에서 태스크 순서를 변경합니다.
- `updateTaskAssignee`: 태스크 담당자를 변경합니다.
- `updateTaskAllowAll`: 태스크의 `allow_all_commands` 권한을 토글합니다.
- `updateScheduledTask`: 예약/cron 태스크를 수정합니다.

**커뮤니케이션 및 파일**
- `replyToTask`: 태스크 채팅 스레드에 메시지를 게시합니다.
- `respondToTask`: 권한 요청에 대한 allow/deny 판단을 제출합니다.
- `getAttachment`: 특정 첨부파일의 데이터와 메타데이터를 base64로 가져옵니다.

### Supervisor에 연결하기(Claude Code)
Supervisor는 OAuth2를 사용하므로 `~/.mcp.json`에 다음 설정으로 연결할 수 있습니다.

```json
{
  "mcpServers": {
    "agentrq": {
      "type": "http",
      "url": "https://mcp.agentrq.com/mcp"
    }
  }
}
```

이 서버로 Claude를 처음 실행하면 브라우저에서 인증할 수 있는 링크가 제공됩니다.

## 🧩 공식 확장

AgentRQ는 주요 AI 에이전트 CLI 도구에서 Supervisor MCP 설정과 연동을 단순화하기 위한 공식 확장을 제공합니다. 하위 에이전트 MCP는 각자의 워크스페이스별 MCP 서버 URL을 사용해야 합니다.

### 🍊 Claude Code
Claude Code용 AgentRQ 플러그인은 공식 마켓플레이스를 통해 배포됩니다. 내장 스킬과 사전 설정된 MCP 접근을 제공합니다.

**설치:**
```bash
/plugin marketplace add https://github.com/agentrq/agentrq-claude-extension
/plugin install agentrq@agentrq
```

### ♊ Gemini CLI
Gemini CLI 확장을 사용하면 Google Gemini 모델로 터미널에서 직접 AgentRQ 워크스페이스와 태스크를 관리할 수 있습니다.

> **팁:** Gemini에서 실시간 태스크 알림을 활성화하려면 [ACP Gateway](#-acp-gatewayacp-에이전트용-브리지)를 사용하세요.

**설치:**
```bash
gemini extensions install https://github.com/agentrq/agentrq-gemini-extension
```

## 🔌 연동

### Slack 연동
AgentRQ는 실시간 태스크 생성, 스레드 답글 동기화, 에이전트 권한 요청을 위한 멀티테넌트 Slack 연동을 지원합니다.
- [Slack 연동 설정 및 사용 가이드](integrations/slack/README.md)

## 🤝 크레딧

- [AgentRQ](https://agentrq.com) — 공식 에이전트-휴먼 협업 플랫폼입니다.
- [HasMCP](https://hasmcp.com) — API와 에이전트 사이의 간극을 연결합니다.

## 📝 라이선스
Apache-2.0
