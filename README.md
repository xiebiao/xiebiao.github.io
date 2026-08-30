# xiebiao.com

Hugo source for [xiebiao.com](https://xiebiao.com/), plus a local-only R2 media manager in `tools/media-manager/`.

## Site

```sh
hugo server
```

Production output is generated in `public/`. GitHub Actions deploys only the Hugo site; the media manager is not built or deployed.

The default language is English at `/`; Chinese is built under `/zh/`. Pair translated articles by giving them the same base name and a language suffix:

```text
content/posts/my-post.zh.md
content/posts/my-post.en.md
```

A Chinese-only article may use either `my-post.md` or `my-post.zh.md`. Shared interface strings live in `i18n/zh.toml` and `i18n/en.toml`.

## Content sections

- `content/projects/` uses product portfolio templates. Project front matter supports `logo`, `externalURL`, `platforms`, and `status`.
- `content/posts/` uses long-form article templates and the managed-media shortcode below.
- `content/photos/` uses gallery templates. Photo front matter supports `cover`, `location`, and an `images` list with responsive `srcset` entries.

Each section has independent list and single-page templates under `layouts/projects/`, `layouts/posts/`, and `layouts/photos/`.

Reference managed media from either language with:

```go-html-template
{{</* figure asset="asset-id" */>}}
```

The shortcode reads public media metadata from `data/media/assets.json`, which is exported by the local media manager.
