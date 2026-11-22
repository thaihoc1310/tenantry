// Kubernetes quantities as the API speaks them ("500m", "4Gi") and the
// numbers the portal shows (cores, Gi).

const suffix: Record<string, number> = {
  '': 1,
  m: 1e-3,
  k: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
  Ki: 2 ** 10,
  Mi: 2 ** 20,
  Gi: 2 ** 30,
  Ti: 2 ** 40,
  Pi: 2 ** 50,
  Ei: 2 ** 60,
}

// quantity parses a Kubernetes quantity into base units (cores, bytes).
export function quantity(q: string): number {
  const m = /^([+-]?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)(m|k|M|G|T|P|E|Ki|Mi|Gi|Ti|Pi|Ei)?$/.exec(q.trim())
  if (!m) throw new Error(`not a quantity: ${q}`)
  return Number(m[1]) * (suffix[m[2] ?? ''] ?? 1)
}

const round = (n: number, digits = 3) => Math.round(n * 10 ** digits) / 10 ** digits

export const cores = (q: string) => round(quantity(q))
export const gib = (q: string) => round(quantity(q) / 2 ** 30, 2)

// cpu writes cores as the API stores them: whole cores, else millicores.
export function cpu(n: number): string {
  return Number.isInteger(n) ? String(n) : `${Math.round(n * 1000)}m`
}

// memory writes Gi, else Mi for a fraction of one.
export function memory(n: number): string {
  return Number.isInteger(n) ? `${n}Gi` : `${Math.round(n * 1024)}Mi`
}
