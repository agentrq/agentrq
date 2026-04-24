## 2025-02-14 - [Path Traversal in Storage Service]
**Vulnerability:** The storage service used `filepath.Join(baseDir, id)` without validating the `id` parameter, allowing for path traversal (e.g., `id = "../../etc/passwd"`).
**Learning:** Even when using `filepath.Join`, it's crucial to validate that the resulting path is still within the intended directory. `filepath.Base(id) == id` is a good first check for simple filename IDs.
**Prevention:** Always use a helper like `fullPath` that validates the `id` is a simple filename and doesn't contain path separators before joining it with a base directory.
