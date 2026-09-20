// Re-takes the screenshots the site and the README publish.
//
// They are taken from the demo build, not from a live cluster, because a
// screenshot of a real cluster carries the name of whoever operates it, the
// cluster's own name and absolute paths from the machine that took it, and
// a landing page is the wrong place for any of that. The demo fixtures are
// scrubbed, so anything shot from them is safe to publish by construction.
//
//   make shots
//
// Uses the system Chrome through playwright-core, so there is no browser
// download and nothing extra in the repository.

import { createRequire } from 'node:module'
import { join } from 'node:path'

// playwright-core is a devDependency of the app, which is where the browser
// tooling already lives.
import { serveSite, findChrome } from './serve-site.mjs'

const require = createRequire(new URL('../app/package.json', import.meta.url))
const { chromium } = require('playwright-core')

const ROOT = new URL('../site', import.meta.url).pathname
const OUT = join(ROOT, 'img')
const PORT = Number(process.env.PORT ?? 8099)
const CHROME = findChrome()

const shots = []
const shoot = async (page, name, go) => {
  try {
    await go(page)
    await page.waitForTimeout(900)
    await page.screenshot({ path: join(OUT, `${name}.png`) })
    // A dialog left open blocks every click in the shot after this one.
    const cancel = page.locator('button:has-text("Cancel")').first()
    if (await cancel.count()) await cancel.click({ timeout: 2000 }).catch(() => {})
    shots.push(`  ✓ ${name}.png`)
  } catch (e) {
    shots.push(`  ✗ ${name}.png  ${e.message.split('\n')[0]}`)
  }
}

const rail = (page, label) => page.click(`.rail button[title="${label}"]`)

const server = await serveSite(ROOT, PORT)
const browser = await chromium.launch({ executablePath: CHROME, args: ['--no-sandbox'] })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 1 })

// Dark, because that is how the product is meant to be seen, and set before
// the app boots so there is no flash of the wrong theme in the capture.
await page.addInitScript(() => {
  try { localStorage.setItem('clustertrail.theme', 'dark') } catch { /* private window */ }
})
await page.goto(`http://127.0.0.1:${PORT}/demo/`, { waitUntil: 'networkidle' })

// The demo banner belongs to the demo, not to the product. A screenshot of
// it on the product's own page would advertise "Get ClusterTrail" to someone
// already reading about ClusterTrail. Found by its text rather than its
// class, because the class is scoped and generic.
await page.evaluate(() => {
  const banner = [...document.querySelectorAll('div')]
    .find((d) => d.querySelector(':scope > strong')?.textContent?.trim() === 'This is a demo.')
  banner?.remove()
})
await page.waitForTimeout(1800)

await shoot(page, 'overview', (p) => rail(p, 'Overview'))
await shoot(page, 'timeline', (p) => rail(p, 'What changed'))
await shoot(page, 'helm', async (p) => {
  await rail(p, 'Helm')
  await p.waitForTimeout(900)
  const release = p.locator('.rel.card').first()
  if (await release.count()) { await release.click(); await p.waitForTimeout(1200) }
})
await shoot(page, 'changes', (p) => rail(p, 'Changes'))

await shoot(page, 'drift', async (p) => {
  await rail(p, 'Drift')
  await p.waitForTimeout(500)
  // The empty state is not the feature. Scan the sample repo so the shot
  // shows what drift actually looks like.
  await p.click('text=Try it with the sample repo')
  await p.waitForTimeout(1800)
  // Scoped to the screen. `.item` is also the resource-tree button class,
  // so an unscoped click here navigated to Pods and the drift shot was a
  // screenshot of the pods table.
  const first = p.locator('.screen .item.card').first()
  if (await first.count()) { await first.click(); await p.waitForTimeout(700) }
})

await shoot(page, 'detail', async (p) => {
  await rail(p, 'Resources')
  await p.waitForTimeout(500)
  await p.click('text=Pods')
  await p.waitForTimeout(900)
  // A failing pod is the one somebody actually opens, and this one has a
  // recorded object behind it.
  await p.click('.row.data:has-text("payments")')
  await p.waitForTimeout(1000)
})

// A container log with real output in it, which is also the one screen whose
// rendering is virtualised and so worth seeing.
await shoot(page, 'logs', async (p) => {
  await rail(p, 'Resources')
  await p.waitForTimeout(400)
  await p.click('text=Pods')
  await p.waitForTimeout(900)
  await p.click('.row.data:has-text("checkout-5f49f447d8-4nfzk")')
  await p.waitForTimeout(800)
  await p.click('text=Logs')
  await p.waitForTimeout(1500)
})

// The review dialog, which is the product's central claim. The demo carries
// one recorded proposal, for the checkout deployment, so this has to be that
// object.
await shoot(page, 'gate', async (p) => {
  await rail(p, 'Resources')
  await p.waitForTimeout(400)
  await p.click('text=Deployments')
  await p.waitForTimeout(900)
  await p.click('.row.data:has-text("checkout")')
  await p.waitForTimeout(900)
  await p.locator('.input.num').first().fill('8')
  await p.click('button:has-text("Scale")')
  await p.waitForTimeout(1400)
})

await shoot(page, 'secret-consumers', async (p) => {
  await rail(p, 'Resources')
  await p.waitForTimeout(400)
  await p.click('text=Secrets')
  await p.waitForTimeout(900)
  // The point of this panel is a Secret something depends on. The first row
  // is a bootstrap token nothing refers to, which demonstrates nothing.
  await p.click('.row.data:has-text("checkout-credentials")')
  await p.waitForTimeout(900)
  const used = p.locator('text=Used by').first()
  if (await used.count()) await used.click()
  await p.waitForTimeout(700)
})

await browser.close()
server.close()
console.log(shots.join('\n'))
