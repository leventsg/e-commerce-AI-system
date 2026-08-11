import type { UIMessage } from '@/types'

export type ChatRenderItem =
  | { kind: 'user'; message: UIMessage }
  | {
      kind: 'assistant-group'
      id: string
      messages: UIMessage[]
      thinkingText: string
      toolMessages: UIMessage[]
      confirmationMessages: UIMessage[]
      assistantMessages: UIMessage[]
      errorMessages: UIMessage[]
      isStreaming: boolean
    }

const ASSISTANT_SIDE_TYPES = new Set<UIMessage['type']>(['assistant', 'thinking', 'tool-result', 'confirmation', 'error', 'system'])

function sortByTimestamp(messages: UIMessage[]) {
  return [...messages].sort((a, b) => a.timestamp - b.timestamp)
}

function latestToolMessages(messages: UIMessage[]) {
  const byToolCall = new Map<string, UIMessage>()
  const noCallId: UIMessage[] = []

  for (const message of sortByTimestamp(messages.filter(item => item.type === 'tool-result'))) {
    if (!message.toolCallId) {
      noCallId.push(message)
      continue
    }
    byToolCall.set(message.toolCallId, message)
  }

  return sortByTimestamp([...noCallId, ...byToolCall.values()])
}

function toAssistantGroup(messages: UIMessage[], index: number): ChatRenderItem {
  const sorted = sortByTimestamp(messages)
  const thinkingMessages = sorted.filter(message => message.type === 'thinking')
  const toolMessages = latestToolMessages(sorted)
  const confirmationMessages = sorted.filter(message => message.type === 'confirmation')
  const assistantMessages = sorted.filter(message => message.type === 'assistant')
  const errorMessages = sorted.filter(message => message.type === 'error')
  const first = sorted[0]

  return {
    kind: 'assistant-group',
    id: `assistant_group_${first?.id || index}`,
    messages: sorted,
    thinkingText: thinkingMessages.map(message => message.content.trim()).filter(Boolean).join('\n'),
    toolMessages,
    confirmationMessages,
    assistantMessages,
    errorMessages,
    isStreaming: sorted.some(message => message.streaming) || toolMessages.some(message => message.toolStatus === 'running'),
  }
}

export function buildChatRenderItems(messages: UIMessage[]): ChatRenderItem[] {
  const items: ChatRenderItem[] = []
  let group: UIMessage[] = []

  const flushGroup = () => {
    if (group.length === 0) return
    items.push(toAssistantGroup(group, items.length))
    group = []
  }

  for (const message of messages) {
    if (message.type === 'user') {
      flushGroup()
      items.push({ kind: 'user', message })
      continue
    }

    if (ASSISTANT_SIDE_TYPES.has(message.type)) {
      group.push(message)
    }
  }

  flushGroup()
  return items
}
