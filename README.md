# Vantic Core Service

This service acts as the central execution backbone for Vantic prediction markets. It is responsible for order matching, risk evaluation, position management, and deterministic settlement. It provides robust HTTP and WebSocket interfaces to support high throughput trading operations and acts as the system of record for all market states.

## API Documentation and Health Checks

API Documentation: https://vcs-api.vantic.xyz/docs
Core Service Health: https://vcs-api.vantic.xyz/health

## Core Service Architecture

The core service is built with Go, PostgreSQL, and Redis to achieve low latency and strong data consistency. It uses a decoupled architecture where read heavy operations for market discovery and orderbook states are separated from state mutation paths. 

Load is managed through bounded worker channels and background synchronization patterns that prevent database bottlenecks during bursty trading periods. The system is designed to absorb sudden spikes in trading activity by utilizing an in-memory execution model where the matching engine operates rapidly over active market books, flushing critical state changes asynchronously to PostgreSQL for durability. Redis acts as a fast caching layer and pub/sub broker. WebSocket fan out is heavily optimized to keep clients updated with real-time price and balance changes, effectively eliminating polling pressure on the core REST endpoints.

## Orderbook Design and Execution Math

The matching engine employs an in-memory orderbook hub that handles side-aware depth and strict price-time priority execution. The orderbook tracks YES and NO-sided liquidity independently, maintaining a spread and computing executable depth in real-time. 

When a user places an order, the system differentiates between limit and market executions. Limit orders reserve quote balances directly by locking exact funds (price * quantity). Market orders calculate expected execution costs iteratively; the engine traverses the orderbook depth, sweeping through available asks level by level, to estimate the precise spend required to fill the requested volume. This math ensures that users never overcommit funds and that slippage is accurately modeled before execution. Risk controls enforce these balance locks and liquidity bounds strictly before entering execution paths, minimizing downstream failures and ensuring that partial fills maintain precise accounting correctness.

## Settlement Mechanics

Settlement in Vantic is fully deterministic and mathematically transparent. When a market expires and the underlying data provider (e.g., Coinbase) confirms the target price, the backend calculates the outcome. 

Winning positions are transitioned at fixed payout multipliers (e.g., a 1.9x payout rate for correct predictions, accounting for the house edge), and balances are credited immediately. Losers' funds are retained. Following the off-chain balance updates, the core service constructs a cryptographic settlement message (including the end price, the outcome, and a secure Ed25519 signature). This payload is then broadcast to the Solana blockchain, acting as an immutable settlement log. This dual-layer approach guarantees that fast, frictionless payouts occur off-chain, while the cryptographic proof on-chain ensures absolute transparency and exact accounting precision.

## Vantic System Components

The Vantic ecosystem relies on several interconnected services to deliver a complete prediction market experience.

### Frontend Application
Repository: https://github.com/davidnzube101/vant-fe
The frontend is built with **Next.js, React, and TypeScript**, styled with Tailwind CSS. It provides the user interface for market discovery, wallet management, and real-time trading. It integrates directly with the core service via REST for authentication and state changes, and utilizes WebSocket connections for live orderbook updates.

### Indexer Service
Repository: https://github.com/vant-xyz/indexer
URL: https://indexer-core.vantic.xyz
Health: https://indexer-core.vantic.xyz/health
The indexer is a specialized service built in **Rust** that monitors the Solana and Base networks for incoming deposits to Vant user wallets. It subscribes to on-chain WebSocket RPC streams and issues real-time callbacks to the core Go backend to finalize funding operations. This asynchronous flow allows the core service to recognize external capital without maintaining heavy RPC node connections itself.

### Vantic Auxiliary Service (VAS)
Repository: https://github.com/vant-xyz/backend-auxiliary
URL: https://vas-api.vantic.xyz
Health: https://vas-api.vantic.xyz/health
The auxiliary service is built with **NestJS and TypeScript**. It handles non-execution responsibilities such as transactional email delivery (waitlists, transaction notifications) and ecosystem health monitoring. It communicates with the core backend through internal APIs and shared infrastructure to manage user notifications and administrative alerts asynchronously.

### Smart Contract
Repository: https://github.com/vant-xyz/contract
Address: `2ffqwm4YARP7DVFT3Wz2UuWzCpAPNid7L1FdrJzt5sxg`
The smart contract is a native **Rust** program running on **MagicBlock Ephemeral Rollups** to provide high-performance cryptographic proof of fair market resolution. It acts as an immutable settlement log where the core Go backend posts signed resolution outcomes. The program verifies Ed25519 signatures on-chain via the instructions sysvar and utilizes delegation/undelegation patterns for optimized transaction processing. It does not hold funds, serving entirely as a transparent audit layer.
