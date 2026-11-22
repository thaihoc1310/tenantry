import { describe, expect, it } from 'vitest'
import type { Schemas } from '@/api/client'
import { fromApproval, fromGrant, toApproval } from './grant'

// The seed's onboarding policy (cmd/tenantry-seed).
const proposal: Schemas['Approval'] = {
  entitlement: { maxMembers: 20, maxGroups: 10, maxWorkspacesTotal: 8 },
  clusterGrant: {
    clusterId: 'cluster-a',
    maxWorkspaces: 2,
    maxNamespacesTotal: 6,
    resources: {
      requestsCpu: '2',
      requestsMemory: '4Gi',
      limitsCpu: '4',
      limitsMemory: '8Gi',
      pods: 20,
      requestsStorage: '20Gi',
    },
    sharedPoolIds: ['shared-general'],
    networkPoolIds: ['onboarding-pod-network'],
    allowedStorageClasses: ['standard'],
  },
}

describe('grant form', () => {
  it('approves the proposal unchanged when nothing is edited', () => {
    expect(toApproval(fromApproval(proposal))).toEqual(proposal)
  })
  it('writes fractions the API reads back exactly', () => {
    const m = { ...fromApproval(proposal), rcpu: 0.5, rmem: 1.5 }
    const r = toApproval(m).clusterGrant.resources
    expect([r.requestsCpu, r.requestsMemory]).toEqual(['500m', '1536Mi'])
  })
  it('drops the shared quota without a shared pool', () => {
    const g = toApproval({ ...fromApproval(proposal), pool: '' }).clusterGrant
    expect(g.sharedPoolIds).toEqual([])
    expect(g.resources).toMatchObject({ requestsCpu: '0', limitsMemory: '0Gi', pods: 0, requestsStorage: '20Gi' })
  })
  it('keeps the pools beyond the first, which the form does not show', () => {
    const g = { ...proposal.clusterGrant, sharedPoolIds: ['shared-general', 'gpu'], networkPoolIds: ['a', 'b'] }
    const out = toApproval(fromGrant(g)).clusterGrant
    expect([out.sharedPoolIds, out.networkPoolIds]).toEqual([
      ['shared-general', 'gpu'],
      ['a', 'b'],
    ])
  })
})
