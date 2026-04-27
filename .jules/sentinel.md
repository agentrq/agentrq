## 2026-04-27 - Path Traversal in Storage Service
**Vulnerability:** Path traversal in `backend/internal/service/storage/storage.go` allowed reading/writing files outside the intended base directory.
**Learning:** Using `filepath.Join` with unsanitized user input is unsafe as it resolves `..` and absolute paths.
**Prevention:** Implement a validation helper that ensures the ID is a simple filename using `filepath.Base(id) == id` and explicitly rejects `.` and `..`.
