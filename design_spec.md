# Couchbase Developer Platform (Couchbase-Supabase)
## Design Specification & Architecture Proposal

This document outlines the architectural design and roadmap for building an open-source Developer Platform over Couchbase, modeled after the concepts of Supabase. The goal is to provide developers with a robust backend-as-a-service (BaaS) that simplifies building applications on Couchbase by offering authentication, auto-generated REST/GraphQL APIs, realtime updates, and object storage.

---

## 1. Core Architecture Overview

To ensure scalability, modularity, and easy maintenance, the platform is designed as a set of **modular microservices** written in **Golang**. Each microservice has a single, well-defined responsibility and communicates with Couchbase and other services using lightweight protocols (gRPC or HTTP/REST).

```mermaid
graph TD
    Client[Client App / SDK] --> |HTTPS / WSS| GW[API Gateway / Router]
    
    subgraph Microservices Platform (Go)
        GW --> Auth[Auth Service / GoTrue-like]
        GW --> Data[Data API Service]
        GW --> RT[Realtime Service]
        GW --> Storage[Storage Service]
    end
    
    subgraph Storage Layer
        Auth --> |KV / SQL++| CB[(Couchbase Cluster)]
        Data --> |KV / SQL++| CB
        Storage --> |Metadata| CB
        Storage --> |Object Binary| S3[(S3 / MinIO Object Storage)]
        CB --> |DCP / Eventing / Kafka| RT
    end
```

### Modular Services Breakdown:

1. **API Gateway / Router**:
   - Acts as the single entry point for client requests.
   - Handles SSL termination, routing, CORS, rate limiting, and routes requests to corresponding services based on path (e.g., `/auth/v1/*` to Auth Service, `/rest/v1/*` to Data API Service).

2. **Auth Service (Identity Provider)**:
   - **Authentication**: Manages user sign-up, sign-in, password resets, and session management.
   - **OIDC Integration**: Implements OpenID Connect flows with PKCE. Supports logging in with identity providers (Google, GitHub, Apple, Azure AD, Okta).
   - **SAML Integration**: Implements SAML 2.0 Service Provider (SP) workflows. It reads target enterprise metadata, handles SAML Assertions, and maps enterprise user profiles to Couchbase data.
   - **JWT Issuance**: Issues signed JWT tokens containing user roles, custom claims, and metadata.
   - **Storage**: Stores user profiles, metadata, identities, and active sessions in a dedicated Couchbase scope and collection (e.g., `app-data.auth.users`).

3. **Data API Service (REST/GraphQL Engine)**:
   - Automatically inspects the Couchbase scopes/collections schema.
   - Exposes instant CRUD APIs, filtering, sorting, and pagination.
   - Translates URL parameters or GraphQL queries into N1QL/SQL++ queries or Couchbase Key-Value (KV) / sub-document operations for high-speed access.
   - Performs row-level/document-level authorization checks by validating the incoming JWT and applying policy rules stored in Couchbase.

4. **Realtime Service**:
   - Streams database modifications to client applications in realtime using WebSockets.
   - Listens to Couchbase mutations. This can be implemented via:
     - **Couchbase Database Change Protocol (DCP)**: Directly streaming mutations from the storage nodes.
     - **Couchbase Eventing**: Writing JavaScript handlers in Couchbase that trigger HTTP callbacks to the Realtime service.
     - **Kafka / RabbitMQ Connector**: Subscribing to a message queue fed by Couchbase CDC.
   - Filters mutations based on client subscriptions and authorization policies before broadcasting.

5. **Storage Service**:
   - Manages media assets, documents, and other large binary objects.
   - Stores file metadata in a Couchbase collection (e.g., file name, size, MIME type, owner, permissions).
   - Uploads binary data to S3-compatible object storage (e.g., MinIO for self-hosting, AWS S3/Capella for cloud).
   - Validates user JWTs against access policies before serving files.

---

## 2. Selected Language: Go (Golang)

Golang has been selected as the implementation language for the platform's microservices because:
* **WebSocket Efficiency**: Realtime WebSocket subscriptions consume very little memory per connection, allowing high concurrency on simple nodes.
* **Low Latency & High Performance**: Near-C performance for routing and JSON translation, critical for API gateway and proxy services.
* **Small footprint**: Lightweight containers (15-50MB RAM each) suitable for running locally during development or scaling cost-effectively.
* **SDK Maturity**: The official Couchbase Go SDK (`gocb/v2`) is highly performant and supports Key-Value ops, SQL++ queries, Full-Text Search, and transactions.

---

## 3. Feature Roadmap & User Stories

Here is the proposed list of features structured as Agile User Stories for the initial version (MVP).

### Epic 1: Auth Service (Identity Management)
* **US-1.1: User Sign Up**:
  - *As a developer, I want my users to sign up using an email and password so they can create an account in my app.*
* **US-1.2: User Sign In & JWT Issuance**:
  - *As a developer, I want my users to sign in with their credentials and receive a JSON Web Token (JWT) containing their claims and roles so they can authenticate API requests.*
* **US-1.3: OIDC Provider Integration (Google/GitHub/Apple)**:
  - *As a client app developer, I want my users to sign in using their Google, GitHub, or Apple credentials via OpenID Connect (OIDC) so that sign-in is frictionless.*
* **US-1.4: SAML 2.0 Single Sign-On (SSO)**:
  - *As an enterprise application administrator, I want to authenticate users via our corporate SAML Identity Provider (IdP) (e.g., Okta, Azure AD) so that users do not need separate passwords.*
* **US-1.5: User Management in Couchbase**:
  - *As an administrator, I want user accounts and session states to be securely stored in a Couchbase collection (`auth.users`) for fast lookups and high availability.*

### Epic 2: Data API Service (CRUD Engine)
* **US-2.1: Automatic REST Endpoint Generation**:
  - *As a developer, I want the platform to automatically expose REST endpoints (`/rest/v1/{collection}`) for my Couchbase collections so that I don't have to write custom CRUD controllers.*
* **US-2.2: Document Querying & Filtering**:
  - *As a frontend developer, I want to query documents using HTTP query parameters (e.g., filters, sorting, limits) so that I can fetch custom datasets without writing SQL++ queries manually.*
* **US-2.3: Security Policies (Row-Level Security)**:
  - *As a developer, I want to define access policies on Couchbase collections based on user JWT claims (e.g., `owner_id == jwt.sub`) so that users can only access their own data.*

### Epic 3: Realtime Service (Live Database Streams)
* **US-3.1: WebSocket Connection**:
  - *As a frontend developer, I want to establish a persistent WebSocket connection to the Realtime service so that I can receive live database events.*
* **US-3.2: Collection Subscription**:
  - *As a frontend developer, I want to subscribe to updates on a specific Couchbase collection or document so that my UI updates instantly when a document is created, updated, or deleted.*

### Epic 4: Developer Console (Dashboard)
* **US-4.1: Database Explorer**:
  - *As a developer, I want a web dashboard where I can view, create, edit, and delete Couchbase scopes, collections, and documents.*
* **US-4.2: Auth Configuration Panel**:
  - *As a developer, I want to configure authentication settings, OIDC client keys, and SAML metadata endpoints via the dashboard.*

---

## 4. GitHub Project Setup Plan

To track and manage the implementation, we will create a GitHub Repository and a GitHub Project.

### Proposed GitHub Repository Structure:
* **Organization/Repository**: `alexleely/couchbase-baas` (or custom name)
* **Monorepo**: We will use a Monorepo structure (`/services/auth`, `/services/data`, `/services/realtime`, `/services/gateway`, `/dashboard`) to keep the codebase unified.

### Action Plan:
1. **GitHub Auth Check**: Guide the user to authenticate the GitHub CLI locally.
2. **Repository Creation**: Initialize a git repository locally, commit the design spec and initial files, and push/create the repository under `alexleely`'s GitHub account using `gh repo create`.
3. **GitHub Project & Issue Creation**: Create a GitHub Project and populate it with issues based on the user stories using the GitHub CLI.
