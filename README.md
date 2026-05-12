# Vantic Core Service

The execution backbone of Vantic. Handles order matching, position management, risk evaluation, and market settlement. Exposes REST and WebSocket interfaces for the frontend and communicates with the on-chain settlement layer via the Vantic smart contract on MagicBlock Ephemeral Rollups.

- **API Docs:** https://vcs-api.vantic.xyz/docs
- **Health:** https://vcs-api.vantic.xyz/health

---

## How the Orderbook Works

Built in Go on top of PostgreSQL and Redis for low latency and strong consistency.

The matching engine runs a **fused orderbook model**. YES and NO sides are not isolated books. They are mathematically linked so their prices always sum to 1.0. A NO bid at 0.40 is automatically treated as a YES ask at 0.60. This creates deep synthetic liquidity across both sides and keeps spreads tight even in thin markets.

The engine processes trades in-memory for speed and flushes state to PostgreSQL for durability. Market orders sweep the fused book iteratively. Limit orders reserve collateral at the exact price and quantity. Every share in the system is fully backed, so the engine can always pay out exactly 1.0 unit per winning share.

Load is absorbed through bounded worker channels and async synchronization patterns that prevent database bottlenecks during bursty trading windows.

---

## Settlement

Settlement is binary and deterministic. When a market resolves via a verified data provider, winning shares pay out 1.0 unit and losing shares pay zero. Balances are credited immediately within the custodial system.

After crediting, the core service constructs a cryptographic settlement proof and posts it to the Vantic smart contract on Solana via MagicBlock Ephemeral Rollups. This gives users an on-chain audit trail. Anyone can verify that a market was resolved correctly and that payouts match what was posted on-chain.

---

## Private Withdrawals

USD balance withdrawals and SPL token withdrawals are routed through MagicBlock's private payment network. The on-chain link between a user's Vantic vault and their destination wallet is broken, preserving withdrawal privacy by default. SOL and Base asset withdrawals are direct on-chain transfers.

---

## System Components

Vantic runs as a set of specialized services with clean API boundaries.

### Frontend
`github.com/vant-xyz/frontend`

Next.js application. Connects to the core service via REST for state changes and WebSocket for live orderbook and balance updates.

### Indexer
`github.com/vant-xyz/indexer` · `indexer-core.vantic.xyz` · [health](https://indexer-core.vantic.xyz/health)

Rust service that monitors Solana (devnet and mainnet) and Base (testnet and mainnet) for incoming deposits to Vantic custodial wallets. Registers new addresses via a whitelist endpoint and fires deposit callbacks to the core service in real time.

### Auxiliary Service (VAS)
`github.com/vant-xyz/backend-auxiliary` · `vas-api.vantic.xyz` · [health](https://vas-api.vantic.xyz/health)

NestJS service handling non-critical tasks including transactional email, health monitoring, and waitlist management. Integrates with the core service through internal APIs.

### Smart Contract
`github.com/vant-xyz/contract` · `2ffqwm4YARP7DVFT3Wz2UuWzCpAPNid7L1FdrJzt5sxg`

Native Rust program on Solana running on MagicBlock Ephemeral Rollups. Acts as an immutable settlement log. The core service posts signed resolution outcomes and the contract verifies Ed25519 signatures on-chain, giving users cryptographic proof of fair market resolution.

---

## License

See [LICENSE](./LICENSE).
