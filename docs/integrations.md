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
