# API reference

The API uses JSON unless an endpoint returns plain text or a PNG image. JSON request bodies require `Content-Type: application/json`.

The server rejects unknown fields, extra JSON values, and bodies larger than 2 MiB.

## Authorization

Public read routes require no token. A server can require a write token for create routes.

Send an API token in this header:

```http
Authorization: Bearer TOKEN_SECRET
```

API token scopes are `read`, `write`, and `admin`. The `admin` scope includes all scopes.

Send an item edit secret in this header:

```http
X-Edit-Secret: ITEM_EDIT_SECRET
```

Never put a token or edit secret in a URL.

## Pagination and filters

List and search routes return this envelope:

```json
{
  "items": [],
  "nextCursor": "opaque-value"
}
```

Use `limit` from 1 to 250. Send `nextCursor` as the next `cursor` value.

List routes accept `tag` and `favorite`. Search routes also accept `q`.

## Paste routes

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/pastes` | Creates a paste. |
| `GET` | `/api/pastes` | Lists pastes. |
| `GET` | `/api/pastes/{id}` | Reads a paste. |
| `PUT` | `/api/pastes/{id}` | Updates a paste. |
| `DELETE` | `/api/pastes/{id}` | Deletes a paste. |
| `GET` | `/api/search` | Searches pastes. |
| `GET` | `/raw/{id}` | Returns plain text. |
| `GET` | `/api/pastes/{id}/preview.png` | Returns a generated preview. |
| `GET` | `/api/pastes/{id}/revisions` | Lists revisions. |
| `GET` | `/api/pastes/{id}/revisions/{revision}` | Reads one revision. |
| `POST` | `/api/pastes/{id}/revisions/{revision}/restore` | Restores one revision. |
| `GET` | `/api/pastes/{id}/export` | Exports portable JSON. |

Create a paste:

```bash
curl http://localhost:8083/api/pastes \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Example",
    "content": "Hello",
    "language": "text",
    "tags": ["docs"],
    "favorite": false,
    "burnAfterRead": false
  }'
```

The response includes `id`, `editSecret`, and `revision`. Store the edit secret securely.

Update a paste:

```bash
curl -X PUT http://localhost:8083/api/pastes/ITEM_ID \
  -H 'Content-Type: application/json' \
  -H 'X-Edit-Secret: ITEM_EDIT_SECRET' \
  -d '{
    "title": "Example",
    "content": "Updated text",
    "language": "text",
    "tags": ["docs"],
    "favorite": true,
    "burnAfterRead": false,
    "revision": 1
  }'
```

The server returns `409 Conflict` when the current revision changed.

## Diff routes

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/diff` | Calculates a server diff. |
| `POST` | `/api/saved_diffs` | Creates a saved diff. |
| `GET` | `/api/saved_diffs` | Lists saved diffs. |
| `GET` | `/api/saved_diffs/{id}` | Reads a saved diff. |
| `PUT` | `/api/saved_diffs/{id}` | Updates a saved diff. |
| `DELETE` | `/api/saved_diffs/{id}` | Deletes a saved diff. |
| `GET` | `/api/search_diffs` | Searches saved diffs. |
| `GET` | `/api/saved_diffs/{id}/revisions` | Lists revisions. |
| `GET` | `/api/saved_diffs/{id}/revisions/{revision}` | Reads one revision. |
| `POST` | `/api/saved_diffs/{id}/revisions/{revision}/restore` | Restores one revision. |
| `GET` | `/api/saved_diffs/{id}/export` | Exports portable JSON. |

The server diff route accepts `base` and `compare`. Their combined size cannot exceed 1 MiB or 20,000 lines.

## Revision restore

A restore can include the current revision for conflict protection.

```json
{
  "expectedRevision": 3
}
```

Use the item edit secret or a write API token.

## Transfer and format routes

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/import` | Imports an exported item document. |
| `POST` | `/api/format` | Formats Go, JSON, or configured Python source. |
| `GET` | `/healthz` | Reports server health. |

Go and JSON formatting use the Go standard library. Python formatting requires `black` or `autopep8` on the server.

## API token routes

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/tokens` | Creates a scoped token. |
| `GET` | `/api/tokens` | Lists token metadata. |
| `DELETE` | `/api/tokens/{id}` | Revokes a token. |

These routes require an administrator token.

```bash
curl http://localhost:8083/api/tokens \
  -H "Authorization: Bearer $PASTE_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"deployment","scopes":["read","write"]}'
```

The create response shows the token secret once. The server stores only its hash.

## Common status codes

| Status | Meaning |
|---:|---|
| `400` | The request syntax or identifier is invalid. |
| `401` | Authentication or the edit secret is missing or invalid. |
| `403` | The token lacks a required scope. |
| `404` | The item does not exist. |
| `409` | The expected revision is stale. |
| `413` | The request or diff input is too large. |
| `422` | The content or item options are invalid. |
| `429` | A request or create limit rejected the request. |
| `503` | A bounded work queue or formatter is unavailable. |
