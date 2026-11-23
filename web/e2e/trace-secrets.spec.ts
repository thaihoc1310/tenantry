// The passwords the tests type stay out of their traces (fillSecret, and
// traceRecording without DOM snapshots): a
// trace is recorded here (the runner's own tracing is off for this file), a value typed with fill
// is in it (the check works), a secret typed with fillSecret is not. Local:
// it reaches no server.
import { execFileSync } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, test } from '@playwright/test'
import { fillSecret } from './support'
import { traceRecording } from './trace'

test.use({ trace: 'off' })

test('a secret typed with fillSecret is not in the trace', async ({ browser }) => {
  const dir = mkdtempSync(join(tmpdir(), 'trace-secrets-'))
  const context = await browser.newContext()
  try {
    await context.tracing.start(traceRecording) // as the suite records them
    const page = await context.newPage()
    await page.setContent('<form><input id="user"><input id="pw" type="password"></form>')
    const marker = `visible-${crypto.randomUUID()}`
    const secret = `secret-${crypto.randomUUID()}`
    await page.fill('#user', marker)
    await fillSecret(page, '#pw', secret)
    // The field holds the secret: compared in the page, only a boolean comes back.
    const fn = '__compare'
    await page.exposeFunction(fn, () => secret)
    expect(
      await page
        .locator('#pw')
        .evaluate(
          async (el, name) =>
            (el as HTMLInputElement).value ===
            (await (window as unknown as Record<string, () => Promise<string>>)[name]!()),
          fn,
        ),
    ).toBe(true)
    const zip = join(dir, 'trace.zip')
    await context.tracing.stop({ path: zip })
    const trace = execFileSync('unzip', ['-p', zip], { maxBuffer: 64 << 20 }).toString('latin1')
    expect(trace).toContain(marker)
    expect(trace).not.toContain(secret)
  } finally {
    await context.close()
    rmSync(dir, { recursive: true, force: true })
  }
})
