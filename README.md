# Couchbase Developer Platform (Couchbase-Supabase)

An open-source, modular Backend-as-a-Service (BaaS) built over **Couchbase**, providing authentication (OIDC/SAML), auto-generated REST/SQL++ APIs, real-time database subscriptions, and object storage.

## Architecture & Design Specifications
- [Design Specification](design_spec.md)
- [Functional Specification](functional_spec.md)
- [User Stories & Agile Epics](user_stories.md)

## Tech Stack
- **Language**: Go (Golang 1.22+)
- **Database**: Couchbase Server 7.x / Capella
- **API Protocol**: REST, WebSockets, gRPC
- **Storage**: S3 / MinIO compatible object storage
- **Authentication**: JWT, OIDC (Google, GitHub, Apple), SAML 2.0 (Okta, Azure AD)

## Microservices Breakdown
- `services/gateway`: API Router, SSL termination, and Rate Limiting
- `services/auth`: Identity provider with OIDC and SAML 2.0 support
- `services/data`: Auto-REST API & SQL++ translation engine with Row-Level Security
- `services/realtime`: Change Data Capture (DCP/Eventing) & WebSocket engine
- `services/storage`: S3 binary object storage & Couchbase metadata tracking
- `dashboard`: Developer web console (React / Next.js)
