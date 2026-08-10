import { useState, useRef, useEffect, type KeyboardEvent } from 'react'
import { Plus, MessageSquare, Search, MoreHorizontal, Trash2, Pencil, Check, X, Bot } from 'lucide-react'
import { useAuth } from '@/contexts'
import type { ConversationSummary } from '@/types'

interface SidebarProps {
  isOpen: boolean; toggleSidebar: () => void
  conversations: ConversationSummary[]
  activeId: string | null
  onSelect: (id: string) => void
  onNew: () => void
  onDelete?: (id: string) => void
  onRename?: (id: string, title: string) => void
}

export function Sidebar({ isOpen, toggleSidebar, conversations, activeId, onSelect, onNew, onDelete, onRename }: SidebarProps) {
  const { username, logout } = useAuth()
  const [search, setSearch] = useState('')
  const [menuId, setMenuId] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editTitle, setEditTitle] = useState('')
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handler = (e: MouseEvent) => { if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuId(null) }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const filtered = search ? conversations.filter(c => c.title.toLowerCase().includes(search.toLowerCase())) : conversations

  const startRename = (c: ConversationSummary) => { setEditingId(c.id); setEditTitle(c.title) }
  const submitRename = (id: string) => {
    if (editTitle.trim()) onRename?.(id, editTitle.trim())
    setEditingId(null)
  }
  const handleKeyDown = (e: KeyboardEvent, id: string) => {
    if (e.key === 'Enter') submitRename(id)
    if (e.key === 'Escape') setEditingId(null)
  }

  return (
    <>
      {/* Mobile backdrop */}
      {isOpen && <div className="fixed inset-0 z-30 bg-black/50 backdrop-blur-sm lg:hidden" onClick={toggleSidebar} />}

      <div className={`
        fixed lg:static inset-y-0 left-0 z-40 w-64 flex flex-col bg-gray-950 border-r border-gray-800
        transform transition-transform duration-300 ease-in-out
        ${isOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}
      `}>
        {/* Header */}
        <div className="px-4 py-3 border-b border-gray-800">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <Bot className="w-5 h-5 text-orange-500" />
              <span className="text-sm font-semibold text-gray-200">会话列表</span>
            </div>
            <button onClick={onNew} className="w-7 h-7 rounded-lg bg-gray-900 border border-gray-800 hover:border-orange-500/30 flex items-center justify-center transition-colors group">
              <Plus className="w-3.5 h-3.5 text-gray-500 group-hover:text-orange-400 transition-colors" />
            </button>
          </div>
          <div className="relative">
            <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-600" />
            <input
              value={search} onChange={e => setSearch(e.target.value)}
              placeholder="搜索会话..." className="w-full bg-gray-900 border border-gray-800 rounded-lg pl-8 pr-3 py-1.5 text-xs text-gray-300 placeholder-gray-600 outline-none focus:border-orange-500/30 transition-colors"
            />
          </div>
        </div>

        {/* List */}
        <div className="flex-1 overflow-y-auto">
          {filtered.length === 0 && (
            <div className="flex flex-col items-center justify-center py-12 px-4 text-center">
              <MessageSquare className="w-8 h-8 text-gray-700 mb-3" />
              <p className="text-xs text-gray-600">暂无会话</p>
            </div>
          )}
          {filtered.map(c => (
            <div key={c.id}
              onClick={() => onSelect(c.id)}
              className={`group relative mx-2 my-0.5 rounded-xl cursor-pointer transition-all duration-150 ${
                c.id === activeId ? 'bg-gray-900 border border-orange-500/15 shadow-lg shadow-orange-500/5' : 'hover:bg-gray-900/50 border border-transparent'
              }`}
            >
              <div className="px-3 py-2.5">
                <div className="flex items-center justify-between gap-2">
                  {editingId === c.id ? (
                    <input
                      value={editTitle}
                      onChange={e => setEditTitle(e.target.value)}
                      onBlur={() => submitRename(c.id)}
                      onKeyDown={e => handleKeyDown(e, c.id)}
                      className="flex-1 bg-gray-800 rounded px-1.5 py-0.5 text-xs text-gray-200 outline-none"
                      onClick={e => e.stopPropagation()}
                      autoFocus
                    />
                  ) : (
                    <span className="text-sm text-gray-300 truncate flex-1">{c.title}</span>
                  )}
                  <button
                    onClick={e => { e.stopPropagation(); setMenuId(menuId === c.id ? null : c.id) }}
                    className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-gray-800 transition-opacity"
                  >
                    <MoreHorizontal className="w-3 h-3 text-gray-600" />
                  </button>
                </div>
                {c.last_message_preview && (
                  <p className="text-[11px] text-gray-600 truncate mt-1 ml-0">{c.last_message_preview}</p>
                )}
              </div>
              {/* Dropdown menu */}
              {menuId === c.id && (
                <div ref={menuRef} className="absolute right-1 top-full mt-1 z-50 bg-gray-900 border border-gray-800 rounded-xl shadow-2xl py-1 min-w-[100px] animate-fade-in">
                  <button onClick={e => { e.stopPropagation(); startRename(c); setMenuId(null) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-gray-400 hover:text-gray-200 hover:bg-gray-800">
                    <Pencil className="w-3 h-3" /> 重命名
                  </button>
                  <button onClick={e => { e.stopPropagation(); onDelete?.(c.id); setMenuId(null) }} className="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-red-400 hover:bg-gray-800">
                    <Trash2 className="w-3 h-3" /> 删除
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>

        {/* Footer */}
        <div className="px-4 py-3 border-t border-gray-800">
          <div className="flex items-center justify-between">
            <span className="text-xs text-gray-500 truncate">{username || '未登录'}</span>
            <button onClick={logout} className="text-[10px] text-gray-600 hover:text-gray-400 transition-colors">退出</button>
          </div>
        </div>
      </div>
    </>
  )
}
