// Drives every screen of the demo and asserts it answers.
//
// The demo is the only thing most people will ever see, and its failure mode
// is quiet: a screen that refuses a read looks the same as one with nothing
// to show. Three separate features were found dead this way, one of them
// twice, because a later edit replaced the handler and only the path being
// worked on was re-checked.
//
//   make check-demo
//
// Exits non-zero on the first failure, so CI can run it.

import { createRequire } from 'node:module'
import { join } from 'node:path'

import { serveSite, findChrome } from './serve-site.mjs'

const require = createRequire(new URL('../app/package.json', import.meta.url))
const { chromium } = require('playwright-core')

const ROOT = new URL('../site', import.meta.url).pathname
const PORT = Number(process.env.PORT ?? 8104)
const CHROME = findChrome()
const results = []
let failed = 0
const check = (name, ok, detail = '') => {
  results.push(`  ${ok ? '✓' : '✗'} ${name}${detail && !ok ? `  ${detail}` : ''}`)
  if (!ok) failed++
}

// The demo's own refusal. Seeing it on a read is the bug this script exists
// to catch.
const REFUSAL = /recorded data, so nothing can be changed/i

const server = await serveSite(ROOT, PORT)
const browser = await chromium.launch({ executablePath: CHROME, args: ['--no-sandbox'] })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
const consoleErrors = []
page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()) })
page.on('pageerror', (e) => consoleErrors.push(String(e)))

await page.addInitScript(() => { try { localStorage.setItem('clustertrail.theme', 'dark') } catch { /* private window */ } })
await page.goto(`http://127.0.0.1:${PORT}/demo/`, { waitUntil: 'networkidle' })
await page.waitForTimeout(1800)

const body = () => page.locator('body').innerText()
const rail = (label) => page.click(`.rail button[title="${label}"]`)
const clear = async () => {
  const cancel = page.locator('button:has-text("Cancel")').first()
  if (await cancel.count()) await cancel.click({ timeout: 2000 }).catch(() => {})
  await page.keyboard.press('Escape').catch(() => {})
  await page.waitForTimeout(300)
}

// Every section answers rather than refusing.
for (const [label, want] of [
  ['Overview', /pods? ready|no pods/i],
  ['What changed', /rollout|field write|cluster event|just now/i],
  ['Drift', /deployed from|differs|in sync/i],
  ['Changes', /approved|rejected|applied|no changes/i],
  ['Helm', /release|shop-frontend/i],
]) {
  await rail(label)
  await page.waitForTimeout(1200)
  const text = await body()
  check(`${label} renders`, want.test(text), 'unexpected content')
  check(`${label} is not refused`, !REFUSAL.test(text))
}

// Drift actually scans.
await rail('Drift')
await page.waitForTimeout(500)
await page.click('text=Try it with the sample repo')
await page.waitForTimeout(1800)
const drift = await body()
check('drift scan returns a report', /differs from Git|in Git, not running|files read/i.test(drift))
check('drift scan is not refused', !REFUSAL.test(drift))

// A pod drawer: the object, and its logs.
await rail('Resources')
await page.waitForTimeout(400)
await page.click('text=Pods')
await page.waitForTimeout(1000)
await page.click('.row.data:has-text("checkout-5f49f447d8-4nfzk")')
await page.waitForTimeout(900)
await page.click('text=YAML')
await page.waitForTimeout(900)
const yaml = await body()
check('pod YAML is the object that was clicked', /checkout-5f49f447d8-4nfzk/.test(yaml) && !/not in the demo recording/i.test(yaml))
await page.click('text=Logs')
await page.waitForTimeout(1500)
const logs = await body()
check('pod logs have real output', /GET \/|nginx|HTTP\/1\.1/.test(logs))
check('pod logs are not the placeholder', !/logs are recorded, not live/i.test(logs))
const rendered = await page.locator('.logs .virt .line').count()
check('log view is virtualised', rendered > 0 && rendered < 200, `${rendered} lines in the DOM`)
await clear()

// A Secret: what uses it.
await page.click('text=Secrets')
await page.waitForTimeout(1000)
await page.click('.row.data:has-text("checkout-credentials")')
await page.waitForTimeout(1000)
const secret = await body()
check('Used by lists what refers to the Secret', /Deployment\/checkout/.test(secret), 'panel is empty')
check('Secret values stay masked', /Reveal/.test(secret) && !/demo-value-not-a-real/.test(secret))
await clear()

// The review dialog, from a scale.
await page.click('text=Deployments')
await page.waitForTimeout(1000)
await page.click('.row.data:has-text("checkout")')
await page.waitForTimeout(900)
await page.locator('.input.num').first().fill('8')
await page.click('button:has-text("Scale")')
await page.waitForTimeout(1500)
const gate = await body()
check('scale opens the review dialog', /Your change|Approve and apply/i.test(gate))
check('review dialog shows a diff', /replicas: 8/.test(gate))
check('proposing is not refused', !REFUSAL.test(gate))
await clear()

// And from a rollback, which is a different path into the same dialog.
await rail('Helm')
await page.waitForTimeout(900)
await page.click('.rel.card')
await page.waitForTimeout(1000)
await page.click('text=History')
await page.waitForTimeout(800)
const rb = page.locator('button', { hasText: /roll ?back/i }).first()
check('a rollback is offered', await rb.count() > 0)
if (await rb.count()) {
  await rb.click()
  await page.waitForTimeout(2000)
  const after = await body()
  check('rollback plans and proposes', /Approve and apply/i.test(after))
  check('rollback is not refused', !REFUSAL.test(after))
  check('no-change line is not shown for a change that does something',
    !/already matches; applying would change nothing/i.test(after))
}
await clear()

// Approving is the one thing that must refuse.
await page.click('.rail button[title="Resources"]')
await page.waitForTimeout(400)
await page.click('text=Deployments')
await page.waitForTimeout(900)
await page.click('.row.data:has-text("checkout")')
await page.waitForTimeout(800)
await page.locator('.input.num').first().fill('9')
await page.click('button:has-text("Scale")')
await page.waitForTimeout(1200)
const approve = page.locator('button:has-text("Approve and apply")').first()
if (await approve.count()) {
  await approve.click()
  await page.waitForTimeout(1500)
  check('approving is refused, which is the point', REFUSAL.test(await body()))
}

check('no console errors', consoleErrors.length === 0, consoleErrors.slice(0, 2).join(' | '))

await browser.close()
server.close()
console.log(results.join('\n'))
console.log(failed ? `\n${failed} check${failed === 1 ? '' : 's'} failed` : '\nall checks passed')
process.exit(failed ? 1 : 0)
