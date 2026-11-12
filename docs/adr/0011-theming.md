# ADR-0011: Data-only themes compiled to CSS custom properties

**Status:** Accepted · **Date:** 2026-11-11

## Context
Requirement: user-changeable colours, fonts and style; must remain safe and consistent.

## Decision
Themes are validated JSON (schema) → CSS variables (`--y-*`); components use tokens only (lint-enforced); fonts self-hosted + optional validated uploads; no custom CSS/JS in 1.0; presets incl. a TrueNAS-like default.

## Consequences
Live preview, easy sharing, safe by construction; limits extreme customisation (tracked TD-004).

## Alternatives considered
Free-form CSS injection: security risk. CSS-in-JS runtime theming: heavier and slower.
