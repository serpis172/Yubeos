## What and why
<!-- One paragraph. Link the issue and roadmap phase (phase:N). -->

## Contract changes
- [ ] `api/openapi.yaml` / `schemas/` / `docs/DATA-MODEL.md` updated **first** (or: no contract change)
- [ ] `make gen` run; generated client committed

## Security impact
- [ ] No new privileged method / or: schema + allow-list entry + audit event + hostile-input test added
- [ ] Inputs validated at the boundary; no shell strings; no secrets in logs

## Evidence
- [ ] `make lint test` green
- [ ] Works in `--demo` mode; screenshots (dark + light) for UI changes
- [ ] Docs updated; debt (if any) registered in `docs/engineering/TECH-DEBT.md`

Self-review done with `docs/engineering/CODE-REVIEW.md`.
