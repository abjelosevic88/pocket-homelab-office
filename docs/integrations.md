# Opening from other Pocket apps

## Pocket Files

Pocket Files shows "Edit in Pocket Office" on documents only when both apps are connected through
Pocket Core. It uses this app's `office:open` scope to ask `GET /api/v1/integrations/files` (the only
route that scope reaches) whether this person's documents there can be opened here:
`{ready, reason?, open_url}`, ready when this app's own `files:read` + `files:write` link to Pocket Files
is on.

Pocket Files then loads `/open/files?path=<its WebDAV path>&embed=1` in a frame. That page makes the
person's "Pocket Files" place through Core the first time (`POST /api/v1/sources/files/ensure`, a place
over all of their files) and opens the editor. With `embed=1` the editor's Back becomes Close and posts
`{type: "pocket-office:close"}` to the page around it; saving works as for any place.

## Pocket Move

Pocket Move can copy your My files out and bring them in. `GET /api/v1/export.zip` is every document in
your own My files (the place on this box), in its folders; `POST /api/v1/import` (multipart `file`, a zip)
adds such files to your My files: a file already there with the same bytes is skipped, a different file of
the same name comes in beside it as `name (2).docx`, nothing is replaced. `GET /api/v1/pocket/portable`
describes both for `move:export` / `move:import` links. Files in Nextcloud, Pocket Documents, Pocket Files
or WebDAV stay where they are.
