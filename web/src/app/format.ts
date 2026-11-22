// "21 Nov 2026, 14:02", as on the mockups, in the viewer's time zone.
const fmt = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

export const when = (iso: string) => fmt.format(new Date(iso))
