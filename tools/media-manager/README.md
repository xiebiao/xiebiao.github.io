# R2 Media Manager

Local-only image catalog and uploader for Hugo posts. The service always binds to `127.0.0.1`, stores its catalog in SQLite, uploads one immutable original to R2, asks Cloudflare Images to generate responsive WebP files once, stores those files permanently in R2, and exports their direct URLs to `../../data/media/assets.json` for Hugo.

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

Open <http://127.0.0.1:7331>. Image resizing and WebP encoding happen through Cloudflare Image Transformations during upload. The resulting files are downloaded and written back to R2, so the local service has no CGO, `libvips`, or third-party Go dependencies.

The manager scans `../../content/posts` and `../../content/photos` through `GET /api/content`. Translations with the same source basename are merged into one stable reference, such as `posts/my-post` or `photos/my-gallery`, and can be selected when uploading or editing an asset. Override the Hugo content directory with `MEDIA_CONTENT` when starting the service from another layout.

## Object and metadata model

- Original: `posts/{asset-id}/original.{ext}`
- Responsive files: `posts/{asset-id}/{width}.webp`
- Default widths: 480, 960, 1440, and 1920 pixels, never generating beyond the source width
- Each width uses one Cloudflare Image Transformation when the asset is uploaded; normal site traffic reads the stored WebP directly from R2
- R2 originals and WebP variants use `If-None-Match: *` plus one-year immutable cache headers
- SQLite is the complete metadata source; R2 custom metadata stores the asset ID, checksum, filename, alt, and caption
- The JSON export is keyed by asset ID and is safe to commit

If transformation or R2 storage fails, the upload is rejected and every object written for that attempt is rolled back. No dynamic transformation URL is exported to Hugo.

Assets created by an older version may still contain dynamic `format=auto` URLs. Use **生成并存储 WebP** on the asset card to generate the fixed R2 objects and replace those legacy manifest entries.

Use the copied shortcode in a post:

```go-html-template
{{</* figure asset="asset-id" */>}}
```

An asset with article references cannot be deleted until those references are removed from its metadata.
