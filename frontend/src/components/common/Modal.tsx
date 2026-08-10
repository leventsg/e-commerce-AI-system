import { useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useEffect } from 'react'

interface ModalProps {
  open: boolean; onClose: () => void; title?: string
  children: ReactNode; maxWidth?: 'sm' | 'md' | 'lg' | 'xl'
}

const widths: Record<string, string> = { sm: 'max-w-sm', md: 'max-w-md', lg: 'max-w-lg', xl: 'max-w-xl' }

export function Modal({ open, onClose, title, children, maxWidth = 'lg' }: ModalProps) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
    return () => { document.removeEventListener('keydown', onKey); document.body.style.overflow = '' }
  }, [open, onClose])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center px-4 py-8">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div className={`relative w-full ${widths[maxWidth]} bg-gray-900 rounded-2xl shadow-2xl border border-gray-800 overflow-hidden`}>
        {(title) && (
          <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
            <h3 className="text-xl font-semibold text-gray-100">{title}</h3>
            <button onClick={onClose} className="p-1 rounded-lg hover:bg-gray-800 text-gray-400 hover:text-gray-200"><X className="w-5 h-5" /></button>
          </div>
        )}
        <div className="px-6 py-4 overflow-y-auto">{children}</div>
      </div>
    </div>,
    document.body,
  )
}
