# Module: Repo-Papers

Route `/repo-papers` · Roles: user, admin (sources/tokens: admin) · Backend: `internal/repopapers` · API: [`/repopapers/*`](../API.md#repo-papers)

From the sketch: *"Search and organise the best repos of the week, month, 3 months, 6 months (more stars **and growing**) by topic"* and *"Search and organise papers from reliable sources — of interest / important / that people are talking about."*

Packaged as a **built-in first-party module** that uses the same internal module API as everything else (it is not delivered as a container add-on; it appears in the sidebar next to Add-ons). The Add-ons framework remains available for third-party equivalents.

## 1. Views

| View | Content |
|---|---|
| **Repos** | Window tabs `7 d · 30 d · 90 d · 180 d`; topic filter chips; sort `Trending` (default) / `Stars` / `New`; card per repo: name, description, language, topics, ★ total, **★ +Δ in window** (sparkline when snapshots exist), license, last push, link |
| **Papers** | Same windows and topic chips; sort `Hot` (default) / `Cited` / `New`; card: title, authors, venue/source badge (`preprint` vs `peer-reviewed`), date, abstract (collapsed), signals (HF upvotes, citations, citation velocity), links (abs/pdf/code repo when known) |
| **Topics** | CRUD of user topics (see §3) |
| **Sources** | Enable/disable sources, GitHub token status, rate-limit state, last refresh, errors (admin) |
| **Why this rank?** | Drawer on each item showing the score breakdown — ranking is transparent and tunable |

User state per item: *saved*, *hidden*, *read* (stored per user in SQLite).

## 2. Sources and reliability tiers

| Source | Use | API | Auth | Trust tier |
|---|---|---|---|---|
| GitHub | repos, stars, topics, license | REST Search + **GraphQL** (batched snapshots) | optional PAT (strongly recommended: 5 000 req/h vs 60) | – |
| arXiv | preprints by category (e.g. `cs.LG`, `cs.CL`, `cs.CV`, `cs.DC`) | Atom API (≤ 1 req / 3 s per policy) | none | T2 (preprint) |
| Semantic Scholar | citations, influential citations, venue | Graph API | optional key | T1 signal provider |
| Hugging Face Daily Papers | community attention ("people are talking about it") | public JSON | none | attention signal |
| OpenAlex | venue/peer-review metadata, citation counts | REST | none (polite pool email) | T1 signal provider |

*Reliability tiers (configurable allow-list):* **T1** = peer-reviewed venue on the allow-list (e.g. NeurIPS, ICML, ICLR, ACL/EMNLP, CVPR, Nature/Science family, USENIX/OSDI/SOSP, …); **T2** = arXiv preprint (labelled as *not peer-reviewed*); **T3** = anything else (hidden by default). Papers Yubeos cannot place in a tier are never boosted by "reliability".

(Papers With Code is intentionally not a source: the service was folded into Hugging Face; its data is reachable through HF.)

## 3. Topics

A **topic** is a saved query: `{id, name, githubTopics[], keywords[], languages[], arxivCategories[], minStars}`. Built-ins (editable, deletable): *AI/ML, LLM tooling, Self-hosting, Homelab/NAS, Rust, Go, DevOps, Security, Data engineering, Systems*. Matching for repos: any GitHub topic ∈ `githubTopics` OR keyword match in name/description/topics (case-insensitive, word-boundary). Matching for papers: arXiv category ∈ set OR keyword in title/abstract. Cheap deterministic matching — no ML dependency in 1.0.

## 4. Ranking (deterministic, documented)

**Repos.** For window *W* and a tracked repo *r*:

```
Δstars(W)  = stars_now − stars_at(now − W)          # from local snapshots
growth     = Δstars / max(stars_at(now − W), 50)     # relative growth, floor avoids tiny-base noise
score_trend = 0.55·norm(log1p(Δstars)) + 0.25·norm(growth) + 0.10·norm(log1p(forks Δ)) + 0.10·recency(pushedAt)
score_stars = norm(log1p(stars_now))                  # for the "Stars" sort
filters: not archived, not fork (configurable), minStars ≥ 50, pushedAt within 2·W, spam heuristics (see below)
```

`norm` = rank-percentile within the candidate set (robust to outliers). Weights are config (`repopapers.ranking.repo`), defaults above.

**Snapshots.** Daily GraphQL batch (aliased queries, 100 repos/request) for the **tracked set** = union of (a) top candidates from GitHub Search per topic/window, (b) repos already seen in the last 180 d, (c) user-saved. Cap 10 000 repos. Snapshot row: `(repo_id, date, stars, forks, open_issues, pushed_at)`.

**Cold start** (no snapshot history for *W*): fall back to *New-in-window* ranking via Search (`created:>=DATE sort:stars`) plus star-history estimation is **not** attempted (no API for it); UI labels such lists "estimated until N days of history exist" and the module back-fills naturally. Optional bootstrap import from a public aggregate is a later, clearly-labelled feature.

**Spam/quality heuristics:** stars-to-forks and stars-to-watchers anomaly, repo age < 3 d with > 1 000 stars, awesome-list/clone patterns (configurable deny-patterns), single-commit repos.

**Papers.**

```
score_hot = 0.35·norm(log1p(hf_upvotes))              # attention
          + 0.30·norm(log1p(citation_velocity))        # citations per month since publication (Semantic Scholar/OpenAlex)
          + 0.15·tier_bonus(T1=1.0, T2=0.5)            # reliability
          + 0.10·norm(github_stars_of_linked_repo)     # if a code repo is linked
          + 0.10·recency_decay(published, half-life 21 d)
```

For windows ≤ 30 d citations are sparse, so the effective weights are renormalised toward attention + recency (formula switch recorded in the breakdown). All inputs and weights appear in the *Why this rank?* drawer.

## 5. Scheduler, caching and etiquette

- In-process scheduler (`time.Ticker` + jobs table): GitHub search refresh every 6 h; snapshots daily 03:00; arXiv every 6 h (≤ 1 request/3 s); HF every 3 h; Semantic Scholar/OpenAlex enrichment lazily for the top N (N = 200) per list.
- HTTP: conditional requests (ETag / If-Modified-Since), `User-Agent: yubeos/<ver> (+repo URL)`, global per-host token bucket, honour `Retry-After` and GitHub `X-RateLimit-*`, exponential back-off with jitter, circuit breaker per source.
- Response caps: 5 MB per response, 10 s timeout; all strings length-capped before storage; HTML stripped (abstracts rendered as escaped text).
- Offline: serves cache, shows `updated 3 h ago` badge; no errors surfaced beyond the Sources page.
- Privacy: no user data is sent to any source; the only identifier is the optional GitHub token you provide.

## 6. Limits and honesty

- "Growing" is only as good as the snapshot history; the UI shows history depth.
- Star counts can be gamed; heuristics reduce, not eliminate, this.
- "Reliable" is an allow-list judgement, exposed and editable — not a truth claim. Preprints are always labelled as such.
- No LLM summarisation in 1.0 (non-goal); an optional, user-configured summariser hook is considered post-1.0 and would live behind an add-on boundary.

## 7. Acceptance criteria (Phase 8)

1. Fixture-driven ranking tests (golden outputs) for each window and for the cold-start path.
2. With the network disabled, the module renders from cache without errors.
3. Rate-limit simulation: Yubeos stops before exhausting the GitHub budget and resumes after reset.
4. Adding a topic changes lists on next refresh without restart; hidden/saved items persist per user.
5. Score breakdown matches recomputation from stored inputs.
