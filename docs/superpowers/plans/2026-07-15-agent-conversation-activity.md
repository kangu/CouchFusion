# Agent Conversation Activity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Populate safe reasoning and command rows and append an accessible animated progress row to active component-generation transcripts.

**Architecture:** The Go manager remains the disclosure boundary: it retains only Codex reasoning summaries and maps structured command-action types to fixed labels. Vue consumes the existing item fields and derives transient progress from conversation state, requiring no API or persistence migration.

**Tech Stack:** Go 1.24, Codex app-server JSONL, Vue 3/Nuxt 4, TypeScript, Vitest, Vue Test Utils, CSS.

## Global Constraints

- Never expose raw reasoning, commands, command output, absolute working directories, process identifiers, or unparsed arguments.
- Map only `read`, `listFiles`, and `search`; all other actions use `Running a project command`.
- Preserve the existing `ConversationItem` and CouchDB schema.
- Pending interactions take precedence over animation; respect `prefers-reduced-motion`.
- Leave changes uncommitted for user review.

## File Structure

- `internal/agent/conversations/manager.go`: safe normalization and reasoning merge.
- `internal/agent/conversations/manager_test.go`: backend behavior and disclosure tests.
- `web/components/sites/ComponentGenerator.vue`: reasoning fallback and activity row.
- `web/assets/css/main.css`: activity animation and reduced-motion rule.
- `web/tests/component-generator.test.ts`: presentation tests.

---

### Task 1: Backend Activity Normalization

**Files:**
- Modify: `internal/agent/conversations/manager.go:585-615,1002-1045`
- Test: `internal/agent/conversations/manager_test.go`

**Interfaces:**
- Consumes: `item/started`, `item/reasoning/summaryTextDelta`, and `item/completed`.
- Produces: `func commandActivityLabel(actions []commandAction) string` and safe `ConversationItem.Text`.

- [x] **Step 1: Write failing command-label tests**

Add `TestNormalizeItemUsesSafeCommandActivityLabels`. Pass command items with `commandActions` values `read`, `listFiles`, `search`, `unknown`, and empty. Assert the respective labels `Reading project files`, `Listing project files`, `Searching project files`, and the generic fallback. Include raw `command`, `cwd`, and `aggregatedOutput` canaries and assert none occur in `item.Text`.

Representative assertion:

```go
raw := json.RawMessage(`{"id":"command","type":"commandExecution","status":"inProgress","command":"do-not-expose","cwd":"/private/site","aggregatedOutput":"do-not-expose-output","commandActions":[{"type":"search","command":"rg password /private"}]}`)
item, ok := normalizeItem(raw, "turn-1")
if !ok || item.Text != "Searching project files" { t.Fatalf("item = %#v", item) }
for _, forbidden := range []string{"do-not-expose", "/private", "password"} {
	if strings.Contains(item.Text, forbidden) { t.Fatalf("exposed %q in %q", forbidden, item.Text) }
}
```

- [x] **Step 2: Write failing reasoning-merge tests**

Add `TestCompletedReasoningPreservesStreamedSummaryUnlessFinalSummaryExists`. Create a real test manager and active conversation. Send a reasoning `item/started`, `summaryTextDelta`, then `item/completed` through the existing event channel. In one subtest the completed summary is empty and must preserve `Inspecting component conventions.`; in another it is `Component conventions inspected.` and must replace the streamed draft.

Use helpers that marshal these real protocol payloads:

```go
protocol.ItemNotification{ThreadID: threadID, TurnID: turnID, Item: rawItem}
map[string]any{"threadId": threadID, "turnId": turnID, "itemId": itemID, "summaryIndex": 0, "delta": delta}
```

- [x] **Step 3: Verify RED**

```sh
go test ./internal/agent/conversations -run 'TestNormalizeItemUsesSafeCommandActivityLabels|TestCompletedReasoningPreservesStreamedSummaryUnlessFinalSummaryExists' -count=1
```

Expected: FAIL because commands use `Command execution` and empty completed reasoning erases streamed text.

- [x] **Step 4: Implement safe command labels**

Add:

```go
type commandAction struct {
	Type string `json:"type"`
}

func commandActivityLabel(actions []commandAction) string {
	seen := map[string]bool{}
	for _, action := range actions { seen[action.Type] = true }
	switch {
	case seen["search"]:
		return "Searching project files"
	case seen["listFiles"]:
		return "Listing project files"
	case seen["read"]:
		return "Reading project files"
	default:
		return "Running a project command"
	}
}
```

Add `CommandActions []commandAction` with the `commandActions` JSON tag to the private raw item shape and use the helper for `commandExecution`. Do not deserialize other command fields.

- [x] **Step 5: Preserve streamed reasoning on empty completion**

Before replacing an existing item in `handleItem`:

```go
if item.Type == "reasoning" && item.Text == "" && state.conversation.Items[index].Text != "" {
	item.Text = state.conversation.Items[index].Text
}
state.conversation.Items[index] = item
```

- [x] **Step 6: Verify GREEN**

```sh
gofmt -w internal/agent/conversations/manager.go internal/agent/conversations/manager_test.go
go test ./internal/agent/conversations -count=1
```

Expected: PASS.

### Task 2: Transcript Progress Presentation

**Files:**
- Modify: `web/components/sites/ComponentGenerator.vue:20-95,146-165`
- Modify: `web/assets/css/main.css:281-291`
- Test: `web/tests/component-generator.test.ts`

**Interfaces:**
- Consumes: `AgentConversation.status`, `registry.state`, `pendingInteraction`, and safe item text.
- Produces: `activityState` and `data-agent-activity` / `data-item` test hooks.

- [x] **Step 1: Write failing transcript tests**

Update `renders a compact transcript...` to assert:

```ts
expect(wrapper.get('[data-item="reasoning"]').text()).toContain('Inspect registry metadata')
expect(wrapper.get('[data-item="commandExecution"]').text()).toContain('Searching project files')
await wrapper.setProps({ conversation: conversation({ items: [{ id: 'r2', type: 'reasoning', text: '' }] }) })
expect(wrapper.get('[data-item="reasoning"]').text()).toContain('Summary not available')
```

- [x] **Step 2: Write failing activity tests**

Add table cases for active/animated `Codex is working`, interrupting/animated `Stopping the current turn`, pending-interaction/static `Waiting for your response`, and registry-running/animated `Rebuilding component registry`. Assert the activity element is the transcript's last `li`. Assert it is absent for `ready`, `failed`, and `interrupted`.

- [x] **Step 3: Verify RED**

```sh
cd web && bun run test:run -- tests/component-generator.test.ts
```

Expected: FAIL because the fallback and activity row do not exist.

- [x] **Step 4: Add derived activity state and markup**

```ts
const activityState = computed(() => {
  const conversation = props.conversation
  if (!conversation) return undefined
  if (conversation.pendingInteraction) return { label: 'Waiting for your response', animated: false }
  if (conversation.registry.state === 'running') return { label: 'Rebuilding component registry…', animated: true }
  if (conversation.status === 'interrupting') return { label: 'Stopping the current turn…', animated: true }
  if (conversation.status === 'active') return { label: 'Codex is working…', animated: true }
  return undefined
})
```

Render the transcript when items or activity exist. Add `data-item` to each existing row, show `Summary not available` for blank reasoning, and append:

```vue
<li v-if="activityState" class="agent-activity" data-agent-activity :data-animated="String(activityState.animated)" aria-live="polite">
  <span class="agent-activity__signal" aria-hidden="true"><i /><i /><i /></span>
  <span>{{ activityState.label }}</span>
</li>
```

- [x] **Step 5: Add restrained motion**

```css
.agent-activity { display: flex; align-items: center; gap: .65rem; min-height: 2.4rem; padding: .62rem .8rem; border: 1px dashed rgba(23,126,122,.45); color: #52605a; font: 700 .6rem/1.3 var(--mono); letter-spacing: .04em; text-transform: uppercase; }
.agent-activity__signal { display: inline-flex; gap: .2rem; }
.agent-activity__signal i { width: .32rem; height: .32rem; background: var(--cyan-deep); opacity: .35; }
.agent-activity[data-animated="true"] .agent-activity__signal i { animation: agent-activity-pulse 1.05s ease-in-out infinite; }
.agent-activity[data-animated="true"] .agent-activity__signal i:nth-child(2) { animation-delay: .14s; }
.agent-activity[data-animated="true"] .agent-activity__signal i:nth-child(3) { animation-delay: .28s; }
@keyframes agent-activity-pulse { 0%, 65%, 100% { opacity: .28; transform: translateY(0); } 30% { opacity: 1; transform: translateY(-.16rem); } }
@media (prefers-reduced-motion: reduce) { .agent-activity__signal i { animation: none !important; transform: none !important; opacity: .65; } }
```

- [x] **Step 6: Verify GREEN**

```sh
cd web
bun run test:run -- tests/component-generator.test.ts
bun run typecheck
bun run build
```

Expected: PASS; existing dependency annotation warnings may remain, but build must exit 0.

### Task 3: Full Verification and Handoff

**Files:** Verify all modified files; create no additional production files.

**Interfaces:** Consumes Tasks 1-2 and produces a verified uncommitted implementation.

- [x] **Step 1: Run backend verification**

```sh
go test -race ./... -count=1
go vet ./...
```

- [x] **Step 2: Run frontend verification**

```sh
cd web
bun run test:run
bun run typecheck
bun run build
bun run e2e:agent
```

- [x] **Step 3: Audit disclosure and worktree state**

```sh
rg -n 'aggregatedOutput|processId|cwd|command' web/types/api.ts web/components/sites/ComponentGenerator.vue
git diff --check
git status --short --branch
```

Expected: no privileged command fields in the web DTO/component, no whitespace errors, and only intended uncommitted files alongside existing CouchDB/recovery changes.

- [x] **Step 4: Hand off without committing**

Report behavior, verification evidence, and exact modified files. Do not stage or commit.
