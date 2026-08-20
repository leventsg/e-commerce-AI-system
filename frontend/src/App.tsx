import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '@/contexts'
import { useAgent } from '@/hooks'
import { TopBar, Sidebar } from '@/components/layout'
import { AgentChatWindow, AgentChatInput } from '@/components/agent'
import { LoginPage } from '@/pages'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth()
  const location = useLocation()

  useEffect(() => {
    const handler = () => { /* auto-logout handled by context */ }
    window.addEventListener('auth-unauthorized', handler)
    return () => window.removeEventListener('auth-unauthorized', handler)
  }, [])

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />
  }
  return <>{children}</>
}

function AgentConsole() {
  const {
    activeId, messages, sessions, isStreaming, sendMessage, confirmAction,
    selectSession, newSession, stopGeneration,
    hasMoreSessions, sessionsLoading, loadMoreSessions,
    hasOlderMessages, loadingHistory, loadOlderMessages,
  } = useAgent()
  const [sidebarOpen, setSidebarOpen] = useState(false)

  const handleSend = useCallback((content: string) => {
    sendMessage(content)
  }, [sendMessage])

  const handleDelete = useCallback((_id: string) => {
    // Simple delete — just filter out
  }, [])

  const handleRename = useCallback((_id: string, _title: string) => {
    // Simple rename
  }, [])

  return (
    <div className="h-screen flex flex-col bg-gray-950 overflow-hidden">
      <TopBar />
      <div className="flex-1 flex overflow-hidden">
        <Sidebar
          isOpen={sidebarOpen}
          toggleSidebar={() => setSidebarOpen(o => !o)}
          conversations={sessions}
          activeId={activeId}
          onSelect={selectSession}
          onNew={newSession}
          onDelete={handleDelete}
          onRename={handleRename}
          hasMore={hasMoreSessions}
          loadingMore={sessionsLoading}
          onLoadMore={loadMoreSessions}
        />
        <div className="flex-1 min-w-0 flex flex-col">
          <button
            className="lg:hidden absolute top-14 left-2 z-20 p-1.5 rounded-lg bg-gray-900 border border-gray-800 text-gray-400"
            onClick={() => setSidebarOpen(true)}
          >
            ☰
          </button>
          <AgentChatWindow
            key={activeId}
            messages={messages}
            isStreaming={isStreaming}
            onSuggestionClick={handleSend}
            onConfirmAction={confirmAction}
            hasOlderMessages={hasOlderMessages}
            loadingHistory={loadingHistory}
            onLoadOlder={() => loadOlderMessages(activeId)}
          />
          <AgentChatInput
            onSend={handleSend}
            onCancel={stopGeneration}
            isStreaming={isStreaming}
          />
        </div>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/*" element={
        <RequireAuth>
          <AgentConsole />
        </RequireAuth>
      } />
    </Routes>
  )
}
