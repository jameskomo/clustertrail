// A static server for site/, used by the screenshot and demo-check scripts.
//
// Read the file before writing any header: doing it the other way round means
// a miss tries to write headers twice and the process dies with
// ERR_HTTP_HEADERS_SENT instead of falling back.

import { existsSync } from 'node:fs'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join, normalize } from 'node:path'

/** The first Chrome on this machine. CI runners and desktops differ. */
export function findChrome() {
  const explicit = process.env.CHROME
  if (explicit) return explicit
  const candidates = [
    '/usr/bin/google-chrome-stable',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium-browser',
    '/usr/bin/chromium',
    '/snap/bin/chromium',
  ]
  const found = candidates.find((p) => existsSync(p))
  if (!found) throw new Error(`no Chrome found; set CHROME to one of: ${candidates.join(', ')}`)
  return found
}

const TYPES = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2',
  '.woff': 'font/woff',
  '.ico': 'image/x-icon',
}

/** Serves root on port, falling back to the demo's index for its own routes. */
export async function serveSite(root, port) {
  const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost')
    let path = join(root, normalize(url.pathname).replace(/^(\.\.[/\\])+/, ''))
    if (path.endsWith('/')) path += 'index.html'

    let body
    let type = TYPES[extname(path)] ?? 'application/octet-stream'
    try {
      body = await readFile(path)
    } catch {
      // The demo is a single-page app: unknown paths under it are its routes.
      try {
        body = await readFile(join(root, 'demo/index.html'))
        type = 'text/html'
      } catch {
        res.writeHead(404, { 'content-type': 'text/plain' })
        res.end('not found')
        return
      }
    }
    res.writeHead(200, { 'content-type': type })
    res.end(body)
  })

  await new Promise((resolve) => server.listen(port, '127.0.0.1', resolve))
  return server
}
