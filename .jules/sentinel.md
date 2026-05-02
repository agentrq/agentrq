## 2025-05-14 - Path Traversal in Storage Service
**Vulnerability:** The `storage` service used `filepath.Join(baseDir, id)` without validating the `id`. This allowed an attacker to use `../` to read or write files outside the intended storage directory.
**Learning:** `filepath.Join` on its own does not prevent path traversal if one of the components is an absolute path or contains traversal elements that are not resolved relative to the base directory in a safe way.
**Prevention:** Always validate user-provided file identifiers. A robust pattern in Go is to ensure that `filepath.Base(id) == id` and that the ID is not empty or equal to `.` or `..`. This ensures the ID is a simple filename and cannot escape the base directory.

## 2026-04-30 - Hardcoded JWT Secret and Open Redirect
**Vulnerability:** The application had a hardcoded default JWT secret ("agentrq-secret-change-me") as a fallback, and the Google OAuth callback allowed redirects to any absolute URL starting with "http".
**Learning:** Fallback secrets can lead to insecure production deployments if configuration is missed. Weak validation of redirect parameters in OAuth flows is a common entry point for phishing.
**Prevention:** Remove all hardcoded security defaults; the application should fail to start if critical secrets are missing. Always validate redirect URLs against a whitelist or ensure they are local paths.

## 2025-05-15 - Open Redirect in OAuth Flow
**Vulnerability:** The MCP OAuth2 authorize handler used the `redirect_uri` parameter directly without validation, allowing attackers to redirect users to malicious domains. The Google OAuth handler also had weak prefix-based validation that could be bypassed.
**Learning:** String-prefix matching for URL validation is dangerous (e.g., `http://baseurl.com.malicious.com`). `url.Parse` should be used to explicitly verify host and scheme.
**Prevention:** Use `url.Parse` to validate redirect URLs against the application's base URL. For relative paths, explicitly block protocol-relative (`//`) and Windows-style paths (`/\`).
