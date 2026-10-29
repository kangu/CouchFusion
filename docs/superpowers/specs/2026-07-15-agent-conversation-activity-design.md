# Agent Conversation Activity Design

Date: 2026-07-15
Status: Approved design, pending written-spec review

## Goal

Make active component-generation conversations informative without exposing raw shell commands or raw model reasoning. Reasoning summaries must contain the summary supplied by Codex, command rows must state the safe category of work being performed, and the transcript must end with a clear activity indicator while work is ongoing.

## Backend Event Normalization

`internal/agent/conversations` remains the security boundary for conversation items.

- Reasoning items expose only Codex reasoning-summary text. Raw reasoning content remains discarded.
- Streamed `item/reasoning/summaryTextDelta` text is retained when a later completed reasoning item has an empty summary instead of replacing the accumulated text with an empty string.
- Command-execution items are mapped from Codex `commandActions` to a bounded, non-sensitive activity label. The browser never receives the raw command, command output, absolute working directory, process identifier, or unparsed arguments.
- The schema's known action types map to concise labels: `read` becomes `Reading project files`, `listFiles` becomes `Listing project files`, and `search` becomes `Searching project files`. Unknown or empty actions use the generic fallback `Running a project command`; CouchFusion does not inspect raw shell text to guess a more specific category.
- Item status continues to come from the app-server item and is rendered separately from the safe label.

Completed items and live item events use the same normalization so restored conversations and live conversations render consistently.

## Transcript Presentation

The existing compact transcript remains visually consistent with the CouchFusion UI.

- Reasoning summaries remain collapsed by default. Expansion shows the preserved summary. An empty summary is not rendered as an empty content area; it shows a muted `Summary not available` fallback.
- Command rows show the safe backend-generated activity label and the existing status. They do not expand into raw command or output details.
- The activity row is the final list item in the transcript, including when no other items exist yet.
- During `active`, `interrupting`, or registry `running` states, the row uses a restrained animated cyan signal and an appropriate label such as `Codex is working…`, `Stopping the current turn…`, or `Rebuilding component registry…`.
- While a question, approval, or consent interaction is pending, the animation pauses and the row reads `Waiting for your response`.
- Completed, ready, failed, and interrupted conversations do not show the activity row.
- The activity row uses polite live-region semantics. Animation is disabled under `prefers-reduced-motion` while the text remains visible.

## Data Flow

1. Codex emits item started, delta, and completed notifications.
2. The conversation manager normalizes reasoning and command items into bounded public text.
3. Existing conversation snapshot and SSE event DTOs carry the normalized item without adding privileged fields.
4. Vue renders the normalized label and derives the final activity row from conversation, registry, and interaction state.

No persistence-schema migration is required because the public conversation item continues to use its existing `text`, `type`, and `status` fields.

## Error and Edge Cases

- A completed reasoning item with an empty summary preserves a non-empty streamed summary already held for the same item.
- A reasoning item that never receives summary text renders the explicit unavailable fallback.
- Multiple command actions collapse to one bounded category label; no raw command fragments are concatenated.
- An unknown command action never leaks its serialized payload.
- A pending interaction takes precedence over active animation.
- Registry rebuilding takes precedence after the Codex turn has completed.

## Verification

- Go tests cover reasoning-delta preservation, completed summary replacement when non-empty, safe command-action mapping, unknown-action fallback, and absence of raw command/output/path content.
- Vue component tests cover populated reasoning expansion, the unavailable fallback, safe command text, animated active/rebuilding rows, static waiting state, hidden terminal-state progress, and live-region semantics.
- Existing agent lifecycle, API safety, SSE, typecheck, build, and browser acceptance suites remain green.
