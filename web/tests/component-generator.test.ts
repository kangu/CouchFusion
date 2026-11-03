import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AgentChangedFiles from '../components/sites/AgentChangedFiles.vue'
import AgentInteractionCard from '../components/sites/AgentInteractionCard.vue'
import ComponentGenerator from '../components/sites/ComponentGenerator.vue'
import type { AgentConversation, AgentInteraction, AgentReadiness } from '../types/api'

const ready: AgentReadiness = { status: 'ready', available: true }

function conversation(overrides: Partial<AgentConversation> = {}): AgentConversation {
  return {
    id: 'c1', workspaceId: 'w1', siteId: 'shop', title: 'Editorial hero', status: 'active',
    registry: { state: 'not_needed' }, sequence: 2, bindingValid: true,
    createdAt: '2026-07-14T12:00:00Z', updatedAt: '2026-07-14T12:01:00Z',
    ...overrides,
  }
}

describe('component generator', () => {
  it('starts with a naturally sized labelled prompt and generate action', async () => {
    const wrapper = mount(ComponentGenerator, {
      props: { readiness: ready, conversations: [], busy: false, error: '' },
    })

    const controls = wrapper.findAll('textarea, button')
    expect(controls[0]?.element.tagName).toBe('TEXTAREA')
    expect(wrapper.get('textarea').attributes('rows')).toBe('3')
    expect(wrapper.get('textarea').attributes('maxlength')).toBe('16384')
    expect(wrapper.get('label').text()).toContain('Describe the component')
    await wrapper.get('textarea').setValue('Create a quiet editorial hero')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('create')?.[0]).toEqual(['Create a quiet editorial hero'])
  })

  it.each([
    [{ status: 'missing', available: false }, 'Install Codex'],
    [{ status: 'launch_failed', available: false }, 'Could not start'],
    [{ status: 'unsupported', available: false }, 'Update Codex'],
    [{ status: 'unauthenticated', available: false }, 'codex login'],
    [{ status: 'launch_failed', available: false, problem: 'CouchFusion agent control data is unavailable' }, 'control database'],
  ] as Array<[AgentReadiness, string]>)('shows actionable unavailable guidance for %s', (readiness, guidance) => {
    const wrapper = mount(ComponentGenerator, {
      props: { readiness, conversations: [], busy: false, error: '' },
    })
    expect(wrapper.get('[role="alert"]').text()).toContain(guidance)
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.find('[data-action="rebuild-registry"]').exists()).toBe(false)
  })

  it('marks a changed site binding read-only with specific guidance', () => {
    const wrapper = mount(ComponentGenerator, {
      props: {
        readiness: ready,
        conversations: [conversation({ bindingValid: false, readOnly: true })],
        conversation: conversation({ bindingValid: false, readOnly: true }),
        busy: false,
        error: '',
      },
    })
    expect(wrapper.get('[role="alert"]').text()).toContain('site location has changed')
    expect(wrapper.find('[data-action="follow-up"]').exists()).toBe(false)
  })

  it('renders a compact transcript, safe activity details, interrupt, registry states, and follow-up', async () => {
    const active = conversation({
      items: [
        { id: 'u1', type: 'userMessage', text: 'Create a hero' },
        { id: 'a1', type: 'agentMessage', text: 'I will inspect the content conventions.' },
        { id: 'r1', type: 'reasoning', text: 'Inspect registry metadata and existing component patterns.' },
        { id: 'x1', type: 'commandExecution', status: 'inProgress', text: 'Searching project files' },
      ],
    })
    const wrapper = mount(ComponentGenerator, {
      props: { readiness: ready, conversations: [active], conversation: active, busy: false, error: '' },
    })
    expect(wrapper.get('ol[aria-label="Conversation transcript"]').text()).toContain('Create a hero')
    expect(wrapper.get('[data-item="reasoning"]').text()).toContain('Inspect registry metadata')
    expect(wrapper.get('[data-item="commandExecution"]').text()).toContain('Searching project files')
    expect(wrapper.get('[aria-live="polite"]').text()).toContain('active')
    await wrapper.get('[data-action="interrupt"]').trigger('click')
    expect(wrapper.emitted('interrupt')).toHaveLength(1)

    await wrapper.setProps({ conversation: conversation({ items: [{ id: 'r2', type: 'reasoning', text: '' }] }) })
    expect(wrapper.get('[data-item="reasoning"]').text()).toContain('Summary not available')

    await wrapper.setProps({ conversation: conversation({ status: 'rebuilding_registry', registry: { state: 'running' } }) })
    expect(wrapper.text()).toContain('Rebuilding component registry')
    await wrapper.setProps({ conversation: conversation({ status: 'ready', registry: { state: 'failed', problem: 'Build failed' }, generatedComponentId: 'hero' }) })
    expect(wrapper.get('[role="alert"]').text()).toContain('Build failed')
    expect(wrapper.get('a[href="#component-hero"]').text()).toContain('hero')
    expect(wrapper.find('[data-action="follow-up"]').exists()).toBe(true)
  })

  it.each([
    [conversation({ status: 'active', items: [] }), 'Codex is working', 'true'],
    [conversation({ status: 'interrupting', items: [] }), 'Stopping the current turn', 'true'],
    [conversation({ status: 'waiting', items: [], pendingInteraction: { requestId: 'q1', kind: 'question', questions: [] } }), 'Waiting for your response', 'false'],
    [conversation({ status: 'rebuilding_registry', items: [], registry: { state: 'running' } }), 'Rebuilding component registry', 'true'],
  ] as const)('appends the current activity for %s', (current, label, animated) => {
    const wrapper = mount(ComponentGenerator, {
      props: { readiness: ready, conversations: [current], conversation: current, busy: false, error: '' },
    })
    const transcriptItems = wrapper.findAll('ol[aria-label="Conversation transcript"] > li')
    const activity = wrapper.get('[data-agent-activity]')
    expect(activity.text()).toContain(label)
    expect(activity.attributes('data-animated')).toBe(animated)
    expect(transcriptItems.at(-1)?.attributes('data-agent-activity')).toBe('')
  })

  it.each(['ready', 'failed', 'interrupted', 'completed'] as const)('removes transient activity in the %s state', (status) => {
    const current = conversation({ status, items: [] })
    const wrapper = mount(ComponentGenerator, {
      props: { readiness: ready, conversations: [current], conversation: current, busy: false, error: '' },
    })
    expect(wrapper.find('[data-agent-activity]').exists()).toBe(false)
  })

  it('renders retained conversations as compact resume rows', async () => {
    const retained = conversation({ id: 'older', title: 'Testimonial rail', status: 'ready', generatedComponentId: 'testimonial' })
    const wrapper = mount(ComponentGenerator, {
      props: { readiness: ready, conversations: [retained], busy: false, error: '' },
    })
    expect(wrapper.get('[data-conversation="older"]').text()).toContain('Testimonial rail')
    await wrapper.get('[data-conversation="older"] button').trigger('click')
    expect(wrapper.emitted('open')?.[0]).toEqual(['older'])
  })
})

describe('agent interaction card', () => {
  it.each(['command', 'file_change'] as const)('offers only its declared approval decisions for %s', async (action) => {
    const interaction: AgentInteraction = {
      requestId: 'r1', kind: 'approval', action, summary: 'Update the hero source', affectedPaths: ['app/components/content/Hero.vue'],
    }
    const decisions = ['approve_once', 'approve_generation', 'decline', 'cancel'] as const
    const wrapper = mount(AgentInteractionCard, {
      props: { interaction, availableDecisions: [...decisions], busy: false },
    })
    for (const decision of decisions) await wrapper.get(`[data-decision="${decision}"]`).trigger('click')
    expect(wrapper.emitted('approve')?.map(([decision]) => decision)).toEqual(expect.arrayContaining([...decisions]))
    expect(wrapper.get('code').text()).toContain('app/components/content/Hero.vue')
  })

  it.each(['dependency_change', 'network_access'] as const)('requires typed %s consent for this generation only', async (kind) => {
    const interaction: AgentInteraction = { requestId: 'r2', kind: 'consent', consentKind: kind, summary: 'Protected operation' }
    const wrapper = mount(AgentInteractionCard, {
      props: { interaction, availableDecisions: ['approve_once', 'approve_generation', 'decline', 'cancel'], busy: false },
    })
    expect(wrapper.text()).toContain('this generation only')
    expect(wrapper.find('[data-decision="approve_generation"]').exists()).toBe(false)
    await wrapper.get('[data-consent="grant"]').trigger('click')
    expect(wrapper.emitted('consent')?.[0]).toEqual([kind, true])
  })

  it('collects focused question answers', async () => {
    const interaction: AgentInteraction = {
      requestId: 'r3', kind: 'question', questions: [{ id: 'tone', header: 'Tone', question: 'Which tone?', options: [{ label: 'Quiet' }, { label: 'Bold' }] }],
    }
    const wrapper = mount(AgentInteractionCard, { props: { interaction, availableDecisions: [], busy: false } })
    await wrapper.get('select').setValue('Quiet')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('answer')?.[0]).toEqual(['r3', { tone: 'Quiet' }])
  })
})

describe('agent changed files', () => {
  it('separates baseline work from conversation changes and uses relative code paths', () => {
    const wrapper = mount(AgentChangedFiles, {
      props: {
        baseline: [{ path: 'app/components/content/Existing.vue', kind: 'modified', baselineDirty: true }],
        changed: [{ path: 'app/components/content/Hero.vue', kind: 'added' }],
      },
    })
    expect(wrapper.get('[data-file-group="baseline"]').text()).toContain('Existing.vue')
    expect(wrapper.get('[data-file-group="conversation"]').text()).toContain('Hero.vue')
    expect(wrapper.findAll('code')).toHaveLength(2)
  })
})
