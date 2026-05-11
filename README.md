# Vantic Core Service (`/be`)

This service is the core backend for Vantic markets and trading. It exposes authenticated and public APIs, manages balances and orders, coordinates settlement, and acts as the system-of-record API consumed by web and auxiliary services. It also provides real-time streams over WebSocket for prices, orderbook activity, and balance updates.

## API and Health

- API docs: https://vcs-api.vantic.xyz/docs
- Health: `GET /health` on `https://vcs-api.vantic.xyz/health`

## Core Service Deep Dive

The core service is implemented in Go with Gin and PostgreSQL, with Redis used for low-latency order state and matching flow support. Order placement paths validate market status, price/quantity constraints, risk boundaries, and balance lock requirements before entering matching and persistence workflows. The matching subsystem maintains market books with side-aware depth and fill handling, while balances are guarded through lock, unlock, and credit semantics to preserve accounting correctness during partial and complete fills. Settlement logic finalizes outcomes, computes payouts deterministically, updates position state, credits winners, and exposes onchain-verifiable references for auditability.

From a load perspective, the service separates read-heavy market/orderbook access from state mutation paths, uses background sync patterns for database durability, and relies on bounded channel/worker style flows in market hubs and matching components. WebSocket fan-out reduces polling pressure and keeps clients synchronized with near-real-time balance and market changes. Risk and liquidity controls are enforced before execution to reduce downstream failure churn and preserve predictable behavior under bursty traffic. The architecture is designed so critical trading rules live in one place, while external services integrate through clear HTTP boundaries.

## Orderbook and Math Model

The orderbook tracks YES/NO-sided liquidity and computes executable depth by price-time priority. Market order cost estimation iterates asks to calculate expected spend based on available levels, while limit orders reserve quote balances directly from `price * quantity`. Fill paths update filled quantity, remaining quantity, and position average entry cost, then settle user-level PnL through payout and realized accounting fields. In binary resolution, winning positions settle at fixed payout-per-share semantics, which keeps settlement math transparent and easy to verify.

## Role in the Vantic System

This service is the trading and market execution backbone for Vantic. Frontend clients use it for auth, market discovery, order placement, position management, and websocket updates. Admin and automation flows use it to create markets, trigger sync, and execute settlement. Indexer and auxiliary services consume and contribute data/events through controlled integration endpoints and keys.

## Service Map and Integrations

### Frontend (`vant-fe`)
- Repo: https://github.com/davidnzube101/vant-fe
- Language: TypeScript (Next.js/React)
- Role: User-facing web app for trading, market views, wallet/account UX, and real-time subscriptions.
- Integration: Calls this core service over REST and WebSocket using auth tokens and API proxy routes.

### Indexer (`indexer`)
- Repo: https://github.com/vant-xyz/indexer
- URL: https://indexer-core.vantic.xyz
- Health: `GET /health` on `https://indexer-core.vantic.xyz/health`
- Language: Go
- Role: Tracks onchain state and supports indexed views required by Vantic market operations.
- Integration: The core service notifies/queries indexer-facing paths for wallet and market-related workflows.

### VAS (`backend-auxiliary`)
- Repo: https://github.com/vant-xyz/backend-auxiliary
- URL: https://vas-api.vantic.xyz
- Health: `GET /health` on `https://vas-api.vantic.xyz/health`
- Language: Go
- Role: Auxiliary backend features and background service responsibilities outside core execution.
- Integration: Communicates with the core API and shared infrastructure to support non-matching critical paths.

### Contract (`contract`)
- Repo: https://github.com/vant-xyz/contract
- Language: Rust/Anchor (onchain programs)
- Role: Onchain market primitives and settlement-verifiable state transitions.
- Integration: Core service orchestrates market lifecycle around contract interactions and stores resulting tx metadata.
- Contract address: See the contract repository README for the canonical deployed address.

## Project Layout (`/be`)

- `main.go`: service bootstrap, routing, middleware, lifecycle wiring.
- `handlers/`: HTTP layer and request/response contracts.
- `services/`: business logic for markets, auth, balances, settlement, integrations.
- `db/`: queries, migrations, and persistence abstractions.
- `models/`: typed domain models and JSON schema shapes.
- `docs/swagger.yaml`: API specification source.

## Private Repository Notice

This repository is private Vantic business code. Access is for approved collaboration and review only, including hackathon evaluators invited by the Vantic team. No reuse rights are granted outside the custom license in this directory.
