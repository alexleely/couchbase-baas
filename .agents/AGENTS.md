# Couchbase Developer Platform (Couchbase-Supabase)
## Agent Guardrails & Coding Standards

This document establishes the strict coding standards and architecture rules that Antigravity (and all subagents) must follow during the development of this project.

---

## 1. Dependency & Versioning Constraints

* **Go Version**: The core Go version is locked to **Go 1.22**.
* **Downgrades Forbidden**: No 3rd-party dependencies or Go versions may be downgraded.
* **Synchronized Upgrades**: If any dependency or the core Go version is upgraded, the upgrade **must be propagated across all microservices simultaneously** (updating `go.mod` files, Dockerfiles, `.github/workflows`, and `go.work` in a single unified change).

---

## 2. Standard Service Directory Layout

We enforce a unified directory structure for all services in this monorepo, using `services/auth` as the blueprint. Every service must follow this layout:

```
services/<service-name>/
├── Dockerfile          # Multi-stage production-ready Dockerfile (locked to golang:1.22-alpine)
├── go.mod              # Clean Go module file named after its service (e.g., 'module auth')
├── cmd/
│   ├── main.go         # Application entry point, router initialization, and setup
│   └── main_test.go    # HTTP router/API handler tests and mock checks
└── internal/
    ├── db/             # Database connection, model repositories, and migration logic
    ├── models/         # Structs for JSON request/response and Couchbase storage
    └── <other>/        # Service-specific internal domain components (e.g., 'crypto' in auth)
```

---

## 3. Go Coding Standards

* **Error Handling**: 
  - Never silence errors. 
  - Wrap internal errors before bubble-up using `fmt.Errorf("context message: %w", err)`.
* **Testing**:
  - Every controller, endpoint, and library function must be accompanied by matching unit tests (`*_test.go`).
* **Environment Variables**:
  - Services must load all operational parameters (database connection strings, ports, secrets) from environment variables with fallback defaults.
* **Portability**:
  - Do not hardcode repository URLs (e.g. `github.com/alexleely/...`) for local imports. Always use the service domain-based module prefix (`auth/...`, `data/...`).
