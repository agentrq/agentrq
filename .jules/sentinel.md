## 2025-05-14 - Path Traversal in Storage Service
**Vulnerability:** The `storage` service used `filepath.Join(baseDir, id)` without validating the `id`. This allowed an attacker to use `../` to read or write files outside the intended storage directory.
**Learning:** `filepath.Join` on its own does not prevent path traversal if one of the components is an absolute path or contains traversal elements that are not resolved relative to the base directory in a safe way.
**Prevention:** Always validate user-provided file identifiers. A robust pattern in Go is to ensure that `filepath.Base(id) == id` and that the ID is not empty or equal to `.` or `..`. This ensures the ID is a simple filename and cannot escape the base directory.

## 2026-04-30 - Hardcoded JWT Secret and Open Redirect
**Vulnerability:** The application had a hardcoded default JWT secret ("agentrq-secret-change-me") as a fallback, and the Google OAuth callback allowed redirects to any absolute URL starting with "http".
**Learning:** Fallback secrets can lead to insecure production deployments if configuration is missed. Weak validation of redirect parameters in OAuth flows is a common entry point for phishing.
**Prevention:** Remove all hardcoded security defaults; the application should fail to start if critical secrets are missing. Always validate redirect URLs against a whitelist or ensure they are local paths.

## 2024-05-20 - IDOR in Workspace Endpoints
**Vulnerability:** The `getWorkspaceToken` endpoint allowed any authenticated user to generate access tokens for any workspace by providing its ID, lacking an ownership check.
**Learning:** Endpoints that operate on specific resources must always verify that the authenticated user has the necessary permissions/ownership for that resource ID, even if they are already authenticated.
**Prevention:** Always include a user-specific identifier (e.g., `userID`) in repository/controller queries for resource-specific operations to ensure implicit authorization.

## 2024-05-21 - IDOR in Workspace Stats Endpoint
**Vulnerability:** The `GetDetailedWorkspaceStats` controller method retrieved statistics for any workspace ID provided in the request without verifying if the authenticated user owned or had access to that workspace.
**Learning:** Even when handlers correctly extract and pass the `userID`, the business logic layer must explicitly use it to authorize access to the requested resource. Simply passing it to a downstream service that ignores it leaves the application vulnerable to IDOR.
**Prevention:** Always perform an ownership check (e.g., fetching the resource with both `resourceID` and `userID`) before executing any operations on specific resources, including read-only operations like fetching statistics.

## 2025-05-22 - Stored XSS via Workspace Icon
**Vulnerability:** The `ResizeBase64` service returned the original input string if the `data:image/` prefix was missing. The CRUD controller then stored this unsanitized string in the database. Since the frontend rendered some icons using `v-html`, this allowed for Stored XSS (e.g., using `javascript:` or malicious SVG).
**Learning:** Fallback mechanisms that return unvalidated user input when processing fails are dangerous. If a service is designed to process/sanitize input, it must fail explicitly if the input doesn't meet the expected format.
**Prevention:** Enforce strict input validation (e.g., prefix checks) and remove "fallback to original" logic in data processing services. Ensure that only successfully processed and sanitized data reaches the persistence layer.

## 2025-05-23 - CSRF in Slack OAuth Flow
**Vulnerability:** The Slack OAuth flow used a predictable base62 workspace ID as the `state` parameter. This lacked cryptographic integrity and session binding, making it vulnerable to CSRF attacks where an attacker could force a user to link their Slack workspace to an arbitrary AgentRQ workspace.
**Learning:** The `state` parameter in OAuth2 is intended to be a non-guessable, session-bound value to prevent CSRF. Using a resource ID directly is insufficient.
**Prevention:** Always use cryptographically signed tokens or high-entropy random nonces for the OAuth `state` parameter. In this project, `TokenService.CreateOAuthStateToken` provides a secure, signed JWT that can carry payload (like workspace ID) while ensuring origin and integrity.

## 2026-07-18 - OAuth CSRF Vulnerability via Optional/Non-Terminal State Parameter
**Vulnerability:** The Google and GitHub OAuth callback handlers validated the cryptographically signed `state` JWT parameter, but did not make validation failure terminal. If the state token was invalid, expired, or missing, the handlers simply logged the issue, fell back to redirecting to `/`, and proceeded to successfully authenticate the user anyway, exposing users to CSRF and account linking/takeover attacks.
**Learning:** Checking or parsing a security token (like OAuth state) is useless if validation failures are not treated as terminal/aborted. Defensive programming requires that any authorization or origin-validation check fail closed and immediately abort the flow upon failure.
**Prevention:** Always validate parameters representing cryptographic origin or session proofs (such as state, nonce, or CSRF tokens) as the very first operation of a handler, and return an immediate client-side error (e.g., 403 Forbidden) to terminate execution.
