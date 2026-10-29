<script setup lang="ts">
import { computed, ref } from 'vue'
import AgentChangedFiles from './AgentChangedFiles.vue'
import AgentInteractionCard from './AgentInteractionCard.vue'
import type {
  AgentApprovalDecision,
  AgentConsentKind,
  AgentConversation,
  AgentConversationItem,
  AgentReadiness,
} from '../../types/api'

const props = defineProps<{
  readiness: AgentReadiness
  conversations: AgentConversation[]
  conversation?: AgentConversation | undefined
  busy: boolean
  error: string
}>()

const emit = defineEmits<{
  create: [prompt: string]
  open: [conversationId: string]
  'follow-up': [prompt: string]
  answer: [requestId: string, answers: Record<string, string>]
  approve: [requestId: string, decision: AgentApprovalDecision]
  consent: [kind: AgentConsentKind, granted: boolean]
  interrupt: []
}>()

const prompt = ref('')
const followUpPrompt = ref('')
const availableDecisions: AgentApprovalDecision[] = ['approve_once', 'approve_generation', 'decline', 'cancel']

const retainedConversations = computed(() => props.conversations.filter(item => item.id !== props.conversation?.id))
const baselineFiles = computed(() => props.conversation?.changedFiles?.filter(file => file.baselineDirty) || [])
const conversationFiles = computed(() => props.conversation?.changedFiles?.filter(file => !file.baselineDirty) || [])
const canInterrupt = computed(() => ['active', 'waiting', 'interrupting'].includes(props.conversation?.status || ''))
const canFollowUp = computed(() => Boolean(
  props.conversation
  && props.conversation.bindingValid
  && !props.conversation.readOnly
  && ['ready', 'completed', 'interrupted', 'failed'].includes(props.conversation.status),
))
const activityState = computed(() => {
  const conversation = props.conversation
  if (!conversation) return undefined
  if (conversation.pendingInteraction) return { label: 'Waiting for your response', animated: false }
  if (conversation.registry.state === 'running') return { label: 'Rebuilding component registry…', animated: true }
  if (conversation.status === 'interrupting') return { label: 'Stopping the current turn…', animated: true }
  if (conversation.status === 'active') return { label: 'Codex is working…', animated: true }
  return undefined
})
const unavailableGuidance = computed(() => {
  if (props.readiness.available) return ''
  if (props.readiness.problem?.toLowerCase().includes('control')) {
    return 'The CouchFusion control database is unavailable. Check CouchDB, then reload this site.'
  }
  return {
    missing: 'Install Codex locally, then restart CouchFusion.',
    launch_failed: 'Could not start the local Codex service. Check the system page for diagnostics.',
    unsupported: 'Update Codex to a supported version, then restart CouchFusion.',
    unauthenticated: 'Run codex login locally, then reload this site.',
    restarting: 'Codex is restarting. Wait a moment and try again.',
    ready: '',
  }[props.readiness.status]
})

function submitCreate() {
  const value = prompt.value.trim()
  if (!value) return
  emit('create', value)
  prompt.value = ''
}

function submitFollowUp() {
  const value = followUpPrompt.value.trim()
  if (!value) return
  emit('follow-up', value)
  followUpPrompt.value = ''
}

function itemRole(item: AgentConversationItem) {
  return {
    userMessage: 'You',
    agentMessage: 'Codex',
    reasoning: 'Reasoning summary',
    commandExecution: 'Command',
    fileChange: 'File change',
    unknown: 'Update',
  }[item.type]
}

function visibleItemText(item: AgentConversationItem) {
  const text = item.text || ''
  return item.type === 'commandExecution' ? text.slice(0, 4000) : text
}

function statusLabel(status: string) {
  return status.replaceAll('_', ' ')
}
</script>

<template>
  <section class="component-generator" aria-label="Create component with Codex">
    <header class="component-generator__header">
      <div>
        <p class="eyebrow">Local generation agent</p>
        <h3>Create component with Codex</h3>
      </div>
      <span v-if="readiness.available" class="status-lozenge">Codex ready</span>
    </header>

    <p v-if="error" class="notice notice--error" role="alert">{{ error }}</p>
    <p v-if="unavailableGuidance" class="notice notice--error" role="alert">{{ unavailableGuidance }}</p>

    <form v-if="readiness.available && !conversation" class="agent-composer" @submit.prevent="submitCreate">
      <label for="agent-component-prompt">Describe the component</label>
      <textarea
        id="agent-component-prompt"
        v-model="prompt"
        rows="3"
        maxlength="16384"
        placeholder="Describe its content, behavior, responsive layout, and visual tone."
        required
      />
      <div class="agent-composer__footer">
        <small>Include useful defaults and any child-content behavior.</small>
        <button class="compact-button compact-button--primary" type="submit" :disabled="busy || !prompt.trim()">{{ busy ? 'Starting…' : 'Generate' }}</button>
      </div>
    </form>

    <article v-if="conversation" class="agent-conversation" :data-conversation-active="conversation.id">
      <header class="agent-conversation__header">
        <div>
          <p class="eyebrow">Active conversation</p>
          <h4>{{ conversation.title }}</h4>
        </div>
        <div class="agent-conversation__state" aria-live="polite">
          <span>{{ statusLabel(conversation.status) }}</span>
          <button v-if="canInterrupt" class="compact-button compact-button--danger" type="button" data-action="interrupt" :disabled="busy" @click="emit('interrupt')">
            {{ conversation.status === 'interrupting' ? 'Interrupting…' : 'Interrupt' }}
          </button>
        </div>
      </header>

      <p v-if="!conversation.bindingValid" class="notice notice--error" role="alert">
        This site location has changed since the conversation started. Restore the binding to resume; this conversation is read-only.
      </p>

      <ol v-if="conversation.items?.length || activityState" class="agent-transcript" aria-label="Conversation transcript">
        <li v-for="item in conversation.items" :key="item.id" :data-item="item.type">
          <details v-if="item.type === 'reasoning'" class="agent-message agent-message--reasoning">
            <summary>{{ itemRole(item) }}</summary>
            <p>{{ visibleItemText(item) || 'Summary not available' }}</p>
          </details>
          <article v-else class="agent-message" :class="`agent-message--${item.type}`">
            <header>
              <strong>{{ itemRole(item) }}</strong>
              <span v-if="item.status">{{ item.status }}</span>
            </header>
            <p>{{ visibleItemText(item) }}</p>
          </article>
        </li>
        <li
          v-if="activityState"
          class="agent-activity"
          data-agent-activity
          :data-animated="String(activityState.animated)"
          aria-live="polite"
        >
          <span class="agent-activity__signal" aria-hidden="true"><i /><i /><i /></span>
          <span>{{ activityState.label }}</span>
        </li>
      </ol>

      <AgentInteractionCard
        v-if="conversation.pendingInteraction"
        :interaction="conversation.pendingInteraction"
        :available-decisions="conversation.pendingInteraction.kind === 'approval' ? availableDecisions : []"
        :busy="busy"
        @answer="(requestId, answers) => emit('answer', requestId, answers)"
        @approve="decision => emit('approve', conversation!.pendingInteraction!.requestId, decision)"
        @consent="(kind, granted) => emit('consent', kind, granted)"
      />

      <p v-if="conversation.registry.state === 'failed'" class="notice notice--error" role="alert">
        Registry rebuild needs attention. {{ conversation.registry.problem }}
      </p>
      <a v-if="conversation.generatedComponentId" class="agent-generated-link" :href="`#component-${conversation.generatedComponentId}`">
        View generated component <code>{{ conversation.generatedComponentId }}</code>
      </a>

      <AgentChangedFiles :baseline="baselineFiles" :changed="conversationFiles" />

      <form v-if="canFollowUp" class="agent-composer agent-composer--follow-up" @submit.prevent="submitFollowUp">
        <label for="agent-follow-up">Continue this conversation</label>
        <textarea id="agent-follow-up" v-model="followUpPrompt" rows="2" maxlength="16384" required />
        <div class="agent-composer__footer">
          <small>The original Codex thread and site context will be retained.</small>
          <button class="compact-button compact-button--primary" type="submit" data-action="follow-up" :disabled="busy || !followUpPrompt.trim()">Send follow-up</button>
        </div>
      </form>
    </article>

    <section v-if="retainedConversations.length" class="agent-history" aria-label="Previous component conversations">
      <h4>Previous conversations</h4>
      <article v-for="item in retainedConversations" :key="item.id" class="agent-history__row" :data-conversation="item.id">
        <div>
          <strong>{{ item.title }}</strong>
          <small>{{ statusLabel(item.status) }} · {{ new Date(item.updatedAt).toLocaleString() }}</small>
        </div>
        <code v-if="item.generatedComponentId">{{ item.generatedComponentId }}</code>
        <button class="compact-button" type="button" :disabled="busy" @click="emit('open', item.id)">Resume</button>
      </article>
    </section>
  </section>
</template>
