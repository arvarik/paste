# Developer guide

Paste uses Go for the server and browser JavaScript for the interface. The production image contains one server and prebuilt static assets.

## Toolchain

Install these versions or newer compatible patch releases:

- Go 1.26.6
- Node.js 24.15.0
- npm
- Docker with the Compose plugin

Install frontend packages:

```bash
npm ci
```

## Source layout

```text
cmd/server/          HTTP server entry point
cmd/pastectl/        Backup and integrity tool
internal/api/        HTTP routes, middleware, and work limits
internal/auth/       API token storage and verification
internal/config/     Environment parsing and validation
internal/models/     Shared data types
internal/storage/    Atomic files, revisions, quotas, and backups
static/css/          Frontend CSS source
static/js/           Browser modules, workers, and tests
templates/           Go HTML templates
docs/                User and developer documentation
```

## Frontend build

The asset build bundles JavaScript and vendor CSS. It also compiles Tailwind CSS and generates third-party notices.

```bash
npm run build
```

Generated files go to `static/dist`. Git ignores this directory.

Run the frontend test suite and build together:

```bash
npm run check
```

## Server development

Build the assets, then start the server:

```bash
npm run build
go run ./cmd/server
```

Use a temporary data directory for manual tests.

```bash
DATA_DIR=/tmp/paste-development go run ./cmd/server
```

## Required checks

Run these checks before a pull request:

```bash
npm run check
go mod tidy -diff
go test -count=1 ./...
go test -count=1 -race -shuffle=on ./...
go vet ./...
go build ./...
```

The CI workflow also runs Staticcheck, `govulncheck`, `npm audit`, and a production image build.

## Browser review

Test both themes at these viewport sizes:

- 1440 by 1000 for desktop
- 768 by 1024 for tablet
- 390 by 844 for common mobile screens
- 320 by 568 for narrow mobile screens

Review new and saved paste states. Review both diff layouts, all dialogs, the sidebar, options, and revision controls.

Check keyboard focus, horizontal overflow, browser console errors, and automated accessibility results.

## Storage changes

Use atomic file helpers for every durable change. Commit content before metadata selects it.

Keep post-commit errors separate from pre-commit failures. Never delete committed content during error recovery.

Add failure injection tests for every multi-file transaction. Run storage tests with the race detector.

## API changes

Use the shared JSON decoder. It limits body size, rejects unknown fields, and requires one JSON value.

Map storage errors with the shared response helper. Keep secret hashes and internal file names out of public response types.

Add contract tests for status codes, response shapes, authorization, and stale revisions.

## Release checks

1. Update user documentation and screenshots.
2. Run every required check.
3. Build and inspect the production image.
4. Verify desktop and mobile browser flows.
5. Create the release commit and tag.
6. Push the branch and tag.
7. Open the release pull request.
8. Publish the GitHub release notes.
