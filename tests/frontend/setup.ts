import { afterEach, vi } from 'vitest'

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  document.cookie = 'Refresh-Token=; Max-Age=0; path=/'
})
