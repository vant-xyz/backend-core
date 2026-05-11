# Jupiter Developer Experience Report: Vantic Treasury Rebalancing Engine

**Submitted by:** David [Lead Dev/Co-Founder, Vantic]
**Developer Email:** david.nzube.official22@gmail.com
**Date:** May 11, 2026
**Project:** Vantic Autonomous Treasury Rebalancing Engine

---

## 1. Project Overview
Vantic is a high-performance prediction market protocol on Solana, utilizing MagicBlock Ephemeral Rollups for settlement. Our core technical challenge involves managing a multi-asset treasury containing volatile (SOL, ETH) and stable (USDC, USDT, PUSD) assets without manual intervention.

To solve this, we implemented an **Autonomous Rebalancing Engine** (within `be/services/jupiter.go`) in our Go backend. We integrated:
- **Jupiter Price V3 API:** Used as the ground truth for valuing all volatile holdings in real-time.
- **Jupiter Swap V2 (`/swap/v2/quote`):** Used to autonomously trigger swaps when volatile asset holdings exceed a configurable USD threshold (set via `VANTIC_DUMP_THRESHOLD_USD`).

This integration transforms our treasury from a passive holding tank into an autonomous, risk-aware rebalancing machine that protects protocol liquidity and user settlements.

---

## 2. Developer Experience (DX) Feedback

### Onboarding
The onboarding process was seamless. The transition from the legacy organization structure was handled cleanly, and the migration path felt professional.
- **Friction Point:** I experienced a minor issue connecting my GitHub account during authentication, necessitating a fallback to `david.nzube.official22@gmail.com`. Streamlining third-party OAuth providers would improve initial velocity.

### Documentation
- **Missing OpenAPI Spec:** The most significant gap is the lack of a downloadable OpenAPI/Swagger spec (YAML/JSON). We heavily rely on these for type-safety and client generation in our Go backend. Manually mapping endpoints is prone to error and time-consuming.
- **Content Gaps:** The documentation is excellent at a high level but lacks deep-dive integration patterns for backend-heavy workflows.

### API Performance & Reliability
- **Price API Latency:** Initial integration with the Price V3 endpoint experienced severe latency when accessed from local infrastructure. We identified that our local ISP was intermittently blocking requests to the Jupiter endpoint. 
- **Recommendation:** Jupiter should consider routing these requests via Cloudflare or providing globally distributed edge nodes to ensure reliability for developers testing locally or running on varied VPS infrastructure.

### AI Stack Feedback
- **Docs MCP:** The experience was seamless and highly productive. It is a powerful tool for agents that cannot traverse the filesystem, and I encountered no issues here—it was the highlight of the integration process.
- **Integrated Docs AI:** At points, the chat-based docs AI hallucinated API parameters or endpoint behavior. It appears to lack deep "product context" and relies too heavily on generic patterns. It needs to be tuned to the actual live state of the Jupiter API.

---

## 3. The "What I Wish Existed" & Rebuild Strategy

### What I wish existed
1. **Machine-Readable Specs:** Standard OpenAPI/JSON specs are non-negotiable for serious engineering teams to ensure fast, safe integration.
2. **SDK Support:** While raw API calls work, official, well-maintained Go SDKs for Swap V2/Price V3 would be a massive productivity multiplier.

### How I would rebuild the Developer Platform
1. **Spec-First:** The developer platform should be generated from a single, canonical OpenAPI spec that is updated in CI/CD. Providing this as a downloadable artifact should be the first call-to-action on the dashboard.
2. **"Production-Ready" Local Testing:** Provide a standardized local test-net shim or a mock-server that emulates Jupiter's API behavior. This eliminates ISP/latency issues during development and allows for deterministic unit testing of our treasury rebalancing logic.
3. **Contextual AI:** The Docs AI needs a RAG implementation that is strictly constrained to the canonical API documentation. If the AI cannot find a specific answer in the docs, it should direct the developer to a human-staffed Discord or GitHub issue, rather than guessing.
4. **Visibility/Analytics:** Give us a "Project-Level Dashboard" that shows exactly how our backend is hitting your APIs—latency logs, error rates, and quota usage. Seeing where our triggers are failing (e.g., 429s or latency spikes) would allow us to optimize our rebalancing engine on the fly.

---

## 4. Implementation Media
*(See `/be/media/` for screenshots of the treasury balance and autonomous rebalancing flow executed by the Vantic Engine)*
