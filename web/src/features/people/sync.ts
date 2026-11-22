// How a sync status reads on screen (group and group member alike).
import type { Schemas } from '@/api/client'

export function syncWords(s: Schemas['SyncStatus']): { label: string; tone: string; why: string } {
  if (s === 'Synced') return { label: 'Synced', tone: 'ok', why: '' }
  if (s === 'Error')
    return {
      label: 'Sync failing',
      tone: 'bad',
      why: 'Keycloak refused the change or could not be reached. Tenantry keeps retrying; tokens are not up to date yet.',
    }
  return {
    label: 'Sync pending',
    tone: 'warn',
    why: 'Waiting for Keycloak, usually a few seconds. Tokens do not reflect this change yet.',
  }
}
