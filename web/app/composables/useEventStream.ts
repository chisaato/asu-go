export type ConnectionStatus = 'connected' | 'connecting' | 'disconnected'

export interface SseEvent {
  type: string
  data: unknown
  id?: string
}

export function useEventStream(url = '/admin/api/events') {
  const status = ref<ConnectionStatus>('disconnected')
  const lastError = ref<string | null>(null)
  const listeners = new Map<string, Set<(data: unknown) => void>>()

  let eventSource: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let retryDelay = 1000
  let isStopped = false

  function emit(type: string, data: unknown) {
    const handlers = listeners.get(type)
    if (handlers) {
      for (const fn of handlers) {
        try {
          fn(data)
        } catch {
          /* 忽略回调内异常 */
        }
      }
    }
  }

  function on<T = unknown>(type: string, handler: (data: T) => void) {
    if (!listeners.has(type)) {
      listeners.set(type, new Set())
    }
    listeners.get(type)!.add(handler as (data: unknown) => void)
    return () => {
      listeners.get(type)?.delete(handler as (data: unknown) => void)
    }
  }

  function connect() {
    if (isStopped || typeof window === 'undefined' || !('EventSource' in window)) return

    status.value = 'connecting'
    try {
      eventSource = new EventSource(url)

      eventSource.onopen = () => {
        status.value = 'connected'
        lastError.value = null
        retryDelay = 1000 // 重置重试时间
        emit('connect', { timestamp: Date.now() })
      }

      eventSource.onerror = () => {
        status.value = 'disconnected'
        lastError.value = '连接中断，等待重试'
        close()
        scheduleReconnect()
      }

      // 注册常见事件名监听
      const knownEvents = ['job.created', 'job.updated', 'job.finished', 'overview', 'stage', 'log', 'status', 'end']
      for (const eventName of knownEvents) {
        eventSource.addEventListener(eventName, (e: MessageEvent) => {
          let parsed: unknown = e.data
          try {
            parsed = JSON.parse(e.data)
          } catch {
            /* 保持字符串格式 */
          }
          emit(eventName, parsed)
        })
      }

      eventSource.onmessage = (e: MessageEvent) => {
        let parsed: unknown = e.data
        try {
          parsed = JSON.parse(e.data)
        } catch {
          /* 保持原始格式 */
        }
        emit('message', parsed)
      }
    } catch (e) {
      status.value = 'disconnected'
      lastError.value = e instanceof Error ? e.message : '连接建立失败'
      scheduleReconnect()
    }
  }

  function scheduleReconnect() {
    if (isStopped) return
    if (retryTimer) clearTimeout(retryTimer)
    retryTimer = setTimeout(() => {
      retryDelay = Math.min(retryDelay * 2, 30000) // 指数退避 1s -> 30s
      connect()
    }, retryDelay)
  }

  function close() {
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
  }

  function stop() {
    isStopped = true
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
    close()
    status.value = 'disconnected'
  }

  // 页面可见性管理：切后台断开或暂停，切回重新连接
  function handleVisibilityChange() {
    if (document.hidden) {
      close()
      status.value = 'disconnected'
    } else if (!isStopped) {
      retryDelay = 1000
      connect()
    }
  }

  onMounted(() => {
    connect()
    document.addEventListener('visibilitychange', handleVisibilityChange)
  })

  onUnmounted(() => {
    document.removeEventListener('visibilitychange', handleVisibilityChange)
    stop()
  })

  return {
    status,
    lastError,
    on,
    reconnect: () => {
      isStopped = false
      retryDelay = 1000
      close()
      connect()
    },
    stop
  }
}
