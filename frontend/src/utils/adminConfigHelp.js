// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The help shown on the admin config page for each block, group and
 * setting, by its path in the config. A path with no entry shows no help, so
 * a setting a later release adds still appears, only without a description.
 *
 * Every instance of the server reads the same stored config, so where it
 * matters a line says whether a limit is counted by each instance on its own
 * or across all of them.
 */
export const CONFIG_HELP = {
  app: 'Where the server listens and the addresses people and agents reach it at.',
  'app.port': 'The port each instance listens on for HTTP. 3000 by default.',
  'app.sslPort': 'The port each instance listens on for HTTPS when SSL is on.',
  'app.baseUrl': 'The public address of the web app, such as https://agentrq.example.com. Links in emails, sign-in redirects and agent setup commands are built from it.',
  'app.domain': 'The domain the app is served on, such as agentrq.example.com. Requests for other hosts are refused, and each workspace\'s MCP server is reached at a subdomain under mcp.<domain>.',
  'app.basePath': 'A path prefix when a proxy serves the app under a subpath, such as /agentrq. Empty serves it at the root.',
  'app.cookieSecure': 'true sends the sign-in cookie only over HTTPS, false sends it over HTTP too. Empty follows the SSL setting.',
  'app.proxyDomain': 'Another host a reverse proxy reaches the app at, such as example.com. It is accepted like the domain.',
  'app.drainDelay': 'On shutdown, how long an instance keeps serving with its readiness check failing, so a load balancer stops sending it traffic first. Such as 10s; 0s stops at once.',

  auth: 'How people sign in to the web app.',
  'auth.apiJwtSecret': 'The key a person\'s session tokens, terminal and browser tickets and sign-in state are signed with. Required. Changing it signs everyone out.',
  'auth.mcpJwtSecret': 'The key agents\' and MCP clients\' tokens, OAuth codes and client ids are signed with. Required. Changing it invalidates every token agents and MCP clients hold, so each needs a new one.',
  'auth.rootLoginEnabled': 'Replaces the Google sign-in on the login page with a form for the root access token, for a server with no sign-in provider. The admin pages sign in with the token either way.',
  'auth.allowedRedirectDomains': 'Other hosts a sign-in may send people back to, comma separated, such as docs.example.com. Any other host is refused.',
  'auth.google': 'Sign in with a Google account.',
  'auth.google.clientId': 'The OAuth client ID from the Google Cloud console.',
  'auth.google.clientSecret': 'The OAuth client secret for that client ID.',
  'auth.github': 'Sign in with a GitHub account. The button shows on the login page once a client ID is set.',
  'auth.github.clientId': 'The client ID of the GitHub OAuth app.',
  'auth.github.clientSecret': 'The client secret of the GitHub OAuth app.',
  'auth.oauth2Providers': 'Other sign-in providers: any OAuth2 server whose userinfo endpoint answers with the OpenID Connect claims. Each shows a button on the login page. Its callback URL is <baseUrl>/api/v1/auth/oauth2/<id>/callback. A sign-in whose email the provider has not verified is refused.',
  'auth.oauth2Providers.id': 'Names the provider in its sign-in and callback URLs: lowercase letters, digits and -, such as gitlab. Changing it changes the callback URL, and the client secret must be typed again.',
  'auth.oauth2Providers.name': 'Shown on the button, as Continue with <name>.',
  'auth.oauth2Providers.clientId': 'The client ID the provider issued.',
  'auth.oauth2Providers.clientSecret': 'The client secret the provider issued for that client ID.',
  'auth.oauth2Providers.authUrl': 'The provider\'s authorization endpoint. HTTPS, or HTTP on localhost.',
  'auth.oauth2Providers.tokenUrl': 'The provider\'s token endpoint. HTTPS, or HTTP on localhost.',
  'auth.oauth2Providers.userInfoUrl': 'The provider\'s userinfo endpoint, which must return email and email_verified. HTTPS, or HTTP on localhost.',
  'auth.oauth2Providers.scopes': 'The scopes asked for, comma separated, such as openid, email, profile.',
  'auth.oauth2Providers.iconUrl': 'The icon on the button. An uploaded image is cropped square and resized to 40×40, and kept in S3 when it has an endpoint and bucket, in the storage directory otherwise. Empty shows a key.',

  network: 'Which address each client is counted as by the DDoS guard and the rate limits.',
  'network.trustedProxies': 'The proxies whose client address header is believed: CIDRs or addresses, comma separated. Empty believes no header, and every client is counted as the address that connected, so behind a proxy every client counts as the proxy.',
  'network.clientIpHeader': 'The header a trusted proxy puts the client\'s address in: X-Real-IP, X-Forwarded-For (read from the right) or CF-Connecting-IP.',

  ssl: 'HTTPS certificates the server obtains and renews on its own.',
  'ssl.enabled': 'Serves HTTPS with certificates obtained and renewed automatically. Off when a proxy in front ends HTTPS.',
  'ssl.cacheDir': 'The folder certificates are kept in on each instance.',
  'ssl.letsencryptEmail': 'The contact address for the certificate account, which is told about renewal problems.',
  'ssl.cloudflareApiToken': 'A Cloudflare API token that proves you own the domain through DNS when a certificate is obtained.',
  'ssl.s3': 'A private bucket certificates are shared through, so several instances use one certificate instead of each obtaining its own. Separate from the S3 section.',
  'ssl.s3.enabled': 'Looks for a certificate in the bucket before obtaining one, and saves each new one there.',
  'ssl.s3.endpoint': 'The address of the S3-compatible store.',
  'ssl.s3.accessKey': 'The access key for the bucket.',
  'ssl.s3.secretAccessKey': 'The secret key for that access key.',
  'ssl.s3.region': 'The bucket\'s region. us-east-1 by default.',
  'ssl.s3.bucket': 'The bucket\'s name. Keep it private: it holds the private keys.',

  ddos: 'Blocks a client address that sends too many requests in one second. The setting applies on every instance, but each instance counts only the requests it receives itself: the limit is per instance, not across the whole server.',
  'ddos.enabled': 'Turns the guard on for every instance. Each one still counts on its own.',
  'ddos.maxRequestsPerSecond': 'The requests one client address may send to one instance in a second. A first visit to the web app is about 160, so keep it well above that.',
  'ddos.blockDuration': 'How long an instance refuses a client after it goes over, such as 1m. Other instances keep serving that client.',

  ratelimit: 'Limits the requests per client address and per signed-in user in each window. Like the DDoS guard, it applies on every instance and each instance counts on its own.',
  'ratelimit.enabled': 'Turns the limits on for every instance.',
  'ratelimit.maxPerIP': 'The requests one address may make to one instance in a window. The agents on one machine share its address.',
  'ratelimit.maxPerUser': 'The requests one signed-in user may make to one instance in a window.',
  'ratelimit.window': 'How long each window is, such as 60s.',

  smtp: 'The mail server email is sent through, such as team invitations.',
  'smtp.enabled': 'Sends email through the server below. Off, no email is sent.',
  'smtp.host': 'The mail server\'s host name.',
  'smtp.port': 'The mail server\'s port, usually 587.',
  'smtp.username': 'The user to sign in to the mail server as.',
  'smtp.password': 'The password for that user.',
  'smtp.from': 'The sender email shows, such as AgentRQ <no-reply@example.com>.',

  slack: 'Lets workspaces connect to Slack, so their tasks and agent messages show up there.',
  'slack.enabled': 'Turns the Slack app on.',
  'slack.signingSecret': 'Proves a request came from Slack. On the app\'s Basic Information page.',
  'slack.appId': 'The Slack app\'s ID.',
  'slack.clientId': 'The Slack app\'s OAuth client ID.',
  'slack.clientSecret': 'The Slack app\'s OAuth client secret.',

  webPush: 'Notifications sent to people\'s browsers and phones.',
  'webPush.vapidPublicKey': 'The public key of the VAPID key pair browsers subscribe with.',
  'webPush.vapidPrivateKey': 'The private key of that pair.',
  'webPush.subscriber': 'A contact for push services, as mailto:you@example.com or a URL.',

  storage: 'Where files are kept on disk, and for how long.',
  'storage.dir': 'The folder for attachments, and for skills when they are kept locally. Must be writable. Each instance uses its own disk, so with several instances choose s3 storage or a shared folder.',
  'storage.attachmentRetentionPeriod': 'How long task attachments are kept before they are deleted, such as 7d.',

  s3: 'An S3-compatible store, used by every section set to s3 storage.',
  's3.endpoint': 'The address of the store.',
  's3.accessKey': 'The access key for the bucket.',
  's3.secretAccessKey': 'The secret key for that access key.',
  's3.region': 'The bucket\'s region. us-east-1 by default.',
  's3.bucket': 'The bucket\'s name. With an endpoint and a bucket set, sign-in pictures are kept here too.',
  's3.publicUrl': 'The base of public links to the bucket, such as a CDN. Empty uses <endpoint>/<bucket>.',

  artifacts: 'Where task attachments are kept.',
  'artifacts.storage': 'local keeps them in the storage folder, s3 in the bucket. Every file has a public link, so with s3 the bucket must allow public reads under artifacts/.',

  skills: 'Where skill files are kept, and how large a skill may be.',
  'skills.storage': 'local keeps them in the storage folder, s3 in the bucket. Every file has a public link, so with s3 the bucket must allow public reads under skills/.',
  'skills.maxFileSize': 'The most bytes one skill file may hold. A larger file is refused, never cut short.',
  'skills.maxFiles': 'The most files one skill may have.',

  memory: 'Workspace memory: the notes agents keep between tasks.',
  'memory.maxFileSize': 'The most bytes one memory may hold, up to 131072. 0 uses the default, 16384.',

  pano: 'The Pano: markdown docs shared by every workspace of an account.',
  'pano.storage': 'local keeps them in the storage folder, s3 in the bucket. With s3 the bucket must allow public reads under pano/.',
  'pano.maxTtlDays': 'The most days a doc may live after its last save. A doc lives 30 days unless its writer asks for longer.',

  hooks: 'Hooks turn requests from outside services, such as a CI, into events.',
  'hooks.storage': 'Where hook modules are kept: local in the storage folder, s3 in the bucket under hooks/. They are never public.',
  'hooks.allowUserHooks': 'Lets every account upload hooks of its own. Off, only the built-in hooks the root user registers are available.',
  'hooks.deliveryRetention': 'How long a delivery is kept, from its arrival, for its log, a replay and spotting repeats. Such as 7d; at least 1d.',
  'hooks.maxPayloadSize': 'The most bytes one event payload a hook writes may hold, from 1024 to 1048576.',

  machines: 'Machines running agentrqd for an account.',
  'machines.maxSessions': 'The most agents one account may run on one machine. A machine page can set a lower cap, never a higher one. 0 is no cap. Counted in the database, so across all instances; agentrqd still applies its own --max-per-profile.',
  teams: 'Teams let accounts read the workspaces their members share with them. Limits here are counted across all instances.',
  'teams.maxInvitesPerDay': 'The invitations one team may create in any 24 hours, revoked ones included. 0 turns invitations off.',
  'teams.maxInvitesPerUserPerDay': 'The invitations one account may create in any 24 hours, across all its teams. 0 is no such cap.',
  'teams.emailInvites': 'Emails invitations when SMTP is set up. Off, the person inviting shares the link themselves.',
  'teams.maxSkillsPerWorkspace': 'The most skills a team workspace may hold. 0 uses the default, 100.',

  mcp: 'The MCP servers agents connect to.',
  'mcp.openaiAppsChallenge': 'The domain verification token for an OpenAI plugin submission, served at /.well-known/openai-apps-challenge. Empty serves 404.',

  sentry: 'Server panics reported to Sentry, with the server\'s version as their release and its ENV as their environment. Request bodies, cookies and auth headers are never sent.',
  'sentry.enabled': 'Reports server panics only, not log lines. Off by default.',
  'sentry.dsn': 'The Sentry project\'s DSN, such as https://<key>@o0.ingest.sentry.io/<project>. Required when enabled.',
  sentryFrontend: 'The web and desktop app\'s errors reported to Sentry, relayed through this server, with the app\'s version as their release and the server\'s ENV as their environment. Read at startup.',
  'sentryFrontend.enabled': 'Reports the app\'s errors. Off by default.',
  'sentryFrontend.dsn': 'The Sentry project\'s DSN, usually a project apart from the server\'s. Required when enabled.',

  prometheus: 'HTTP request metrics for Prometheus, served at /metrics to a scrape that carries the token.',
  'prometheus.enabled': 'Serves /metrics. Off by default.',
  'prometheus.token': 'The secret a scrape passes as /metrics?prometheus_token=<token>; any other gets 404. Exactly 32 characters. Required when enabled.',

  pubsub: 'Messages passed between the parts of one instance.',
  'pubsub.maxDurationForSubscriberToReceive': 'How long a message waits for a busy receiver to take it, such as 5s.',
}
