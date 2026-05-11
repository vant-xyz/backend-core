# Jupiter Developer Experience Report: Vantic Treasury Rebalancing Engine

**Submitted by:** David (Lead Dev / Co-Founder, Vantic)

**Email:** david.nzube.official22@gmail.com

**Date:** May 11, 2026

**Project:** Vantic Autonomous Treasury Rebalancing Engine

**Sidetrack Pitch Video:** https://youtu.be/dCsqiyP7txk?si=IAQE3fIkKJi-iJLO

---

## 1. Project Overview

Vantic is a prediction market protocol on Solana, built on MagicBlock Ephemeral Rollups. Our treasury holds a mix of volatile assets (SOL, ETH) and stablecoins (USDC, USDT, USDG, PUSD), and we needed it to rebalance automatically without anyone manually stepping in.

The solution lives in `services/jupiter.go`, a rebalancing engine in our Go backend powered by two Jupiter APIs:

- **Jupiter Price V3** -- our source of truth for pricing volatile holdings in real time.
- **Jupiter Swap V2** (`/swap/v2/quote`) -- what triggers a swap when a volatile asset crosses the threshold.

The result is a treasury that actively protects protocol liquidity and user settlements, rather than passively holding assets.

---

## 2. DX Report Questions

### How was onboarding?

Generally smooth. The migration away from the old org structure was clean and the path forward felt deliberate. One thing did trip me up early: a GitHub OAuth issue during authentication forced a fallback to email login. Not a blocker, but tightening that flow would spare developers an unnecessary headache at the start.

### What is broken or missing in the docs?

The biggest gap is a downloadable OpenAPI/Swagger spec. For Go backends, machine-readable specs are not a nice-to-have -- they are how we generate clients and enforce type safety. Without one, you are stuck manually mapping endpoints, which is slow and error-prone. That gap alone probably cost us hours we did not need to lose.

The high-level docs are solid, but there is very little for teams running backend-heavy workflows. More real-world integration patterns for that use case would go a long way.

### Where did the APIs bite you?

The Price V3 endpoint gave us bad latency early on that we could not immediately explain. We eventually traced it to our local ISP intermittently blocking requests to the Jupiter endpoint. Not Jupiter's fault, but it points to a real infrastructure concern. Routing through Cloudflare or offering globally distributed edge nodes would make a meaningful difference for developers testing locally or running on mixed VPS infrastructure.

### Did you use the AI stack?

The Docs MCP was the highlight of the whole integration. It just worked, and it is genuinely well-suited for agents that cannot traverse the filesystem.

The in-chat Docs AI was a different story. It hallucinated API parameters and endpoint behavior more than once. It felt like it was pattern-matching off generic knowledge rather than the actual live Jupiter API. It needs to be grounded in what the API does today, not what a language model expects APIs to look like.

---

## 3. What I Wish Existed and How I Would Rebuild the Platform

### What I wish existed

**Machine-readable specs.** An OpenAPI/JSON spec is the baseline for serious engineering teams. Without it, integration is slower and more fragile than it needs to be.

**A Go SDK.** Raw API calls work, but an official maintained SDK for Swap V2 and Price V3 would meaningfully cut integration time and reduce the surface area for mistakes. I'm building this.

### How I would rebuild the developer platform

**Start with the spec.** Generate the entire platform from a single canonical OpenAPI spec, updated in CI/CD. Make it the first download on the doc. Everything else flows from that.

**Provide a real local testing setup.** A mock server or local testnet shim emulating Jupiter's API behavior would eliminate the ISP and latency issues during development entirely. It would also allow deterministic unit tests for our rebalancing logic, which right now we cannot write cleanly.

**Fix the AI with RAG.** Constrain the Docs AI strictly to the canonical documentation. If it cannot find an answer, it should say so and point the developer to Discord or a GitHub issue, not produce a confident guess. A wrong answer costs more time than no answer.

---

Thank You
