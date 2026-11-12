# Module: File Explorer

Route `/files` · Roles: user, admin · Backend: `internal/files` + `fsworker` · API: [`/files/*`](../API.md#files)

From the sketch: breadcrumb bar `/ … / … /`, a table with **Name · Date · Type**, expandable rows (`>`), and **Upload** / **Download** buttons at the bottom.

## 1. UX specification

- **Breadcrumb** (clickable segments, editable on click for typing a path, copy-path button).
- **Table** (TanStack Table, virtualised for ≥ 10 k entries): Name (icon + name + chevron for folders), Date (modified; absolute on hover), Type (folder, MIME-derived label, extension), Size (extra column vs. sketch; useful and cheap), Owner/Permissions (hidden by default, toggle).
- **Expandable rows:** clicking `>` lazy-loads children inline (tree-in-table); state kept in URL query for deep links.
- **Selection:** click, shift-click, ctrl-click, keyboard (↑↓, space, Enter, Backspace for parent).
- **Upload:** button + drag-and-drop anywhere on the table; queue panel with per-file progress, cancel, retry; folders supported (via `webkitdirectory`).
- **Download:** single file → direct stream; folder or multi-selection → **streamed ZIP** (no temp file).
- **Context menu / toolbar (v1):** New folder, Rename, Delete (confirmation, typed for > 100 items), Copy path. **v1.1:** Copy/Move/paste, properties, permissions editor. **v2:** text preview/editor, image preview.
- Empty/error states: permission denied (explains running-as-user semantics), path missing, root not configured.

## 2. Path model and policy

- Configured **roots** (`files.roots`, default `/mnt`) are the visible top level; the UI shows a root picker. Additional per-user roots: the user's `$HOME` if enabled.
- All paths are API-relative to a root: `{root: "/mnt", path: "tank/media/films"}`. The server canonicalises, rejects `..`, NUL, and anything escaping the root; symlinks are *not followed* by default (shown with a link badge).
- The **kernel** enforces real permissions because operations run as the user (see §3). Policy in Go is defence in depth, not the primary control.

## 3. Execution model: operations as the requesting user

Go cannot safely `setuid` per request (credentials are process-wide). Design:

```
yubeosd ──fsworker.spawn(uid,gid,groups)──▶ yubeos-priv ──fork/exec with Credential──▶ yubeos-fsworker (as user)
yubeosd ⇄ fsworker over a socketpair (FD passed by SCM_RIGHTS): list / stat / open(read|write) / mkdir / rename / remove
```

- One worker **per active user session**, started lazily, idle-killed after 5 min, max 1 per session.
- The worker is a tiny static binary (`cmd/yubeos-fsworker`) with a minimal command set and `RLIMIT`s; it uses `openat2(RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS)` relative to a root dirfd.
- Bulk data (upload/download) does **not** pass through the RPC framing: the worker hands back an open file descriptor and `yubeosd` streams it (`io.Copy` / `sendfile`). Uploads write to a temporary hidden file in the destination directory, `fsync`, then `rename` (atomic; no partial files visible).
- Revisit trigger: p95 listing latency > 300 ms ([ARCHITECTURE §15](../ARCHITECTURE.md#15-trade-offs-and-what-we-will-revisit)).

## 4. Transfer details

| Aspect | Decision |
|---|---|
| Download | `GET /files/download` honours `Range` and `If-Range`, `ETag` = size+mtime, `Content-Disposition: attachment; filename*=UTF-8''…`, always `attachment`, `nosniff` |
| Archive | `POST /files/archive` → streamed ZIP (`archive/zip`, store method for already-compressed types, Zip64); no symlink traversal |
| Upload | `PUT /files/upload?root=&path=&name=` raw body streaming (no multipart buffering); `Upload-Offset` based **resumable** chunks in v1.1; max size unlimited, free-space pre-check via `Content-Length` |
| Conflicts | `409` with `existing` metadata; UI offers replace / keep both / skip |
| Names | NFC-normalised on display only; byte-exact on disk; reject `/` and NUL; warn on trailing dots/spaces and case collisions (SMB clients) |
| Large dirs | Server-side pagination (`limit`, `cursor`), default 500; sort server-side by name/date/size/type |
| Type detection | extension map + `http.DetectContentType` on first 512 B only when needed (lazy) |

## 4.1 Edge cases to test explicitly

Unicode names, 255-byte limit, names beginning with `-`, spaces, sockets/FIFOs/devices (shown, not openable), sparse files, files changing during download, cross-filesystem rename (falls back to copy+delete job), ENOSPC mid-upload (partial file removed), quota exceeded on ZFS datasets, permission denied midway through a directory archive (entry skipped and listed in an `ERRORS.txt` entry).

## 5. Acceptance criteria (Phase 4)

1. A non-admin user cannot read or write anything their Linux account cannot (verified by integration tests with two accounts).
2. Upload of a 20 GB file works with ≤ 50 MB server RSS growth; killing the browser leaves no visible partial file.
3. Directory with 100 000 entries lists first page < 500 ms and scrolls smoothly.
4. ZIP of 5 000 files streams with constant memory.
5. Path-traversal, symlink-escape and TOCTOU test suite passes.
