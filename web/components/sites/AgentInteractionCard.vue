<script setup lang="ts">
import { reactive } from 'vue'
import type {
  AgentApprovalDecision,
  AgentConsentKind,
  AgentInteraction,
} from '../../types/api'

const props = defineProps<{
  interaction: AgentInteraction
  availableDecisions: AgentApprovalDecision[]
  busy: boolean
}>()

const emit = defineEmits<{
  approve: [decision: AgentApprovalDecision]
  answer: [requestId: string, answers: Record<string, string>]
  consent: [kind: AgentConsentKind, granted: boolean]
}>()

const answers = reactive<Record<string, string>>({})

const decisionLabels: Record<AgentApprovalDecision, string> = {
  approve_once: 'Approve once',
  approve_generation: 'Approve for this generation',
  decline: 'Decline',
  cancel: 'Cancel generation',
}

function submitAnswers() {
  emit('answer', props.interaction.requestId, { ...answers })
}
</script>

<template>
  <article class="agent-interaction" :class="`agent-interaction--${interaction.kind}`">
    <p class="eyebrow">
      {{ interaction.kind === 'question' ? 'Clarification needed' : interaction.kind === 'consent' ? 'Explicit consent' : 'Approval required' }}
    </p>
    <h4 v-if="interaction.summary">{{ interaction.summary }}</h4>

    <ul v-if="interaction.affectedPaths?.length" class="agent-interaction__paths" aria-label="Affected paths">
      <li v-for="path in interaction.affectedPaths" :key="path"><code>{{ path }}</code></li>
    </ul>

    <form v-if="interaction.kind === 'question'" class="agent-question" @submit.prevent="submitAnswers">
      <label v-for="question in interaction.questions || []" :key="question.id">
        <span><strong>{{ question.header }}</strong> {{ question.question }}</span>
        <select v-if="question.options?.length" v-model="answers[question.id]" required>
          <option value="" disabled>Select an answer</option>
          <option v-for="option in question.options" :key="option.label" :value="option.label">
            {{ option.label }}{{ option.description ? ` — ${option.description}` : '' }}
          </option>
          <option v-if="question.isOther" value="other">Other</option>
        </select>
        <input v-else v-model="answers[question.id]" :type="question.isSecret ? 'password' : 'text'" required>
      </label>
      <button class="compact-button compact-button--primary" type="submit" :disabled="busy">{{ busy ? 'Sending…' : 'Send answer' }}</button>
    </form>

    <div v-else-if="interaction.kind === 'consent' && interaction.consentKind" class="agent-consent">
      <p>
        {{ interaction.consentKind === 'dependency_change'
          ? 'Allow dependency manifest or lockfile changes for this generation only.'
          : 'Allow network access for this generation only.' }}
      </p>
      <div class="button-row">
        <button class="compact-button compact-button--primary" type="button" data-consent="grant" :disabled="busy" @click="emit('consent', interaction.consentKind!, true)">Allow once</button>
        <button class="compact-button" type="button" data-consent="decline" :disabled="busy" @click="emit('consent', interaction.consentKind!, false)">Decline</button>
      </div>
    </div>

    <div v-else-if="interaction.kind === 'approval'" class="agent-interaction__decisions">
      <button
        v-for="decision in availableDecisions"
        :key="decision"
        class="compact-button"
        :class="{
          'compact-button--primary': decision === 'approve_once' || decision === 'approve_generation',
          'compact-button--danger': decision === 'cancel',
        }"
        type="button"
        :data-decision="decision"
        :disabled="busy"
        @click="emit('approve', decision)"
      >{{ decisionLabels[decision] }}</button>
    </div>
  </article>
</template>
