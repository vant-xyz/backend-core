# Vantic Core Service

This service acts as the central execution backbone for Vantic prediction markets. It is responsible for order matching, risk evaluation, position management, and deterministic settlement. It provides robust HTTP and WebSocket interfaces to support high throughput trading operations and acts as the system of record for all market states.

## API Documentation and Health Checks

The core service exposes a comprehensive suite of authenticated and public endpoints.

- API Documentation: https://vcs-api.vantic.xyz/docs
- Core Health: https://vcs-api.vantic.xyz/health

## Technical Design and Orderbook Math

The core service is built with Go, PostgreSQL, and Redis to achieve low latency and strong data consistency. It uses an advanced in-memory matching engine that operates on an orderbook fusion model. Instead of maintaining isolated books for YES and NO outcomes, the engine mathematically links them so that the sum of the prices always equals 1.0. A bid for NO at 0.40 is automatically interpreted as an ask for YES at 0.60, creating deep, synthetic liquidity that significantly reduces spreads for traders.

Load is managed through bounded worker channels and asynchronous synchronization patterns that prevent database bottlenecks during bursty trading periods. The matching engine processes trades in-memory for maximum performance and flushes state changes to PostgreSQL for durability. Market orders calculate execution costs by iteratively sweeping the fused book across direct and complementary levels, while limit orders reserve quote balances based on the exact price and quantity. This architecture ensures that every share held by a user is fully collateralized, guaranteeing that the system can always pay out exactly 1.0 unit of the quote currency per winning share.

## Settlement and Role in the Vantic System

Settlement is binary and deterministic, localized entirely within the core service for speed and precision. When a market is resolved via a verified data provider, the engine calculates payouts based on a fixed 1.0 unit value for winning shares and 0 for losing ones. Balances are credited immediately to user accounts within the custodial system. The core service then constructs a cryptographic settlement proof and broadcasts it to the Solana blockchain. This design makes the core service the primary system of record, while the on-chain layer provides a transparent audit trail for user verification.

## Vantic System Components

The Vantic ecosystem consists of several specialized services that communicate through controlled API boundaries and high-speed messaging.

### Frontend Application
Repository: https://github.com/davidnzube101/vant-fe
The frontend is built with Next.js, React, and TypeScript to provide a seamless user interface for trading and wallet management. It communicates with the core service via REST for state changes and uses WebSocket connections for live orderbook and balance updates.

### Indexer Service
Repository: https://github.com/vant-xyz/indexer
URL: indexer-core.vantic.xyz
Health: indexer-core.vantic.xyz/health
The indexer is a high-performance Rust service that monitors the Solana and Base networks for incoming deposits to Vant user wallets. It provides a whitelist endpoint for the core service to register new custodial addresses and issues real-time callbacks to the backend when funding events are detected.

### Vantic Auxiliary Service (VAS)
Repository: https://github.com/vant-xyz/backend-auxiliary
URL: vas-api.vantic.xyz
Health: vas-api.vantic.xyz/health
The auxiliary service is built with NestJS and TypeScript to handle non-critical ecosystem tasks such as transactional email delivery and health monitoring. It integrates with the core service through internal APIs to dispatch notifications for waitlists and completed trades.

### Smart Contract
Repository: https://github.com/vant-xyz/contract
Address: 2ffqwm4YARP7DVFT3Wz2UuWzCpAPNid7L1FdrJzt5sxg
The smart contract is a native Rust program on Solana that runs on MagicBlock Ephemeral Rollups to serve as an immutable settlement log. The core service posts signed resolution outcomes to the contract, which verifies Ed25519 signatures on-chain to provide users with cryptographic proof of fair market resolution.
