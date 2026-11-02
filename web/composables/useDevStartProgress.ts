import type { ActivityEvent, DevSessionView } from '../types/api'

/** No dev output for this long while starting (after the grace period) counts as "may be stuck". */
export const QUIET_WARN_SECS = 45
/** First compile is often silent for a while; do not warn before this. */
export const START_GRACE_SECS = 60
const DEFAULT_TIMEOUT_SECS = 180

export interface StartProgress {
  elapsedSecs: number
  timeoutSecs: number
  /** 0–1 share of the timeout used. */
  fraction: number
  /** Seconds since the last dev log line (or since the start when there is none). */
  quietSecs: number
  /** Last dev log lines since this start, oldest first. */
  lines: string[]
  stalled: boolean
}

function ms(value: string | undefined): number | undefined {
  if (!value) return undefined
  const t = Date.parse(value)
  return Number.isNaN(t) ? undefined : t
}

/**
 * Progress of a dev start from what the page already has: the session view (`startedAt`,
 * `readyTimeoutSecs`) and the dev log lines from the site stream. Pure, so it is testable.
 */
export function startProgress(view: DevSessionView | undefined, logs: ActivityEvent[], now: number, maxLines = 6): StartProgress {
  const timeoutSecs = view?.readyTimeoutSecs ?? DEFAULT_TIMEOUT_SECS
  const started = ms(view?.startedAt) ?? now
  const elapsedSecs = Math.max(0, Math.floor((now - started) / 1000))
  const recent = logs.filter((line) => (ms(line.time) ?? 0) >= started - 1000 && line.text.trim())
  const last = recent.length ? ms(recent[recent.length - 1]!.time) ?? started : started
  const quietSecs = Math.max(0, Math.floor((now - last) / 1000))
  return {
    elapsedSecs,
    timeoutSecs,
    fraction: Math.min(1, elapsedSecs / Math.max(1, timeoutSecs)),
    quietSecs,
    lines: recent.slice(-maxLines).map((line) => line.text.trim().slice(0, 300)),
    stalled: elapsedSecs >= START_GRACE_SECS && quietSecs >= QUIET_WARN_SECS,
  }
}

/** `1:05`, `42 s`. */
export function formatSecs(secs: number): string {
  if (secs < 60) return `${secs} s`
  const m = Math.floor(secs / 60)
  const s = secs % 60
  return `${m}:${String(s).padStart(2, '0')}`
}
