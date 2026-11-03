import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { commitDraft, noteStamp } from '../composables/useGitRepos'
import DevGitDrawer from '../components/dev/DevGitDrawer.vue'
import type { RepoStatus, WorkbenchNote } from '../types/api'

const stamp = (iso: string) => iso.slice(11, 16)

/** Overrides may set `text: undefined` to leave the text out. */
function note(overrides: { [K in keyof WorkbenchNote]?: WorkbenchNote[K] | undefined } = {}): WorkbenchNote {
  const merged: Record<string, unknown> = { jobId: 'r1', type: 'llm.request', completedAt: '2026-11-03T10:16:00Z', title: 'Shortened the hero', text: 'Shortened the hero. Added a contact CTA.', files: ['app/a.vue'], ...overrides }
  if (merged.text === undefined) delete merged.text
  return merged as unknown as WorkbenchNote
}

describe('commit draft', () => {
  it('uses the summary of a single task', () => {
    expect(commitDraft(undefined)).toBe('')
    expect(commitDraft([note()], stamp)).toBe('Shortened the hero\n\nShortened the hero. Added a contact CTA.')
    expect(commitDraft([note({ text: undefined })], stamp)).toBe('Shortened the hero')
  })

  it('adds timestamped notes when tasks pile up', () => {
    const draft = commitDraft([note(), note({ jobId: 'g1', type: 'component.generate', completedAt: '2026-11-03T11:40:00Z', title: 'Add component network-pricing', text: undefined })], stamp)
    expect(draft).toBe('Shortened the hero (+1 more)\n\n- [10:16] Shortened the hero\n  Shortened the hero. Added a contact CTA.\n- [11:40] Add component network-pricing')
    const long = commitDraft([note({ title: 'x'.repeat(72) }), note()], stamp)
    expect(long.split('\n')[0]!.length).toBeLessThanOrEqual(72)
  })

  it('stamps in local time', () => {
    expect(noteStamp('2026-11-03T10:16:00Z')).toMatch(/^2026-11-0\d \d\d:\d\d$/)
  })
})

describe('git drawer commit message', () => {
  function status(notes?: WorkbenchNote[]): RepoStatus {
    const value: RepoStatus = { id: 'apps/radustanciu', branch: 'main', hasRemote: false, pushBranches: [], files: [{ path: 'app/a.vue', index: '.', worktree: 'M', untracked: false }] }
    if (notes) value.workbenchNotes = notes
    return value
  }
  const base = { repos: [], selectedRepo: 'apps/radustanciu', error: '', committing: false }
  const text = (w: ReturnType<typeof mount>) => (w.get('[data-testid="commit-message"]').element as HTMLTextAreaElement).value

  it('pre-fills, follows new tasks, and keeps an edit', async () => {
    const wrapper = mount(DevGitDrawer, { props: { ...base, status: status([note()]) } })
    expect(text(wrapper)).toContain('Shortened the hero')
    await wrapper.setProps({ status: status([note(), note({ jobId: 'r2', title: 'Added pricing', text: undefined })]) })
    expect(text(wrapper)).toContain('(+1 more)')
    expect(text(wrapper)).toContain('Added pricing')

    await wrapper.get('[data-testid="commit-message"]').setValue('My own message')
    await wrapper.setProps({ status: status([note(), note({ jobId: 'r2', title: 'Added pricing' }), note({ jobId: 'r3', title: 'Third' })]) })
    expect(text(wrapper)).toBe('My own message')
    expect(wrapper.get('[data-testid="commit-suggested"]').text()).toContain('Your edit is kept')
    await wrapper.get('[data-testid="commit-suggested"] button').trigger('click')
    expect(text(wrapper)).toContain('(+2 more)')
  })

  it('leaves the message alone without workbench tasks', () => {
    const wrapper = mount(DevGitDrawer, { props: { ...base, status: status() } })
    expect(text(wrapper)).toBe('')
    expect(wrapper.find('[data-testid="commit-suggested"]').exists()).toBe(false)
  })
})
