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

## 2026-07-03 - CSRF in Slack OAuth and Lax State Validation
**Vulnerability:** The Slack OAuth flow used a raw, unverified workspace ID as the `state` parameter, making it vulnerable to CSRF. Additionally, Google/GitHub OAuth callbacks lacked strict state validation, silently ignoring errors and defaulting to `/`.
**Learning:** The `state` parameter in OAuth flows MUST be cryptographically bound to the user session or a specific resource and verified upon callback. Defaulting to success when security parameters are missing or invalid creates a weak security posture.
**Prevention:** Use signed JWTs for the `state` parameter to carry and verify situational metadata (like `workspaceID`). Ensure callbacks strictly validate these tokens and return explicit error codes (e.g., `403 Forbidden`) on failure.
