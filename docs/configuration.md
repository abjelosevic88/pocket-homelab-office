# Configuration

Everything is an environment variable. Defaults make `docker run` with one volume and the address of an
ONLYOFFICE Document Server work. An admin can later change the Document Server part in
Settings → Document Server (stored in the database, overriding the environment).

| Variable | Default | Meaning |
|---|---|---|
| `HOST`, `PORT` | `0.0.0.0`, `8080` | listen address |
| `BASE_URL` | – | the address browsers use (`https://office.example.com`). Needed for sign-in (the OIDC callback is `BASE_URL/api/v1/auth/oidc/callback`) and as the default for `CALLBACK_URL` |
| `DATA_DIR` | `./data` | SQLite file, built-in files, Core token, backups |
| `DB_PATH` | `DATA_DIR/pocket-office.db` | |
| `FILES_DIR` | `DATA_DIR/files` | the built-in place: one folder per person (`FILES_DIR/<user id>/`) |
| `FOLDER_ROOTS` | – | comma-separated absolute paths people may attach as a "folder on the box" (mount them into the container) |
| `MAX_UPLOAD_MB` | `200` | largest upload |
| `LOG_LEVEL`, `LOG_FORMAT` | `info`, `text` | `debug` logs every request; `json` for a log shipper |
| `TRUST_PROXY` | `true` | take the client IP from `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | `/metrics` (Prometheus) |
| `AUTH_MODE` | `local` | `local` (sessions) or `none` (everyone is the first user; development only) |
| `SESSION_TTL` | `720h` | browser session length |
| `SECURE_COOKIES` | `false` | set `true` behind HTTPS |
| `PASSWORD_LOGIN` | `true` | allow email + password next to *Sign in with Pocket Core*. `false` = Core only, no setup page |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | bootstrap the first account from the environment (only while there are no users) |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | an explicit OpenID Connect provider. Not needed with Core: the app uses the client Core issues at registration. When set, they win |
| `ONLYOFFICE_URL` | – | the Document Server as browsers reach it; the editor script `web-apps/apps/api/documents/api.js` is loaded from here |
| `ONLYOFFICE_INTERNAL_URL` | `ONLYOFFICE_URL` | how this server reaches Document Server (conversions, health), e.g. `http://onlyoffice:80` |
| `ONLYOFFICE_JWT_SECRET` | – | the `JWT_SECRET` Document Server was started with. Strongly recommended; without it anyone who can reach Document Server can make it fetch files |
| `ONLYOFFICE_JWT_HEADER` | `AuthorizationJwt` | Document Server's `JWT_HEADER` |
| `CALLBACK_URL` | `CORE_APP_URL`, else `BASE_URL` | how Document Server reaches **this app**: it downloads files from `CALLBACK_URL/api/v1/oo/file/…` and posts saves to `CALLBACK_URL/api/v1/oo/callback/…`. Must be reachable from the Document Server container (`http://pocket-office:8080`, `http://host.docker.internal:8186`, or the public address). Document Server refuses private addresses unless started with `ALLOW_PRIVATE_IP_ADDRESS=true` |
| `COLLABORA_URL` | – | optional second engine: Collabora Online as browsers reach it (`https://collabora.example.com`). Collabora must trust this app as a WOPI host: add `CALLBACK_URL`'s origin to its `aliasgroupN` (e.g. `aliasgroup2: http://pocket-office:8080`) |
| `COLLABORA_INTERNAL_URL` | `COLLABORA_URL` | how this server fetches `hosting/discovery` |
| `DEFAULT_ENGINE` | `onlyoffice` | the box's default editor (`onlyoffice` or `collabora`); an admin can change it in Settings → Document Server, each person under Editor & look |
| `WOPI_URL` | `BASE_URL` | how Collabora reaches this app for WOPI. Collabora lets only this address's origin embed it, so it is normally the public address (the Collabora container must resolve and reach it) |
| `CORE_URL` | – | Pocket Core as the app reaches it (`http://pocket-core:8080`, `http://host.docker.internal:8179`) |
| `CORE_REGISTRATION_KEY` | – | from Core → Settings. Used once; the app token Core mints is kept in `DATA_DIR/core.token` |
| `CORE_APP_URL` | `BASE_URL` | how Core reaches the app for health checks |
| `CORE_PUBLIC_URL` | Core's issuer | what links to Core open |

## What Pocket Core adds

With `CORE_URL` set the app registers itself (slug `office`), and for each person Core can hand it:

- their **Nextcloud** account (catalog type `nextcloud`, fields `USERNAME` + `APP_PASSWORD`), so a Nextcloud place needs nothing typed — the admin and the person can switch that off in Core at any time;
- a link into **Pocket Documents** (`documents:read` + `documents:write`), so the office files in the archive open and save back without an API token;
- a link into **Pocket Files** (`files:read` + `files:write`): a Pocket Files place is the person's home there (or a folder in it) over WebDAV at `<files url>/dav/`, with a `pcx_` token minted for the place's owner as the password. A token is used for four minutes at most (never in its last minute) and then Core is asked again, so switching the link off in Core stops the place within minutes. Pocket Files serves every home at one address, so the owner's email is part of a file's canonical name. Without `CORE_URL` the option is not offered.

It also gives *Sign in with Pocket Core*, accepts Core device tokens (`pcd_`/`pcu_`/`pco_`) as bearer tokens, publishes `office.opened`, `office.saved` and `office.created` on the bus, and offers other apps `files:read` (list places, browse, search). Without `CORE_URL` every place is typed in by hand and everything else works the same.

## Data on disk

- `DATA_DIR/pocket-office.db`: users, sessions, API tokens, places (with typed-in secrets), the keys of documents open in Document Server, recent files. Back it up with `pocket-office backup` (a consistent SQLite copy) or SQLite's backup API — never copy the live `.db` with WAL files beside it.
- `DATA_DIR/files/<user id>/`: the built-in place. Plain files; back them up as they are.
- `DATA_DIR/tmp/`: probes and scratch; safe to exclude.
- `DATA_DIR/core.token` (+ `.oidc.json`): the app's Core credentials.

Keep `DATA_DIR` at mode 0700. Files in Nextcloud, WebDAV, Pocket Documents, Pocket Files or a server folder are never copied here: they are read when opened and written when Document Server reports a save.

## How a document is opened and saved

1. The browser asks `POST /api/v1/sources/{id}/open` for a file. The app looks the file up in its place, records a **document key** (the same key for everyone who opens that file until it is closed — that is how co-editing works) and answers with a signed editor configuration.
2. The browser loads Document Server's `api.js` and starts the editor with that configuration. Document Server downloads the file from `CALLBACK_URL/api/v1/oo/file/<key>?t=<one-off token>`.
3. While people edit, Document Server keeps the document; on every autosave interval and when the last editor closes it posts to `CALLBACK_URL/api/v1/oo/callback/<key>` (JWT in the `AuthorizationJwt` header). The app downloads the result and writes it back where the file came from, with an `If-Match` on the ETag it read: if the file changed outside meanwhile, the save lands beside it as `name (conflict <date>).docx` instead of overwriting.
4. A closed document drops its key, so the next open reads the file fresh.

The format table (`internal/office/templates/onlyoffice-docs-formats.json`) is the manifest Document Server ships: a type with `edit` or `lossy-edit` opens in the editor and saves back as itself; a type with only `view` + `auto-convert` (`.doc`, `.xls`, `.ppt`, `.pages`…) opens in the viewer and **Edit a copy** converts it into the family's editable type next to the original; Visio types open in the diagram viewer. **Download as…** offers exactly that type's `convert` list (PDF first), and `/export?to=` refuses anything else. Replace the JSON with the one from a newer Document Server (`/var/www/onlyoffice/documentserver/document-formats/`) to pick up new types.

On phones the free (Community) Document Server offers its mobile editors for viewing only, so files open in the viewer there; **Edit** in the top bar opens the full desktop editor on the phone instead.

## Collabora (WOPI)

With `COLLABORA_URL` set (or the address saved in Settings → Document Server), Pocket Office is a WOPI host: `GET /wopi/files/{key}` (CheckFileInfo), `GET /wopi/files/{key}/contents` (GetFile), `POST /wopi/files/{key}/contents` (PutFile) and `POST /wopi/files/{key}` (PutRelative for *Save as*; lock calls are answered without locks). The key is the same document key the ONLYOFFICE flow uses and the `access_token` is its fetch token; `WOPISrc` is `WOPI_URL/wopi/files/{key}`. PutFile honours `X-COOL-WOPI-Timestamp`: when the file changed in storage since Collabora last saw it, the answer is 409 with `COOLStatusCode 1010` and Collabora asks the person whether to overwrite. A person's default engine is the user setting `engine`; `POST /sources/{id}/open` takes `engine` to override it for one open, and the answer carries `engine` plus either `api_js`+`config` (ONLYOFFICE) or `collabora: {action_url, access_token, access_token_ttl, wopi_src, edit, origin}` (the browser posts the token into an iframe).

## Pocket Review

With Pocket Core, Office declares a link to Pocket Review (`reviews:read`, `reviews:write`). Folder listings and the editor ask Pocket Review's lookup endpoint for the files' source references and show an *In review* badge that opens the review; *Send for review…* renders the file to PDF (Document Server, else Collabora's convert-to) and posts it to Pocket Review with the reference, a link back and a label. A file already in an unfinished review becomes a new revision (or a separate review on request). References: `documents:<id>` for files from Pocket Documents (the same Documents uses), `office:draft:<user>:<path>` for drafts (moved to the final reference when the draft is saved somewhere), else a hash of the file's canonical address. Endpoints: `GET /sources/{id}/collab?path=` and `POST /sources/{id}/collab/send {path, title, description, note, due_date, mode: new|revision, start}`.

## Drafts

Every person has a **Drafts** place (`DATA_DIR/drafts/<user id>/`). `POST /api/v1/drafts` (multipart `file`) puts dropped or chosen files there and the UI opens the first one at once; Document Server autosaves into the draft like into any other place. `POST /api/v1/drafts/save {path, source_id, dir, name, keep, overwrite}` first asks Document Server to force-save the document if it is open (the command service, waiting up to 10 s for the callback), then copies the file into the chosen place and removes the draft unless `keep` is set.

## API

Base path `/api/v1`. The session cookie, `Authorization: Bearer po_…` (Settings → API tokens), a Pocket Core token (`pcd_`, `pcu_`, `pco_`) or a Core app-link token (`pcx_`, `files:read`). Errors are `{"error": "…"}`.

| Method | Path | Notes |
|---|---|---|
| GET | `/auth/status` | `needs_setup`, `authenticated`, `user`, `oidc`, `oidc_name`, `password_login`, `core`, `version`, `title` |
| GET | `/auth/oidc/start?next=` · `/auth/oidc/callback` | Sign in with Pocket Core (or the `OIDC_*` provider) |
| POST | `/auth/login` · `/auth/setup` · `/auth/logout` | password paths |
| GET/PUT | `/auth/me` | the caller; PUT `{name, current_password, new_password}` |
| GET/POST/DELETE | `/auth/tokens` · `/auth/tokens/{id}` | API tokens (plaintext shown once) |
| GET | `/sources` | the caller's places: `{id, kind, name, where, writable, tree, …}`; `builtin` and `drafts` always exist |
| POST | `/drafts` · `/drafts/save` | working copies, see above |
| POST/PUT/DELETE | `/sources` · `/sources/{id}` | `{kind: builtin\|folder\|webdav\|nextcloud\|documents\|files, name, url, username, secret, folder, via_core}`; a new place is tested before it is kept |
| GET | `/sources/options` | what can be added here (`folder_roots`, what Core knows: `core_nextcloud`, `core_documents`, `core_files`) |
| POST | `/sources/{id}/test` | reads the root: `{where, files, folders, ms}` |
| GET | `/sources/{id}/ls?path=&all=1` | one folder: `{entries:[{path, name, is_dir, size, modified, etag, kind, opens, edit, ext}], writable, tree}`; without `all` only files Document Server opens are listed |
| GET | `/sources/{id}/file?path=&inline=1` | download as is |
| GET | `/sources/{id}/export?path=&to=pdf` | download converted; `to` must be in the file's `exports` (see `/formats`), e.g. `pdf`, `pdfa`, `docx`, `odt`, `html`, `png` |
| POST | `/sources/{id}/upload?path=&overwrite=1` | multipart `file` fields; `{saved:[entry], failed:[{file, error}]}` |
| POST | `/sources/{id}/new` | `{path, name, type: docx\|xlsx\|pptx}` → entry (a taken name gets ` (2)`) |
| POST | `/sources/{id}/mkdir` · `/rename` · `/copy` · `/delete` | `{path, name}` · `{from, name\|to}` · `{from, to?}` · `{path}` |
| POST | `/sources/{id}/open` | `{path, mobile, desktop, dark, lang}` → `{api_js, config (signed), key, mode, kind, ext, edit, lossy, convertible, editable_as, exports, file, phone_view}` |
| POST | `/sources/{id}/edit-copy` | `{path, to?}` → the editable copy's entry |
| GET | `/recent` · DELETE `/recent?source=&path=` | lately opened files |
| GET | `/search?q=` | a name across every place (folder trees walked, at most a few thousand entries each) |
| GET | `/dashboard` | `{sources, open_now, opened_week, last, recent, document_server}` for a dashboard widget |
| GET/PUT | `/settings` | title and Document Server (admin), the caller's own `me` settings |
| POST | `/settings/documentserver/test` | admin: health + a conversion round trip through `CALLBACK_URL` |
| GET/POST/PUT/DELETE | `/users` · `/users/{id}` | admin |
| POST | `/settings/collabora/test` | admin: discovery reachable, how many types |
| GET | `/formats` · `/system` | every type with `kind`, `edit`, `lossy`, `convertible`, `mime`, `exports` · versions, engines and registration state |
| GET | `/oo/file/{key}?t=` · POST `/oo/callback/{key}` | Document Server's side; not for people |
| GET | `/healthz` · `/readyz` · `/metrics` | outside `/api/v1` |

## CLI

```
pocket-office                        start the server (default)
pocket-office backup [-o FILE]       write a consistent SQLite backup
pocket-office version
```
