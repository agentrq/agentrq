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
