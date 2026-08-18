# Operations and backups

Paste stores durable data in the configured data directory. The server does not require a database.

## Health check

Request `/healthz` for a readiness result.

```bash
curl --fail http://localhost:8083/healthz
```

The server reports `ok` after successful startup. Startup fails when the storage directory is not writable.

## Storage layout

```text
data/
├── auth/tokens.json
├── items/
│   ├── pastes/{id}/metadata.json
│   └── diffs/{id}/metadata.json
└── revisions/
    ├── pastes/{id}/
    └── diffs/{id}/
```

Do not edit these files while the server runs.

## Command-line tool

The image includes `/app/pastectl`. Source checkouts can use `go run ./cmd/pastectl`.

Stop the server before each command. Separate programs cannot share the in-process storage lock.

Create a verified backup:

```bash
pastectl backup --data-dir ./data --output ./paste-backup.tar
```

Verify the current store:

```bash
pastectl verify --data-dir ./data
```

Merge a backup into the current store:

```bash
pastectl import --data-dir ./data --input ./paste-backup.tar
```

Add `--overwrite` to replace matching item IDs.

Replace the complete item and revision trees:

```bash
pastectl restore --data-dir ./data --input ./paste-backup.tar --force
```

Restore requires `--force`. It uses a durable transaction record and startup recovery.

Backups include items and revisions. They do not include `auth/tokens.json`.

Create a separate protected backup of the token store when you need token recovery.

## Docker volume backups

Stop the Compose service before you run `pastectl` against its volume. You can also back up the named volume with your platform tools.

Test every backup with `pastectl verify` and a restore exercise.

## Upgrades

Create a backup before each upgrade. Read the release notes before you replace the image.

The server migrates the earlier filename layout during startup. It keeps each original creation time.

Migrated legacy items remain readable. A write token can change an item that has no edit secret.

## Logs

The server writes request facts and bounded error classes to standard output. It does not log edit secrets or bearer tokens.

Compose rotates JSON logs at 10 MiB. It retains three files.

## Reverse proxy

Keep the default local bind when a reverse proxy runs on the same host. Terminate TLS at the proxy.

Set `PASTE_TRUSTED_PROXIES` only for proxy addresses that you control. The proxy must remove client forwarding headers first.

## Capacity planning

Storage limits include current content and revisions. Cache limits control in-memory content, search, and previews.

Start with the defaults. Increase limits only after you measure host memory and disk use.
