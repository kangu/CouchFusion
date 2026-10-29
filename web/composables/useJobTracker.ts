import { ref } from 'vue'
import type { JobSnapshot } from '../types/api'

const TERMINAL = new Set(['succeeded', 'failed', 'cancelled'])

export function isTerminalJob(job: Pick<JobSnapshot, 'status'>) {
  return TERMINAL.has(job.status)
}

interface JobApi {
  get<T>(path: string): Promise<T>
}

/**
 * Keeps the latest snapshot of jobs started from the dev page. Site SSE `job` events feed
 * `update()`; a 2 s poll covers a dropped stream. `wait()` resolves on a terminal snapshot.
 */
export function createJobTracker(api: JobApi, options: {
  schedule?: (callback: () => void, delay: number) => unknown
  cancelSchedule?: (handle: unknown) => void
  pollInterval?: number
} = {}) {
  const schedule = options.schedule ?? ((callback, delay) => setTimeout(callback, delay))
  const cancelSchedule = options.cancelSchedule ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  const pollInterval = options.pollInterval ?? 2000
  const jobs = ref<Record<string, JobSnapshot>>({})
  const waiters = new Map<string, Array<(job: JobSnapshot) => void>>()
  const polls = new Map<string, unknown>()

  function update(job: JobSnapshot) {
    const previous = jobs.value[job.id]
    if (previous && isTerminalJob(previous) && !isTerminalJob(job)) return
    jobs.value = { ...jobs.value, [job.id]: job }
    if (isTerminalJob(job)) {
      const poll = polls.get(job.id)
      if (poll !== undefined) cancelSchedule(poll)
      polls.delete(job.id)
      for (const resolve of waiters.get(job.id) ?? []) resolve(job)
      waiters.delete(job.id)
    }
  }

  function poll(id: string) {
    polls.set(id, schedule(async () => {
      polls.delete(id)
      try {
        update(await api.get<JobSnapshot>(`/jobs/${encodeURIComponent(id)}`))
      } catch { /* keep polling */ }
      const current = jobs.value[id]
      if (waiters.has(id) && (!current || !isTerminalJob(current))) poll(id)
    }, pollInterval))
  }

  function track(job: JobSnapshot) {
    update(job)
    return wait(job.id)
  }

  function wait(id: string): Promise<JobSnapshot> {
    const current = jobs.value[id]
    if (current && isTerminalJob(current)) return Promise.resolve(current)
    return new Promise((resolve) => {
      waiters.set(id, [...(waiters.get(id) ?? []), resolve])
      if (!polls.has(id)) poll(id)
    })
  }

  function stop() {
    for (const handle of polls.values()) cancelSchedule(handle)
    polls.clear()
  }

  return { jobs, update, track, wait, stop }
}
