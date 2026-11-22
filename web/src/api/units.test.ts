import { describe, expect, it } from 'vitest'
import { cores, cpu, gib, memory, quantity } from './units'

describe('quantities', () => {
  it('reads what the API writes', () => {
    expect(cores('2')).toBe(2)
    expect(cores('1500m')).toBe(1.5)
    expect(gib('4Gi')).toBe(4)
    expect(gib('512Mi')).toBe(0.5)
    expect(gib('3906250Ki')).toBe(3.73)
    expect(quantity('1e3')).toBe(1000)
    expect(() => quantity('lots')).toThrow()
  })
  it('writes what the API reads back unchanged', () => {
    expect(cpu(2)).toBe('2')
    expect(cpu(0.5)).toBe('500m')
    expect(memory(8)).toBe('8Gi')
    expect(memory(1.5)).toBe('1536Mi')
    for (const n of [0, 0.25, 3, 7.5]) expect(cores(cpu(n))).toBe(n)
    for (const n of [0, 0.5, 4, 12]) expect(gib(memory(n))).toBe(n)
  })
})
