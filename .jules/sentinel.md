# Sentinel's Journal - Critical Security Learnings

This journal contains only critical security learnings discovered during the protection of the AgentRQ codebase.

## 2025-05-14 - Path Traversal in Storage Service
**Vulnerability:** The `storage` service allowed arbitrary file access via the `id` parameter because it directly used `filepath.Join(s.baseDir, id)` without validating that `id` was a simple filename.
**Learning:** Even with `filepath.Join`, an attacker can use `..` to traverse outside the base directory.
**Prevention:** Always validate that user-provided IDs used in file paths do not contain path separators. A robust check is ensuring `filepath.Base(id) == id` and explicitly rejecting empty or dot-only IDs.
