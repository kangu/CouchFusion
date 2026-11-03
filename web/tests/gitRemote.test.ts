import { describe, expect, it } from 'vitest'
import { remoteActions } from '../composables/useGitRepos'
import type { RepoStatus } from '../types/api'

const base: RepoStatus = { id: 'apps/radustanciu', branch: 'main', hasRemote: true, pushBranches: ['main'], files: [] }

describe('remoteActions', () => {
  it('blocks everything without a remote', () => {
    const a = remoteActions({ ...base, hasRemote: false })
    expect(a).toMatchObject({ canFetch: false, canPull: false, canPush: false })
  })

  it('allows the first push when nothing is tracked yet', () => {
    const a = remoteActions(base)
    expect(a).toMatchObject({ canFetch: true, canPull: false, canPush: true, summary: 'Not tracking a remote branch yet' })
  })

  it('only allows configured branches', () => {
    const a = remoteActions({ ...base, branch: 'feature', upstream: 'origin/feature', ahead: 1, behind: 0 })
    expect(a.canPush).toBe(false)
    expect(a.pushBlocked).toBe('Only main may be pushed.')
  })

  it('asks to pull first when behind, and enables pull', () => {
    const a = remoteActions({ ...base, upstream: 'origin/main', ahead: 1, behind: 2 })
    expect(a).toMatchObject({ canPull: true, canPush: false, pushBlocked: 'Pull first: the remote has newer commits.' })
    expect(a.summary).toBe('origin/main · 1 ahead · 2 behind')
  })

  it('has nothing to push when up to date', () => {
    const a = remoteActions({ ...base, upstream: 'origin/main', ahead: 0, behind: 0 })
    expect(a).toMatchObject({ canPush: false, canPull: false, pushBlocked: 'Nothing to push.' })
  })

  it('pushes when ahead', () => {
    expect(remoteActions({ ...base, upstream: 'origin/main', ahead: 3, behind: 0 }).canPush).toBe(true)
  })

  it('treats a server without pushBranches (older backend) as no remote config', () => {
    const a = remoteActions({ ...base, pushBranches: [] })
    expect(a).toMatchObject({ canFetch: false, canPush: false, pushBlocked: 'This repo has no remote in server.toml.' })
  })
})
