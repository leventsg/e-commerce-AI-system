import { render, waitFor } from '@testing-library/react'
import { useEffect } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '../../../frontend/src/contexts'
import { apiGet } from '../../../frontend/src/services/api/client'
import { STORAGE_KEYS } from '../../../frontend/src/constants'

function RenewalProbe() {
  useEffect(() => {
    void apiGet('/douyin/user/info', 'old-access')
  }, [])
  return null
}

describe('AuthProvider token renewal', () => {
  it('persists renewed access and refresh tokens from api client renewal responses', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        code: 10004,
        msg: '令牌续期成功',
        data: { access_token: 'new-access', refresh_token: 'new-refresh' },
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        code: 0,
        msg: 'ok',
        data: { user_id: 1 },
      }), { status: 200 })))

    localStorage.setItem(STORAGE_KEYS.TOKEN, 'old-access')
    localStorage.setItem(STORAGE_KEYS.REFRESH_TOKEN, 'old-refresh')
    document.cookie = 'Refresh-Token=old-refresh; path=/'

    render(
      <AuthProvider>
        <RenewalProbe />
      </AuthProvider>,
    )

    await waitFor(() => {
      expect(localStorage.getItem(STORAGE_KEYS.TOKEN)).toBe('new-access')
      expect(localStorage.getItem(STORAGE_KEYS.REFRESH_TOKEN)).toBe('new-refresh')
      expect(document.cookie).toContain('Refresh-Token=new-refresh')
    })
  })
})
