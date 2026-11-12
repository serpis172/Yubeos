# ADR-0004: React + TypeScript SPA, no component kit

**Status:** Accepted · **Date:** 2026-11-11

## Context
Need a rich, real-time, themeable UI (charts, tables, terminal) with strong ecosystem support for xterm.js and data tables.

## Decision
React 19 + TypeScript (strict) + Vite; TanStack Query/Table, Zustand, uPlot, xterm.js, Radix primitives, i18next; CSS Modules over design tokens. Built assets embedded into `yubeosd`. OpenAPI-generated client.

## Consequences
Large ecosystem and contributor pool; discipline needed to keep bundles small. No UI kit means more component work but total control of theming and TrueNAS-like look.

## Alternatives considered
Svelte/Solid: smaller bundles, smaller ecosystem for terminal/table/a11y primitives. Angular (TrueNAS): heavy. HTMX/server-rendered: poor fit for live dashboards and terminals.
