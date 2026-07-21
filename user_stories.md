# Couchbase Developer Platform (Couchbase-Supabase)
## Feature User Stories & Epic Breakdown

This document provides a comprehensive list of features in Agile User Story format for the Couchbase Developer Platform.

---

## Epic 1: Infrastructure & Core Gateway
* **US-1.1: Local Monorepo & Docker Orchestration**
  - **User Story**: *As a developer, I want a monorepo structure with Docker Compose setup so that I can run Couchbase Server, API Gateway, and all microservices locally with a single command.*
  - **Acceptance Criteria**: Running `docker-compose up` initializes Couchbase 7.x, Traefik/Envoy API Gateway, and stubbed microservices.

---

## Epic 2: Auth Service (Identity & Access)
* **US-2.1: Email & Password Registration**
  - **User Story**: *As a developer, I want users to register with an email and password so their credentials are stored securely in Couchbase.*
  - **Acceptance Criteria**: Expose `POST /auth/v1/signup`, hash passwords using Argon2id/Bcrypt, store user profiles in `auth.users` collection in Couchbase.

* **US-2.2: Sign In & JWT Token Issuance**
  - **User Story**: *As a developer, I want users to sign in and receive a signed JWT access token and refresh token for API authentication.*
  - **Acceptance Criteria**: Expose `POST /auth/v1/token`, issue signed RS256/HS256 JWTs with user ID, role, and expiration.

* **US-2.3: OIDC Social Auth (Google, GitHub, Apple)**
  - **User Story**: *As a frontend developer, I want users to log in using external OIDC providers via PKCE OAuth flows.*
  - **Acceptance Criteria**: Expose `GET /auth/v1/authorize?provider=google`, handle OAuth redirects/callbacks, exchange code, and map attributes to Couchbase user documents.

* **US-2.4: SAML 2.0 Enterprise Single Sign-On (SSO)**
  - **User Story**: *As an enterprise admin, I want my employees to log in via our corporate SAML Identity Provider (Okta, Azure AD).*
  - **Acceptance Criteria**: Provide SAML SP metadata, handle assertions at `POST /auth/v1/saml/acs`, extract enterprise claims, and issue platform JWTs.

* **US-2.5: Token Refresh & Session Invalidation**
  - **User Story**: *As an app user, I want to refresh my access token without logging in again, and log out securely.*
  - **Acceptance Criteria**: Expose `POST /auth/v1/token?grant_type=refresh_token` and `POST /auth/v1/logout` to revoke session documents in Couchbase.

---

## Epic 3: Data API Service (REST & SQL++)
* **US-3.1: Automatic REST Endpoint Generation**
  - **User Story**: *As a developer, I want automatic REST CRUD endpoints generated for Couchbase collections without writing custom controllers.*
  - **Acceptance Criteria**: Automatically route `GET`, `POST`, `PUT`, `PATCH`, and `DELETE` requests under `/rest/v1/{scope}/{collection}` to Couchbase operations.

* **US-3.2: URL Query Parameter to N1QL/SQL++ Translation**
  - **User Story**: *As a frontend developer, I want to filter, sort, and paginate database queries using HTTP query parameters.*
  - **Acceptance Criteria**: Support `?select=`, `?eq=`, `?gte=`, `?order=`, `?limit=` query params and translate them safely into parameterized Couchbase SQL++ queries.

* **US-3.3: Key-Value (KV) & Sub-Document Fast Path**
  - **User Story**: *As a developer, I want direct single-document CRUD requests to bypass SQL++ and use Couchbase Key-Value (KV) API for sub-millisecond performance.*
  - **Acceptance Criteria**: Direct document lookup/mutation by ID uses `gocb` Sub-Document or KV operations.

* **US-3.4: Row-Level Security (RLS) Policy Engine**
  - **User Story**: *As a security engineer, I want access policies evaluated against JWT claims to prevent unauthorized data access.*
  - **Acceptance Criteria**: Evaluate JSON/SQL++ policy expressions per role (e.g. `doc.owner_id == auth.uid()`) before returning query results or mutating documents.

---

## Epic 4: Realtime Service (WebSockets & Mutations)
* **US-4.1: WebSocket Connection & Handshake**
  - **User Story**: *As a client SDK, I want to establish a persistent WebSocket connection to receive live database changes.*
  - **Acceptance Criteria**: Expose `GET /realtime/v1/websocket`, validate JWT during handshake, and send heartbeat ping/pongs.

* **US-4.2: Couchbase CDC / DCP Mutation Listener**
  - **User Story**: *As a system, I want to capture Couchbase mutations in real-time using Couchbase DCP or Eventing handlers.*
  - **Acceptance Criteria**: Realtime service receives document mutation events (Insert, Update, Delete) with scope, collection, and payload.

* **US-4.3: Realtime Channel Subscription & Broadcast**
  - **User Story**: *As a client application, I want to subscribe to updates on a specific scope/collection and receive live filtered events.*
  - **Acceptance Criteria**: Clients subscribe to `channel:scope:collection`. Mutations are checked against access policies and broadcasted over WebSockets.

---

## Epic 5: Storage Service (Object Storage)
* **US-5.1: File Upload & Metadata Storage**
  - **User Story**: *As a developer, I want users to upload binary files into storage buckets with metadata tracked in Couchbase.*
  - **Acceptance Criteria**: Expose `POST /storage/v1/object/{bucket}/{path}`, stream file to S3/MinIO, store metadata in `storage.objects` collection.

* **US-5.2: Public & Private File ACL Policy**
  - **User Story**: *As a developer, I want to configure public access or signed URL access for stored files.*
  - **Acceptance Criteria**: Expose endpoints for downloading public objects or generating signed URLs for private objects.

---

## Epic 6: Developer Console (Dashboard UI)
* **US-6.1: Database Explorer & Document Editor**
  - **User Story**: *As a developer, I want a web dashboard to browse scopes, collections, and edit JSON documents inline.*
  - **Acceptance Criteria**: React-based dashboard listing buckets/scopes/collections with a Monaco editor for document updates.

* **US-6.2: Auth & SSO Management Panel**
  - **User Story**: *As an admin, I want to manage registered users, configure OIDC clients, and upload SAML metadata via the web UI.*
  - **Acceptance Criteria**: Dashboard UI for viewing users, configuring OAuth credentials, and managing SAML SSO connections.
