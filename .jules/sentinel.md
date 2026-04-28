## 2025-05-14 - Path Traversal in Storage Service
**Vulnerability:** The `storage` service used `filepath.Join(baseDir, id)` without validating the `id`. This allowed an attacker to use `../` to read or write files outside the intended storage directory.
**Learning:** `filepath.Join` on its own does not prevent path traversal if one of the components is an absolute path or contains traversal elements that are not resolved relative to the base directory in a safe way.
**Prevention:** Always validate user-provided file identifiers. A robust pattern in Go is to ensure that `filepath.Base(id) == id` and that the ID is not empty or equal to `.` or `..`. This ensures the ID is a simple filename and cannot escape the base directory.
