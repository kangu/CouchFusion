import type { JobSnapshot, ProcessSnapshot } from '../types/api'

export function useActivityStore() {
  const jobs = useState<JobSnapshot[]>('activity-jobs', () => [])
  const processes = useState<ProcessSnapshot[]>('activity-processes', () => [])

  function trackJob(job: JobSnapshot) {
    jobs.value = upsert(jobs.value, job)
  }

  function trackProcess(process: ProcessSnapshot) {
    processes.value = upsert(processes.value, process)
  }

  return { jobs, processes, trackJob, trackProcess }
}

function upsert<T extends { id: string }>(items: T[], item: T) {
  return [item, ...items.filter((candidate) => candidate.id !== item.id)]
}
