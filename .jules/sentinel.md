## 2025-05-14 - Path Traversal in Storage Service
**Vulnerability:** The `storage` service used `filepath.Join(baseDir, id)` without validating the `id`. This allowed an attacker to use `../` to read or write files outside the intended storage directory.
**Learning:** `filepath.Join` on its own does not prevent path traversal if one of the components is an absolute path or contains traversal elements that are not resolved relative to the base directory in a safe way.
**Prevention:** Always validate user-provided file identifiers. A robust pattern in Go is to ensure that `filepath.Base(id) == id` and that the ID is not empty or equal to `.` or `..`. This ensures the ID is a simple filename and cannot escape the base directory.

## 2025-05-14 - Open Redirect in OAuth Callback
**Vulnerability:** The Google OAuth callback handler used the `state` parameter as a redirect destination without validating the host. This allowed an attacker to redirect users to a malicious site after successful authentication.
**Learning:** Redirect destinations from user input (like OAuth `state` or `next` parameters) must be strictly validated. Checking only for `http` prefix is insufficient. Protocol-relative URLs (starting with `//`) can bypass simple path checks and are interpreted by browsers as absolute URLs using the current protocol.
**Prevention:** Use a whitelist of allowed domains for absolute redirects. For internal redirects, ensure the path starts with a single `/` and specifically check that it does NOT start with `//`. Using `url.Parse` to verify the host matches the expected application domain is a robust defense for absolute redirects.
