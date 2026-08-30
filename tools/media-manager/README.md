# R2 Media Manager

Local-only image catalog and uploader for Hugo posts. The service always binds to `127.0.0.1`, stores its catalog in SQLite, uploads one immutable original to R2, and exports Cloudflare Images transformation URLs to `../../data/media/assets.json` for Hugo.

## Prerequisites

- Go 1.24+
- `sqlite3` command

## Configure R2

1. Create a Standard R2 bucket.
2. Attach `media.xiebiao.com` as its custom domain and enable Cloudflare Cache.
3. In **Images > Transformations**, enable transformations for the `xiebiao.com` zone and allow `media.xiebiao.com` as a source origin.
4. Create an R2 API token limited to object read/write for this bucket.
5. Copy `.env.example` to `.env` and set the values. `.env` is ignored by Git.

The service does not parse `.env` itself, keeping credentials handling explicit:

```sh
cd tools/media-manager
set -a
source .env
set +a
go run ./cmd/media-manager
```

Open <http://127.0.0.1:7331>. Image resizing and format negotiation happen at Cloudflare's edge, so the local service has no CGO or third-party Go dependencies.

## Object and metadata model

- Original: `posts/{asset-id}/original.{ext}`
- Transform URLs: `/cdn-cgi/image/width={width},quality=82,format=auto,fit=scale-down,onerror=redirect/{original}`
- Default widths: 480, 960, 1440, and 1920 pixels, never requesting beyond the source width
- R2 originals use `If-None-Match: *` plus one-year immutable cache headers
- SQLite is the complete metadata source; R2 custom metadata stores the asset ID, checksum, filename, alt, and caption
- The JSON export is keyed by asset ID and is safe to commit

`onerror=redirect` falls back to the original R2 object if a transformation fails or the monthly free transformation limit is exhausted.

Use the copied shortcode in a post:

```go-html-template
{{</* figure asset="asset-id" */>}}
```

An asset with article references cannot be deleted until those references are removed from its metadata.
