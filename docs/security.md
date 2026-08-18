# Security guide

Paste separates public read access from change access. Deployments can also require API tokens for new items.

## Public IDs and edit secrets

New items use 128-bit random public IDs. Knowledge of an ID permits a read unless another policy blocks it.

Each create response also returns a separate 256-bit edit secret. The server stores only a hash of this secret.

The browser stores the secret in local storage. It sends the secret in the `X-Edit-Secret` header.

Do not place edit secrets in URLs, logs, screenshots, or shared messages.

## API tokens

Set a bootstrap administrator token with 32 to 256 random characters. Prefer `PASTE_ADMIN_TOKEN_FILE` in production.

Use the bootstrap token to create shorter-lived scoped tokens. Revoke unused tokens.

Available scopes are:

- `read` identifies read-only automation and uses the authenticated request limit.
- `write` permits creates and trusted changes.
- `admin` permits all operations and token administration.

The server stores token hashes in `data/auth/tokens.json`. A created token secret appears once.

## Transport security

Paste serves HTTP. Use an HTTPS reverse proxy for every remote deployment.

The default Compose port binds to `127.0.0.1`. Keep this bind when the proxy runs on the same host.

## Trusted proxies

Paste ignores forwarding headers by default. Add only the proxy IP addresses or CIDRs that you control.

A trusted proxy must remove these client headers before it adds its own values:

- `Forwarded`
- `X-Forwarded-For`
- `X-Real-IP`

A broad trusted range can let a client select its apparent address.

## Markdown and browser controls

Paste sanitizes Markdown with a fixed tag and attribute allowlist. It removes scripts, event attributes, forms, and layout classes.

The server sends a content security policy and other browser security headers. Frontend packages use pinned versions and local bundles.

## Burn-after-read and expiry

A burn item disappears after its first content read. Public lists and searches do not expose burn item IDs.

Do not treat burn-after-read as guaranteed delivery. A reader with the public ID can consume the item first.

Expired items reject reads and changes. A background purge removes their stored files and releases quota.

## Rate and resource limits

Paste limits anonymous requests by IP and authenticated requests by token. Create limits use a rolling one-hour window.

Diff, format, and preview requests use bounded worker pools. Storage, item, cache, and backup limits restrict resource use.

## Deployment checklist

1. Put Paste behind HTTPS.
2. Keep the server port private.
3. Use an administrator token file.
4. Set trusted proxies narrowly.
5. Set storage and request limits.
6. Require write tokens when the service is public.
7. Back up items and the token store separately.
8. Test restore and token revocation procedures.
