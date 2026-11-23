// Where a Workspace's Pods run, in words: the pool, or the dedicated node by
// its name. The node's name comes from the Organization's catalog, which only
// its OrgOwner and OrgAdmins read; others see "a dedicated node", never the
// grant's id.
import type { Schemas } from '@/api/client'

export function computePlace(c: Schemas['WorkspaceCompute'], nodes?: { id: string; name: string }[]): string {
  if (c.mode === 'Shared') return c.sharedPoolId ?? 'a shared pool'
  return nodes?.find((n) => n.id === c.dedicatedNodeGrantId)?.name ?? 'a dedicated node'
}
