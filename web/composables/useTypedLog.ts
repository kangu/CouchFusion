import { computed, onBeforeUnmount, reactive, ref, watch, type Ref } from 'vue'

/** One row of a run log. `key` must be unique across runs (prefix it with the job id). */
export interface LogEntry {
  key: string
  code: string
  text: string
  className?: string
}

export interface ShownEntry extends LogEntry {
  /** The part of `text` revealed so far. */
  shown: string
  typing: boolean
}

/**
 * Characters revealed per animation frame: at least `min` (fast, ~360/s at 60 fps), more when
 * a backlog builds up so it clears within about `catchUpFrames` frames.
 */
export function charsPerFrame(backlog: number, min = 6, catchUpFrames = 45) {
  return Math.max(min, Math.ceil(backlog / catchUpFrames))
}

interface Scheduler {
  request: (callback: () => void) => number
  cancel: (id: number) => void
}

const frameScheduler: Scheduler = {
  request: (callback) => requestAnimationFrame(() => callback()),
  cancel: (id) => cancelAnimationFrame(id),
}

function prefersReducedMotion() {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * Types new log rows in one after another. Rows present when the log is first shown appear at
 * once; rows added later are typed, the label first and then the text, and rows still waiting
 * are not shown yet. With reduced motion every row appears at once.
 */
export function useTypedLog(entries: Ref<LogEntry[]>, options: { instant?: () => boolean; scheduler?: Scheduler } = {}) {
  const instant = options.instant ?? prefersReducedMotion
  const scheduler = options.scheduler ?? frameScheduler
  const done = reactive(new Set<string>())
  const typingKey = ref<string | null>(null)
  const typed = ref(0)
  let frame: number | null = null

  for (const entry of entries.value) done.add(entry.key)

  const pending = () => entries.value.filter((entry) => !done.has(entry.key))

  function stop() {
    if (frame !== null) scheduler.cancel(frame)
    frame = null
  }

  function tick() {
    frame = null
    const waiting = pending()
    if (!waiting.length) {
      typingKey.value = null
      return
    }
    let current = waiting.find((entry) => entry.key === typingKey.value)
    if (!current) {
      current = waiting[0]!
      typingKey.value = current.key
      typed.value = 0
    }
    const backlog = waiting.reduce((sum, entry) => sum + entry.text.length, 0) - typed.value
    typed.value += charsPerFrame(backlog)
    if (typed.value >= current.text.length) {
      done.add(current.key)
      typingKey.value = null
      typed.value = 0
    }
    if (pending().length) frame = scheduler.request(tick)
  }

  function start() {
    if (instant()) {
      for (const entry of entries.value) done.add(entry.key)
      typingKey.value = null
      return
    }
    if (frame === null && pending().length) frame = scheduler.request(tick)
  }

  watch(() => entries.value.map((entry) => entry.key).join('\n'), start)
  onBeforeUnmount(stop)

  const shown = computed<ShownEntry[]>(() => {
    const rows: ShownEntry[] = []
    for (const entry of entries.value) {
      if (done.has(entry.key)) {
        rows.push({ ...entry, shown: entry.text, typing: false })
      } else if (entry.key === typingKey.value) {
        rows.push({ ...entry, shown: entry.text.slice(0, typed.value), typing: true })
        break
      } else {
        break
      }
    }
    return rows
  })

  /** Shows every row in full at once. */
  function finish() {
    stop()
    for (const entry of entries.value) done.add(entry.key)
    typingKey.value = null
    typed.value = 0
  }

  return { shown, finish }
}

/**
 * Keeps a scroll area at its newest content while the user is at the bottom. Scrolling up stops
 * following; scrolling back to the bottom (or `jumpToLatest`) follows again.
 */
export function useStickToBottom(element: Ref<HTMLElement | null | undefined>, threshold = 24) {
  const following = ref(true)
  let lastTop = 0

  function distanceFromBottom(el: HTMLElement) {
    return el.scrollHeight - el.scrollTop - el.clientHeight
  }

  function onScroll() {
    const el = element.value
    if (!el) return
    if (distanceFromBottom(el) <= threshold) following.value = true
    else if (el.scrollTop < lastTop - 1) following.value = false
    lastTop = el.scrollTop
  }

  /** Call after the content changed: scrolls to the newest row while following. */
  function follow() {
    const el = element.value
    if (!el || !following.value) return
    el.scrollTop = el.scrollHeight
    lastTop = el.scrollTop
  }

  function jumpToLatest() {
    following.value = true
    follow()
  }

  return { following, onScroll, follow, jumpToLatest }
}
