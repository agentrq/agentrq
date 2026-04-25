## 2025-02-14 - [Path Traversal in Storage Service]
**Vulnerability:** Path traversal in `backend/internal/service/storage/storage.go` allowed reading, writing, and deleting files outside the base directory.
**Learning:** `filepath.Join` does not sanitize its inputs for traversal elements like `../`.
**Prevention:** Use a validation helper that ensures `filepath.Base(id) == id` and explicitly block `.` and `..` before joining with a base directory.
