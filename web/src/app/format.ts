// "21 Nov 2026, 14:02", as on the mockups, in the viewer's time zone.
const fmt = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

export const when = (iso: string) => fmt.format(new Date(iso))

const rel = new Intl.RelativeTimeFormat('en', { numeric: 'always' })

// "in 6 days", "in 3 hours"; "expired" once the time has passed. For
// deadlines: invitation links, ownership transfers.
export function expiresIn(iso: string, now = Date.now()): string {
  const ms = new Date(iso).getTime() - now
  if (ms <= 0) return 'expired'
  const hours = ms / 3_600_000
  if (hours >= 24) return rel.format(Math.floor(hours / 24), 'day')
  if (hours >= 1) return rel.format(Math.floor(hours), 'hour')
  return rel.format(Math.max(1, Math.floor(ms / 60_000)), 'minute')
}
