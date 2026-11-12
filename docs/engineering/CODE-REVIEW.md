# Code Review Standard

Every PR is reviewed against these dimensions. Reviewers use the checklist; authors self-review first.

## Severity

🔴 **Blocker** (security, data loss, correctness, contract break) · 🟠 **Should fix** (performance, reliability, maintainability) · 🟡 **Suggestion** · 🟢 **Praise** (say what is good).

## 1. Security (all PRs; mandatory deep pass for `internal/privd`, `internal/auth`, `internal/files`, `internal/addons`)

- [ ] AuthN/AuthZ: route metadata present and default-deny preserved; no ad-hoc role checks.
- [ ] Input: validated at the boundary (type, length, charset, range); identifiers re-resolved from fresh state.
- [ ] Injection: no shell, no string-built commands/SQL; args are `[]string`, SQL parameterised; `--` separators/option-injection guarded.
- [ ] Path handling: canonicalised; no traversal/symlink escape/TOCTOU; `openat2` semantics where applicable.
- [ ] Secrets: none in code, logs, errors, audit params, or URLs.
- [ ] Output: no HTML from untrusted data; downloads `attachment`; CSP unaffected.
- [ ] SSRF/egress: only allow-listed hosts; redirects limited; response size/time caps.
- [ ] Privileged method: has schema, class, audit event, hostile-input test; destructive ⇒ step-up.
- [ ] Concurrency: no shared mutable state without sync; no check-then-act on filesystem/DB.

## 2. Correctness

- [ ] Edge cases: empty, huge, Unicode, concurrent modification, partial failure, retries, restarts mid-job.
- [ ] Errors wrapped and mapped to the right problem type; no swallowed errors; `context` cancellation honoured.
- [ ] Idempotency of reconcilers/jobs; atomic writes (`tmp + fsync + rename`).
- [ ] Time via injected clock; monotonic deltas for rates; counter wraps handled.
- [ ] Parsers tolerate unknown fields/versions; have golden + fuzz tests.

## 3. Performance and resources

- [ ] No unbounded queries/loops/allocations; pagination; streaming for large bodies.
- [ ] No N+1 database access; indexes exist for new filters/sorts.
- [ ] Goroutines and file descriptors have owners and are released; no leaks on client disconnect.
- [ ] Frontend: virtualised lists, memoised heavy components, WS frames batched, bundle budget respected.

## 4. Maintainability

- [ ] Layering respected (modules don't import each other; shared infra only).
- [ ] Naming, single responsibility, no duplication of an existing helper (search before adding).
- [ ] Comments explain *why*; non-obvious logic documented; public behaviour documented in `docs/`.
- [ ] Tests meaningful (fail when the logic breaks), no sleep-based flakiness.
- [ ] No new dependency without ADR-0013 justification.

## 5. Contracts and UX

- [ ] OpenAPI/schemas/DATA-MODEL updated *first*; generated client up to date; backward compatible.
- [ ] i18n keys for `en` + `it`; states (loading/empty/error/capability-missing); keyboard + ARIA; light/dark + 3 presets checked.

## 6. Output format for review comments

```
### Summary            (1–2 sentences + overall quality)
### Critical issues    | # | File:Line | Issue | Severity |
### Suggestions        | # | File:Line | Suggestion | Category |
### What looks good
### Verdict            Approve / Request changes / Needs discussion
```

## 7. Review SLAs and approvals

First response ≤ 2 working days; PRs ≤ 400 changed lines preferred (split otherwise); two approvals for `internal/privd`, `internal/priv`, `cmd/yubeos-priv`, `packaging/` and `docs/SECURITY.md`; authors never merge their own PRs on those paths.
