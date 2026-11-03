import { describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick, ref, type Ref } from 'vue'
import { mount } from '@vue/test-utils'
import { charsPerFrame, useStickToBottom, useTypedLog, type LogEntry } from '../composables/useTypedLog'
import DevEventLog from '../components/dev/DevEventLog.vue'

/** Frames run only when the test calls `run()`. */
function manualFrames() {
  let queue: Array<() => void> = []
  return {
    scheduler: {
      request: (callback: () => void) => { queue.push(callback); return queue.length },
      cancel: () => { queue = [] },
    },
    run(count = 1) {
      for (let i = 0; i < count; i++) {
        const callbacks = queue
        queue = []
        callbacks.forEach((callback) => callback())
      }
    },
    get pending() { return queue.length },
  }
}

function entry(key: string, text: string, code = 'codex.message'): LogEntry {
  return { key, code, text }
}

function typedLog(entries: Ref<LogEntry[]>, frames: ReturnType<typeof manualFrames>, instant = false) {
  let api!: ReturnType<typeof useTypedLog>
  const wrapper = mount(defineComponent({
    setup() {
      api = useTypedLog(entries, { scheduler: frames.scheduler, instant: () => instant })
      return () => h('div')
    },
  }))
  return { ...api, wrapper }
}

const texts = (rows: Array<{ shown: string }>) => rows.map((row) => row.shown)

describe('typing speed', () => {
  it('reveals at least six characters a frame and speeds up for a backlog', () => {
    expect(charsPerFrame(0)).toBe(6)
    expect(charsPerFrame(100)).toBe(6)
    expect(charsPerFrame(900)).toBe(20)
  })
})

describe('typed log', () => {
  it('shows rows that were there first at once', () => {
    const frames = manualFrames()
    const { shown } = typedLog(ref([entry('j:1', 'first'), entry('j:2', 'second')]), frames)
    expect(texts(shown.value)).toEqual(['first', 'second'])
    expect(frames.pending).toBe(0)
  })

  it('types new rows one after another', async () => {
    const frames = manualFrames()
    const entries = ref([entry('j:1', 'ready')])
    const { shown } = typedLog(entries, frames)
    entries.value = [...entries.value, entry('j:2', 'abcdefghij'), entry('j:3', 'xyz')]
    await nextTick()

    expect(texts(shown.value)).toEqual(['ready'])
    frames.run()
    expect(texts(shown.value)).toEqual(['ready', 'abcdef'])
    expect(shown.value[1]!.typing).toBe(true)
    frames.run()
    expect(texts(shown.value)).toEqual(['ready', 'abcdefghij'])
    expect(shown.value[1]!.typing).toBe(false)
    frames.run()
    expect(texts(shown.value)).toEqual(['ready', 'abcdefghij', 'xyz'])
    expect(frames.pending).toBe(0)
  })

  it('starts typing again for a new run with the same event ids', async () => {
    const frames = manualFrames()
    const entries = ref([entry('a:1', 'old run')])
    const { shown } = typedLog(entries, frames)
    entries.value = [entry('b:1', 'new run')]
    await nextTick()
    expect(texts(shown.value)).toEqual([])
    frames.run(2)
    expect(texts(shown.value)).toEqual(['new run'])
  })

  it('shows everything at once with reduced motion', async () => {
    const frames = manualFrames()
    const entries = ref<LogEntry[]>([])
    const { shown } = typedLog(entries, frames, true)
    entries.value = [entry('j:1', 'one'), entry('j:2', 'two')]
    await nextTick()
    expect(texts(shown.value)).toEqual(['one', 'two'])
    expect(frames.pending).toBe(0)
  })

  it('stops scheduling frames when unmounted', async () => {
    const frames = manualFrames()
    const entries = ref<LogEntry[]>([])
    const { wrapper } = typedLog(entries, frames)
    entries.value = [entry('j:1', 'x'.repeat(50))]
    await nextTick()
    expect(frames.pending).toBe(1)
    wrapper.unmount()
    expect(frames.pending).toBe(0)
  })
})

/** A scroll area with settable sizes (happy-dom does no layout). */
function scrollBox(scrollHeight: number, clientHeight = 100) {
  const el = document.createElement('div')
  let top = 0
  let height = scrollHeight
  Object.defineProperty(el, 'clientHeight', { get: () => clientHeight })
  Object.defineProperty(el, 'scrollHeight', { get: () => height })
  Object.defineProperty(el, 'scrollTop', { get: () => top, set: (v: number) => { top = Math.max(0, Math.min(v, height - clientHeight)) } })
  return { el, grow: (by: number) => { height += by } }
}

describe('stick to bottom', () => {
  it('follows new content while at the bottom', () => {
    const box = scrollBox(300)
    const { follow, following } = useStickToBottom(ref(box.el))
    follow()
    expect(box.el.scrollTop).toBe(200)
    box.grow(50)
    follow()
    expect(box.el.scrollTop).toBe(250)
    expect(following.value).toBe(true)
  })

  it('stops following when scrolled up and resumes at the bottom', () => {
    const box = scrollBox(300)
    const { follow, following, onScroll } = useStickToBottom(ref(box.el))
    follow()
    box.el.scrollTop = 80
    onScroll()
    expect(following.value).toBe(false)

    box.grow(50)
    follow()
    expect(box.el.scrollTop).toBe(80)

    box.el.scrollTop = 250
    onScroll()
    expect(following.value).toBe(true)
    box.grow(50)
    follow()
    expect(box.el.scrollTop).toBe(300)
  })

  it('does not stop following when content grows under it', () => {
    const box = scrollBox(300)
    const { follow, following, onScroll } = useStickToBottom(ref(box.el))
    follow()
    box.grow(200)
    onScroll()
    expect(following.value).toBe(true)
  })

  it('jumps back to the newest row', () => {
    const box = scrollBox(300)
    const { follow, following, onScroll, jumpToLatest } = useStickToBottom(ref(box.el))
    follow()
    box.el.scrollTop = 10
    onScroll()
    jumpToLatest()
    expect(following.value).toBe(true)
    expect(box.el.scrollTop).toBe(200)
  })
})

describe('DevEventLog', () => {
  it('renders labels, row classes and the jump button only when not following', async () => {
    const wrapper = mount(DevEventLog, { props: { entries: [{ key: 'j:1', code: 'codex.done', text: 'Codex finished', className: 'dev-events__row--warn' }] } })
    const row = wrapper.get('[data-testid="event-log"] li')
    expect(row.text()).toBe('codex.doneCodex finished')
    expect(row.classes()).toContain('dev-events__row--warn')
    expect(wrapper.find('[data-testid="event-log-latest"]').exists()).toBe(false)
  })
})
