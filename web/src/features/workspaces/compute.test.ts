import { describe, expect, it } from 'vitest'
import { computePlace } from './compute'

describe('computePlace', () => {
  const nodes = [{ id: 'nodegrant-01', name: 'fdp-k8s-wk3' }]
  it('names the pool of a shared Workspace', () => {
    expect(computePlace({ mode: 'Shared', sharedPoolId: 'shared-general' })).toBe('shared-general')
  })
  it('names the node of a dedicated Workspace, not its grant id', () => {
    expect(computePlace({ mode: 'Dedicated', dedicatedNodeGrantId: 'nodegrant-01' }, nodes)).toBe('fdp-k8s-wk3')
  })
  it('says a dedicated node when the catalog is not readable or misses it', () => {
    expect(computePlace({ mode: 'Dedicated', dedicatedNodeGrantId: 'nodegrant-01' })).toBe('a dedicated node')
    expect(computePlace({ mode: 'Dedicated', dedicatedNodeGrantId: 'nodegrant-02' }, nodes)).toBe('a dedicated node')
  })
})
