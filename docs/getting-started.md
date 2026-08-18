# Getting started

Paste runs as one Go server with local static assets. It stores data in a private local directory or Docker volume.

## Docker Compose

Install Git and Docker with the Compose plugin.

```bash
git clone https://github.com/arvarik/paste.git
cd paste
cp .env.example .env
docker compose up -d
```

Open [http://localhost:8083](http://localhost:8083).

Compose creates the `paste-data` volume. The container writes application data to `/app/data`.

Use `PASTE_PORT` in `.env` to change the host port.

```dotenv
PASTE_PORT=9090
```

Then open `http://localhost:9090`.

## Run from source

Install these tools:

- Go 1.26.6 or newer
- Node.js 24.15.0 or newer
- npm

Build the frontend assets before you start the server.

```bash
npm ci
npm run build
go run ./cmd/server
```

The source build listens on port `8083`. It stores data in `./data`.

## Create the first item

Open the Paste workspace. Add a title and content, then select **Save**.

The create response contains one edit secret. The browser stores that secret in local storage.

The edit secret stays on the current browser profile. Export an item before you move to a different device.

## Open the diff workspace

Select the workspace menu, then select **Diff**. Enter text or a paste ID in each editor.

Select **Compare** to create a visual result. Save the result if you want a permanent diff URL.

## Next steps

- Read [Configuration](configuration.md) before a production deployment.
- Read [Security](security.md) before you expose Paste to a network.
- Read [Operations](operations.md) before you create a backup plan.
