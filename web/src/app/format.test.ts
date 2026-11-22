import { describe, expect, it } from 'vitest'
import { expiresIn } from './format'

describe('expiresIn', () => {
  const now = Date.parse('2026-11-22T12:00:00Z')
  it('counts whole days, then hours, then minutes', () => {
    expect(expiresIn('2026-11-29T12:00:00Z', now)).toBe('in 7 days')
    expect(expiresIn('2026-11-29T11:59:00Z', now)).toBe('in 6 days')
    expect(expiresIn('2026-11-22T15:30:00Z', now)).toBe('in 3 hours')
    expect(expiresIn('2026-11-22T12:00:20Z', now)).toBe('in 1 minute')
  })
  it('says expired once the time has passed', () => {
    expect(expiresIn('2026-11-22T12:00:00Z', now)).toBe('expired')
    expect(expiresIn('2026-11-01T00:00:00Z', now)).toBe('expired')
  })
})
