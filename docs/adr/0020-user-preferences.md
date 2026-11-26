# ADR-0020: Interface preferences are stored per user on the server

**Status:** Accepted · **Date:** 2026-11-21

## Context
Theme, custom themes, language, network-rate unit and temperature unit were first kept in the browser (`localStorage`). A user who signs in from another browser or device started from scratch.

## Decision
1. Preferences are stored **per user** in `yubeos.db` (`user_prefs`, validated JSON ≤ 128 KiB) and served by `GET`/`PUT /api/v1/me/preferences`.
2. The server **validates** what it stores: enums, theme-id pattern, at most 12 custom themes, each checked with the same rules as `schemas/theme.schema.json` (hex colours, font lists, ranges, no unknown keys). The UI validates again on read.
3. The browser keeps `localStorage` as a **cache** so `public/theme-init.js` can paint the right theme before the app loads.
4. Sync rules (`PrefsProvider`): if the server has nothing, the browser's current settings are uploaded once (migration); otherwise the server wins and is applied locally without echoing back; later changes are uploaded after a 600 ms debounce; last write wins.
5. Demo mode uses an in-memory store.

## Consequences
+ Settings follow the user across browsers and devices.
− Last-write-wins can overwrite a change made in another tab seconds earlier; acceptable for appearance settings.
− A corrupt or outdated row is treated as "nothing saved" instead of failing sign-in.
