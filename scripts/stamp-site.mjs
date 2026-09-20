// Stamps a content hash onto every asset the landing page references.
//
// The page and its images are cached hard at the edge: a week for images. So
// replacing a file on the server does not replace what visitors are served,
// and a screenshot that had to be replaced because it leaked something keeps
// being handed out until the cache expires. Purging by hand is a step
// somebody forgets.
//
// A query string is part of the cache key, so `img/timeline.png?v=<hash>` is
// a different object the moment the file changes, and unchanged files keep
// their URL and stay cached. The demo does not need this: Vite already puts
// a content hash in its filenames.
//
//   node scripts/stamp-site.mjs
//
// Idempotent: run it as often as you like.

import { createHash } from 'node:crypto'
import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

const SITE = new URL('../site', import.meta.url).pathname
const PAGE = join(SITE, 'index.html')

const hashOf = async (rel) => {
  const body = await readFile(join(SITE, rel))
  return createHash('sha256').update(body).digest('hex').slice(0, 8)
}

let page = await readFile(PAGE, 'utf8')
const stamped = []

// Every local asset the page references, with or without an existing stamp.
const refs = new Set()
for (const m of page.matchAll(/(?:src|href)="((?:img\/[^"?]+|style\.css))(?:\?v=[a-f0-9]+)?"/g)) {
  refs.add(m[1])
}

for (const rel of [...refs].sort()) {
  const v = await hashOf(rel)
  const pattern = new RegExp(`((?:src|href)=")${rel.replace(/[.\\/]/g, '\\$&')}(?:\\?v=[a-f0-9]+)?(")`, 'g')
  // Matching is the check, not changing: a file whose hash is unchanged
  // rewrites to exactly what is already there, which is the common case.
  if (!pattern.test(page)) throw new Error(`nothing matched for ${rel}; the page does not reference it the way this expects`)
  pattern.lastIndex = 0
  page = page.replace(pattern, `$1${rel}?v=${v}$2`)
  stamped.push(`  ${rel}?v=${v}`)
}

await writeFile(PAGE, page)
console.log(stamped.join('\n') || '  nothing to stamp')
