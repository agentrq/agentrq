# Slack Integration Setup Guide

AgentRQ's Slack integration allows seamless, real-time collaboration between you and your AI agents directly from your Slack workspace. It enables:
- **Slash Commands**: Use `/t` to create tasks instantly from Slack.
- **Bidirectional Sync**: Real-time message and private file attachment synchronization inside Slack threads.
- **MCP Permission Control**: Approve or reject sensitive agent commands using interactive Block Kit buttons (Allow/Deny) inside Slack threads.
- **Secure Tenancy**: Each AgentRQ workspace maps directly to a single, isolated private Slack channel, ensuring strict task and file separation between projects. All OAuth tokens are persisted and AES-256 encrypted.

---

## 🛠️ Step 1: Create a Slack App

1. Go to the [Slack App Console](https://api.slack.com/apps) and click **Create New App**.
2. Select **From scratch**.
3. Name your app (e.g., `AgentRQ`) and choose your Slack Development Workspace.

---

## 🔑 Step 2: Configure Scopes & OAuth

1. Navigate to **OAuth & Permissions** in the sidebar.
2. Under **Redirect URLs**, click **Add New Redirect URL** and add your endpoint:
   - For production: `https://<your-domain>/slack/oauth/callback`
   - For local development (using Ngrok): `https://<your-subdomain>.ngrok-free.app/slack/oauth/callback`
   - Click **Save URLs**.
3. Scroll down to **Scopes** -> **Bot Token Scopes** and add the following:
   - `groups:write` — Allows the bot to automatically provision private channels.
   - `groups:read` — Allows the bot to find/reuse existing private channels.
   - `chat:write` — Allows the bot to post task updates and reply in threads.
   - `commands` — Registers the `/t` slash command.
   - `app_mentions:read` — Allows the bot to sync thread replies.

---

## ⚡ Step 3: Configure Slash Commands

1. Go to **Slash Commands** and click **Create New Command**.
2. Enter the details:
   - **Command**: `/t`
   - **Request URL**: `https://<your-domain>/slack/commands` (or your Ngrok tunnel URL: `https://<your-subdomain>.ngrok-free.app/slack/commands`)
   - **Short Description**: `Create an AgentRQ task`
   - **Usage Hint**: `[task description] or "[title]" "[description]"`
3. Click **Save**.

---

## 🔔 Step 4: Configure Events & Interactions

### Enable Event Subscriptions
1. Go to **Event Subscriptions** and toggle it **On**.
2. Under **Request URL**, enter: `https://<your-domain>/slack/events` (or your Ngrok tunnel URL: `https://<your-subdomain>.ngrok-free.app/slack/events`)
3. Under **Subscribe to bot events**, click **Add Bot User Event** and select:
   - `app_mention` (Triggered when the user mentions the bot in a task thread)
4. Click **Save Changes**.

### Enable Interactive Components
1. Go to **Interactive Components** and toggle it **On**.
2. Under **Request URL**, enter: `https://<your-domain>/slack/interactions` (or your Ngrok tunnel URL: `https://<your-subdomain>.ngrok-free.app/slack/interactions`)
3. Click **Save Changes**.

---

## ⚙️ Step 5: Configure Backend Environment

Retrieve your credentials from the Slack App Console (**Basic Information** and **App Credentials**) and add them to your `backend/cmd/server/_config/.env` file or environment:

```env
# Enable Slack Integration
AGENTRQ_SLACK_ENABLED=true

# Slack OAuth and App Configurations
AGENTRQ_SLACK_CLIENT_ID=your_client_id_here
AGENTRQ_SLACK_CLIENT_SECRET=your_client_secret_here
AGENTRQ_SLACK_SIGNING_SECRET=your_signing_secret_here
AGENTRQ_SLACK_APP_ID=your_app_id_here
```

Restart your backend server (`make dev`) to load the new settings.

---

## 🔌 Step 6: Link Slack in the Dashboard

1. Open the AgentRQ web UI.
2. Select your workspace and navigate to the **Settings** tab.
3. Click the **Slack** sub-tab.
4. Click **Link Slack Channel** (this initiates the OAuth2 v2 flow).
5. Review the requested permissions in Slack and authorize the app.
6. Upon redirect, associate your workspace with a private Slack channel. **Each AgentRQ workspace maps to exactly one private channel** to guarantee strict data separation between your projects:
   - **Automatic Provisioning (Recommended)**: If left blank, the app automatically creates a secure private channel named `agentrq-<workspace-name>` and invites the installer.
   - **Manual Provisioning**: Enter your preferred Slack Channel ID and Name manually in the input fields.

---

## 🚀 Usage Guide

- **Create a Task**: In your connected Slack channel, type `/t "Create a user login screen" "Include email and Google login buttons"`.
- **Add Replies**: Use `<@bot-name> <message>` (or `@agentrq <message>`) in any task thread. Messages, including uploaded private file attachments, will instantly sync to the AgentRQ dashboard in real-time.
- **Approve Permissions**: When an agent requests a manual action (like writing a file), Allowance buttons (Allow/Deny) will automatically appear in the task's Slack thread. Click them to grant authorization instantly!
