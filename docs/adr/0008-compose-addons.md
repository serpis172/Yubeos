# ADR-0008: Docker Compose as the add-on runtime

**Status:** Accepted · **Date:** 2026-11-11

## Context
Users expect one-click apps; the self-hosting ecosystem publishes Compose files.

## Decision
Add-ons = Compose project + `addon.yaml` manifest (schema-validated). Operations executed by `yubeos-priv` via the `docker compose` CLI with argument arrays; all objects labelled; Yubeos manages only labelled objects. Runtime type field reserves `native`.

## Consequences
Huge catalogue compatibility; Docker is root-equivalent so access is confined to the helper. Less typed control than the Docker SDK.

## Alternatives considered
Kubernetes/k3s: overkill. Podman: viable later behind the same interface. Custom packaging format: reinvents Compose.
