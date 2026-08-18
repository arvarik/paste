# Configuration

Paste reads runtime settings from environment variables. The server validates all values during startup.

## Server settings

| Variable | Default | Purpose |
|---|---:|---|
| `PORT` | `8083` | Sets the HTTP port inside the container. |
| `DATA_DIR` | `./data` | Sets the private storage directory. |
| `PASTE_ADMIN_TOKEN` | empty | Sets the bootstrap administrator token. |
| `PASTE_ADMIN_TOKEN_FILE` | empty | Reads the bootstrap token from a private file. |
| `PASTE_MAX_ITEMS` | `10000` | Sets the maximum stored item count. |
| `PASTE_MAX_STORAGE` | `1GiB` | Sets the total content and revision limit. |
| `PASTE_MAX_ITEM_SIZE` | `2MiB` | Sets the size limit for one item. |
| `PASTE_CONTENT_CACHE` | `64MiB` | Sets the cached content limit. |
| `PASTE_SEARCH_INDEX` | `64MiB` | Sets the search index limit. |
| `PASTE_PREVIEW_CACHE` | `64MiB` | Sets the generated preview limit. |
| `PASTE_BACKUP_LIMIT` | `2GiB` | Sets the backup archive limit. |
| `PASTE_RATE_ANONYMOUS_PER_MINUTE` | `120` | Sets the anonymous request rate. |
| `PASTE_RATE_AUTHENTICATED_PER_MINUTE` | `600` | Sets the API token request rate. |
| `PASTE_RATE_BURST` | `30` | Sets the short request burst size. |
| `PASTE_CREATE_LIMIT_PER_HOUR` | `60` | Sets the rolling create limit per IP or token. |
| `PASTE_REQUIRE_TOKEN_FOR_CREATE` | `false` | Requires a write token for new items. |
| `PASTE_DEFAULT_EXPIRY` | `0s` | Sets the default lifetime. Zero disables expiry. |
| `PASTE_MAX_EXPIRY` | `8760h` | Sets the maximum item lifetime. |
| `PASTE_DIFF_WORKERS` | `2` | Sets concurrent server diff jobs. |
| `PASTE_FORMAT_WORKERS` | `2` | Sets concurrent format jobs. |
| `PASTE_PREVIEW_WORKERS` | `2` | Sets concurrent image jobs. |
| `PASTE_WORK_WAIT_TIMEOUT` | `2s` | Sets the maximum work queue wait. |
| `PASTE_TRUSTED_PROXIES` | empty | Lists trusted proxy IP addresses or CIDRs. |

Byte values accept `KiB`, `MiB`, `GiB`, `KB`, `MB`, and `GB`. Duration values use the Go duration format.

## Docker Compose settings

Compose reads the server variables from `.env`. It also reads `PASTE_PORT` for the host port.

The default port mapping uses `127.0.0.1`. This mapping blocks direct remote access.

## Administrator token file

Use a token file for production. The token must contain 32 to 256 characters.

Create a private file outside the repository. Mount it read-only, then set its container path.

```yaml
services:
  paste:
    environment:
      PASTE_ADMIN_TOKEN_FILE: /run/secrets/paste-admin-token
    volumes:
      - ./secrets/admin-token:/run/secrets/paste-admin-token:ro
```

Do not set `PASTE_ADMIN_TOKEN` and `PASTE_ADMIN_TOKEN_FILE` together.

## Example production values

```dotenv
PASTE_MAX_ITEMS=25000
PASTE_MAX_STORAGE=5GiB
PASTE_MAX_ITEM_SIZE=2MiB
PASTE_RATE_ANONYMOUS_PER_MINUTE=60
PASTE_CREATE_LIMIT_PER_HOUR=30
PASTE_REQUIRE_TOKEN_FOR_CREATE=true
PASTE_TRUSTED_PROXIES=10.0.0.0/8
```

Choose limits that fit the host memory, storage, and traffic.
