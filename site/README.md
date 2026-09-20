# The site

The public page and the interactive demo. One HTML file, one stylesheet, the
screenshots, and the demo build. No framework and no build step for the page
itself, because a page this size does not need one.

## Look at it locally

```sh
python3 -m http.server 8088 --directory . --bind 127.0.0.1
# http://127.0.0.1:8088
```

## What is here

| | |
|---|---|
| `index.html`, `style.css` | The page. |
| `img/` | Screenshots, taken from the demo by `make shots`. |
| `demo/` | The interface built in demo mode by `make demo`, replaying recorded fixtures. |

## Publishing it

It is static, so any static host will serve it: Cloudflare Pages, GitHub
Pages, Netlify, S3, or a file server on a box you already have. Point it at
this directory. There is no build command and the output directory is `.`.

Two things are worth carrying over from how it is served today:

- **The demo is a single-page app.** Unknown paths under `/demo/` are its own
  routes, so they need to fall back to `demo/index.html` rather than 404.
- **Hash the assets.** `make stamp` puts a content hash in each asset's query
  string. Without it, a replaced screenshot keeps being served from whatever
  cache sits in front of the page, which for images is usually a long time.

## Refreshing what is on it

```sh
make demo    # rebuild the demo from the app
make shots   # re-take every screenshot, from the demo
make stamp   # re-hash the assets the page references
```

The screenshots are taken from the demo rather than from a live cluster, on
purpose. A screenshot of a real cluster carries the operator's name, the
cluster's name and a path from the machine it was taken on. The demo fixtures
are scrubbed, so anything shot from them is safe to publish by construction.
