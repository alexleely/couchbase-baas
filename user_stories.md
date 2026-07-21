# Couchbase Developer Platform (Couchbase-Supabase)
## Detailed User Stories, Acceptance Criteria & Sub-tasks

This document provides the complete specification for all features organized as Agile User Stories with explicit **Prerequisites**, **Acceptance Criteria**, and **Sub-tasks**.

---

## Epic 1: Infrastructure & Core Gateway

### US-1.1: Local Monorepo & Docker Orchestration
- **Status**: `[x]` Completed
- **User Story**: *As a developer, I want a monorepo structure with Docker Compose setup so that I can run Couchbase Server, API Gateway, and all microservices locally with a single command.*
- **Prerequisites**: None
- **Acceptance Criteria**:
  1. Monorepo folder layout created for `gateway`, `auth`, `data`, `realtime`, and `storage` services.
  2. Root `go.work` binds all service `go.mod` definitions.
  3. `docker-compose.yml` configures Couchbase 7.x, MinIO S3, and microservice containers.
  4. Each service exposes a working `/health` HTTP endpoint.
- **Sub-tasks**:
  - `[x]` Create service folders & `go.mod` files
  - `[x]` Create root `go.work` and `docker-compose.yml`
  - `[x]` Add `/health` stubs and Dockerfiles
  - `[x]` Create `Makefile` and `start.ps1` scripts

---

## Epic 2: Auth Service (Identity & Access)

### US-2.1: Email & Password Registration
- **Status**: `[ ]` Open
- **User Story**: *As a developer, I want users to register with an email and password so their credentials are stored securely in Couchbase.*
- **Prerequisites**: US-1.1
- **Acceptance Criteria**:
  1. `POST /auth/v1/signup` accepts JSON payload with `email` and `password`.
  2. Passwords are securely hashed using Argon2id or Bcrypt (cost factor >= 12).
  3. User profile document is stored in Couchbase `auth.users` collection with unique ID, hashed password, email, and timestamps.
  4. Duplicate email registration attempts return HTTP 409 Conflict.
- **Sub-tasks**:
  - `[ ]` Add `gocb/v2` Couchbase Go SDK dependency to `services/auth`
  - `[ ]` Create Couchbase connection and bucket/scope/collection manager
  - `[ ]` Implement Argon2id password hashing package
  - `[ ]` Implement `POST /auth/v1/signup` handler and request validation
  - `[ ]` Write unit tests for signup handler

### US-2.2: Sign In & JWT Token Issuance
- **Status**: `[ ]` Open
- **User Story**: *As a developer, I want users to sign in and receive a signed JWT access token and refresh token for API authentication.*
- **Prerequisites**: US-2.1
- **Acceptance Criteria**:
  1. `POST /auth/v1/token` accepts `email` and `password`.
  2. Credentials are verified against stored hash in Couchbase.
  3. On success, returns signed RS256/HS256 JWT `access_token` (expires in 1h) and `refresh_token` (expires in 30d).
  4. Invalid password returns HTTP 401 Unauthorized with generic error message.
- **Sub-tasks**:
  - `[ ]` Add `golang-jwt/jwt/v5` dependency
  - `[ ]` Generate RSA public/private key pairs or HMAC secret manager
  - `[ ]` Implement JWT token builder (claims: `sub`, `email`, `role`, `exp`, `iat`)
  - `[ ]` Implement `POST /auth/v1/token` handler
  - `[ ]` Store refresh token session in Couchbase `auth.sessions`

### US-2.3: OIDC Social Auth (Google, GitHub, Apple)
- **Status**: `[ ]` Open
- **User Story**: *As a frontend developer, I want users to log in using external OIDC providers via PKCE OAuth flows.*
- **Prerequisites**: US-2.2
- **Acceptance Criteria**:
  1. `GET /auth/v1/authorize?provider={provider}` redirects user to provider OAuth authorization page with PKCE `code_challenge`.
  2. `GET /auth/v1/callback` exchanges authorization code for provider tokens and fetches user profile.
  3. Creates or links Couchbase user document and issues platform JWT tokens.
- **Sub-tasks**:
  - `[ ]` Implement OAuth2 PKCE state manager
  - `[ ]` Implement Google & GitHub OIDC client handlers
  - `[ ]` Map provider claims to Couchbase user schema
  - `[ ]` Implement callback endpoint `/auth/v1/callback`

### US-2.4: SAML 2.0 Enterprise Single Sign-On (SSO)
- **Status**: `[ ]` Open
- **User Story**: *As an enterprise admin, I want my employees to log in via our corporate SAML Identity Provider (Okta, Azure AD).*
- **Prerequisites**: US-2.2
- **Acceptance Criteria**:
  1. Auth service exposes SP SAML Metadata XML at `GET /auth/v1/saml/metadata`.
  2. Handles SAML ACS POST request at `POST /auth/v1/saml/acs` from IdPs (Okta, Azure AD).
  3. Validates SAML XML assertion signature and extracts email & enterprise attributes.
  4. Issues platform JWT token on successful SAML assertion validation.
- **Sub-tasks**:
  - `[ ]` Add `crewjam/saml` library dependency
  - `[ ]` Build SAML Service Provider configuration loader from Couchbase
  - `[ ]` Implement `/auth/v1/saml/metadata` endpoint
  - `[ ]` Implement `/auth/v1/saml/acs` assertion consumer handler

### US-2.5: Token Refresh & Session Invalidation
- **Status**: `[ ]` Open
- **User Story**: *As an app user, I want to refresh my access token without logging in again, and log out securely.*
- **Prerequisites**: US-2.2
- **Acceptance Criteria**:
  1. `POST /auth/v1/token?grant_type=refresh_token` validates refresh token against Couchbase session store.
  2. If valid and unrevoked, issues a new JWT access token and rotates the refresh token.
  3. `POST /auth/v1/logout` revokes active session document in Couchbase.
- **Sub-tasks**:
  - `[ ]` Implement refresh token verification logic
  - `[ ]` Implement token rotation algorithm
  - `[ ]` Implement `/auth/v1/logout` endpoint

---

## Epic 3: Data API Service (REST & SQL++)

### US-3.1: Automatic REST Endpoint Generation
- **Status**: `[ ]` Open
- **User Story**: *As a developer, I want automatic REST CRUD endpoints generated for Couchbase collections without writing custom controllers.*
- **Prerequisites**: US-1.1
- **Acceptance Criteria**:
  1. Serves HTTP REST requests at `/rest/v1/{scope}/{collection}`.
  2. Supports `GET` (list documents), `POST` (insert), `PUT`/`PATCH` (update), `DELETE` (remove).
  3. Validates incoming scope and collection names against Couchbase metadata.
- **Sub-tasks**:
  - `[ ]` Add `gocb/v2` SDK to `services/data`
  - `[ ]` Build dynamic HTTP route handler for scope and collection parameters
  - `[ ]` Implement Couchbase SQL++ query builder for CRUD operations
  - `[ ]` Add standard HTTP error responses (404 Not Found, 400 Bad Request)

### US-3.2: URL Query Parameter to N1QL/SQL++ Translation
- **Status**: `[ ]` Open
- **User Story**: *As a frontend developer, I want to filter, sort, and paginate database queries using HTTP query parameters.*
- **Prerequisites**: US-3.1
- **Acceptance Criteria**:
  1. Translates URL params (`?select=id,name`, `?age=gte.18`, `?order=created_at.desc`, `?limit=50&offset=0`) into Couchbase SQL++ syntax.
  2. All SQL++ queries use positional/named parameters to strictly prevent SQL injection attacks.
  3. Returns JSON array of matching documents with pagination headers.
- **Sub-tasks**:
  - `[ ]` Create URL parameter AST query parser
  - `[ ]` Support operators: `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `like`, `in`
  - `[ ]` Implement SQL++ query string generator with bound parameters
  - `[ ]` Add unit tests for query translation logic

### US-3.3: Key-Value (KV) & Sub-Document Fast Path
- **Status**: `[ ]` Open
- **User Story**: *As a developer, I want direct single-document CRUD requests to bypass SQL++ and use Couchbase Key-Value (KV) API for sub-millisecond performance.*
- **Prerequisites**: US-3.1
- **Acceptance Criteria**:
  1. Single document requests (`GET /rest/v1/{scope}/{collection}/{id}`) bypass SQL++ engine and execute direct Couchbase KV get operations.
  2. Partial document updates (`PATCH /rest/v1/{scope}/{collection}/{id}`) use Couchbase Sub-Document MutateIn API.
  3. Returns sub-millisecond response latency.
- **Sub-tasks**:
  - `[ ]` Implement KV Get handler for single ID routes
  - `[ ]` Implement Sub-Document MutateIn handler for PATCH requests
  - `[ ]` Benchmark KV fast path vs SQL++ query path

### US-3.4: Row-Level Security (RLS) Policy Engine
- **Status**: `[ ]` Open
- **User Story**: *As a security engineer, I want access policies evaluated against JWT claims to prevent unauthorized data access.*
- **Prerequisites**: US-2.2, US-3.1
- **Acceptance Criteria**:
  1. Validates JWT Bearer token on incoming REST requests.
  2. Fetches RLS policy rules for collection from Couchbase `_system.policies`.
  3. Evaluates policy filter expressions (e.g. `doc.owner_id == auth.uid()`) and appends `WHERE` predicates to SQL++ queries or filters KV operations.
- **Sub-tasks**:
  - `[ ]` Create JWT middleware to inject `auth.uid()` and `auth.role()` into context
  - `[ ]` Build RLS policy rule parser and evaluator
  - `[ ]` Append RLS predicates to SQL++ AST
  - `[ ]` Reject requests returning HTTP 403 Forbidden when policy check fails

---

## Epic 4: Realtime Service (WebSockets & Mutations)

### US-4.1: WebSocket Connection & Handshake
- **Status**: `[ ]` Open
- **User Story**: *As a client SDK, I want to establish a persistent WebSocket connection to receive live database changes.*
- **Prerequisites**: US-2.2
- **Acceptance Criteria**:
  1. Exposes WebSocket endpoint at `GET /realtime/v1/websocket`.
  2. Validates JWT access token passed via query parameter or header during WS handshake.
  3. Maintains connection heartbeat (ping/pong every 30s) and closes idle connections.
- **Sub-tasks**:
  - `[ ]` Add `gorilla/websocket` dependency to `services/realtime`
  - `[ ]` Implement WS Upgrader and handshake authentication middleware
  - `[ ]` Build client connection registry and heartbeat manager

### US-4.2: Couchbase CDC / DCP Mutation Listener
- **Status**: `[ ]` Open
- **User Story**: *As a system, I want to capture Couchbase mutations in real-time using Couchbase DCP or Eventing handlers.*
- **Prerequisites**: US-4.1
- **Acceptance Criteria**:
  1. Realtime service listens to Couchbase mutations via DCP stream or Eventing HTTP webhooks.
  2. Captures document mutations (Insert, Update, Delete) with bucket, scope, collection, document ID, and body.
  3. Publishes mutation event to internal broadcast bus.
- **Sub-tasks**:
  - `[ ]` Implement Couchbase DCP client or Eventing webhook receiver endpoint
  - `[ ]` Parse mutation events into standard Event payload struct
  - `[ ]` Implement high-speed in-memory Event Bus / Channel distributor

### US-4.3: Realtime Channel Subscription & Broadcast
- **Status**: `[ ]` Open
- **User Story**: *As a client application, I want to subscribe to updates on a specific scope/collection and receive live filtered events.*
- **Prerequisites**: US-4.1, US-4.2
- **Acceptance Criteria**:
  1. Clients send subscribe message: `{ "event": "phx_join", "topic": "realtime:app-data:todos" }`.
  2. Validates client authorization policy before granting channel access.
  3. Broadcasts database mutation events to subscribed WebSocket clients in real-time.
- **Sub-tasks**:
  - `[ ]` Implement Phoenix-like channel protocol parser
  - `[ ]` Evaluate client JWT RLS policy before delivering event payload
  - `[ ]` Push JSON event frame over WebSocket connection

---

## Epic 5: Storage Service (Object Storage)

### US-5.1: File Upload & Metadata Storage
- **Status**: `[ ]` Open
- **User Story**: *As a developer, I want users to upload binary files into storage buckets with metadata tracked in Couchbase.*
- **Prerequisites**: US-2.2
- **Acceptance Criteria**:
  1. `POST /storage/v1/object/{bucket}/{path}` accepts multipart binary file uploads.
  2. Binary file is uploaded directly to MinIO/S3 object storage.
  3. File metadata (name, size, mime_type, owner_id, bucket, path) is stored in Couchbase `storage.objects` collection.
- **Sub-tasks**:
  - `[ ]` Add AWS SDK for Go v2 (`aws-sdk-go-v2/service/s3`) to `services/storage`
  - `[ ]` Implement S3 client initialization for MinIO/S3
  - `[ ]` Implement multipart upload endpoint and Couchbase metadata write

### US-5.2: Public & Private File ACL Policy
- **Status**: `[ ]` Open
- **User Story**: *As a developer, I want to configure public access or signed URL access for stored files.*
- **Prerequisites**: US-5.1
- **Acceptance Criteria**:
  1. `GET /storage/v1/object/public/{bucket}/{path}` serves public bucket objects without authentication.
  2. `GET /storage/v1/object/authenticated/{bucket}/{path}` validates JWT against file owner/ACL rules.
  3. `POST /storage/v1/object/sign/{bucket}/{path}` generates time-limited S3 presigned URLs.
- **Sub-tasks**:
  - `[ ]` Implement public object streaming endpoint
  - `[ ]` Implement ACL validation logic against Couchbase metadata
  - `[ ]` Implement S3 presigned URL generator

---

## Epic 6: Developer Console (Dashboard UI)

### US-6.1: Database Explorer & Document Editor
- **Status**: `[ ]` Open
- **User Story**: *As a developer, I want a web dashboard to browse scopes, collections, and edit JSON documents inline.*
- **Prerequisites**: US-3.1
- **Acceptance Criteria**:
  1. React/Next.js dashboard displays tree view of Couchbase buckets, scopes, and collections.
  2. Paginated table view showing document IDs and fields.
  3. Monaco JSON Editor modal allowing developers to edit document JSON and save changes back via REST API.
- **Sub-tasks**:
  - `[ ]` Set up React / Next.js web application under `/dashboard`
  - `[ ]` Build Buckets -> Scopes -> Collections navigation sidebar
  - `[ ]` Build document grid view and Monaco JSON Editor integration

### US-6.2: Auth & SSO Management Panel
- **Status**: `[ ]` Open
- **User Story**: *As an admin, I want to manage registered users, configure OIDC clients, and upload SAML metadata via the web UI.*
- **Prerequisites**: US-2.3, US-2.4
- **Acceptance Criteria**:
  1. UI dashboard for searching and viewing registered users in `auth.users`.
  2. Provider configuration form to enter OIDC Client IDs / Client Secrets (Google, GitHub).
  3. SAML SSO setup form to upload SAML IdP Metadata XML files.
- **Sub-tasks**:
  - `[ ]` Build Users table view with search and disable user actions
  - `[ ]` Build OIDC provider setup UI form
  - `[ ]` Build SAML Metadata XML file upload and configuration form
