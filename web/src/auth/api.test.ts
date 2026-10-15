import { afterEach, describe, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import MockAdapter from 'axios-mock-adapter'
import { api, clearAuthMemory, onUnauthorized, setCsrfToken } from './api'

const mock = new MockAdapter(api)
afterEach(() => { mock.reset(); clearAuthMemory(); onUnauthorized(null) })

describe('same-origin API security', () => {
  it('sends credentials and the in-memory CSRF token only on writes', async () => {
    expect(api.defaults.withCredentials).toBe(true)
    setCsrfToken('test-only-csrf')
    mock.onGet('/auth/session').reply(config => [200, { hasCsrf: Boolean((config.headers as AxiosHeaders).get('X-CSRF-Token')) }])
    mock.onPost('/auth/logout').reply(config => [204, { csrf: (config.headers as AxiosHeaders).get('X-CSRF-Token') }])
    expect((await api.get('/auth/session')).data.hasCsrf).toBe(false)
    expect((await api.post('/auth/logout')).data.csrf).toBe('test-only-csrf')
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })

  it('clears the token and notifies the router on 401', async () => {
    const redirect = vi.fn()
    onUnauthorized(redirect)
    setCsrfToken('test-only-csrf')
    mock.onGet('/auth/session').reply(401)
    await expect(api.get('/auth/session')).rejects.toMatchObject({ response: { status: 401 } })
    expect(redirect).toHaveBeenCalledOnce()
    mock.onPost('/auth/logout').reply(config => [200, { csrf: (config.headers as AxiosHeaders).get('X-CSRF-Token') }])
    expect((await api.post('/auth/logout')).data.csrf).toBeUndefined()
  })

  it('never persists a no-store token response', async () => {
    mock.onPost('/tokens/introspect').reply(200, { active: true, client_id: 'test-client' }, { 'Cache-Control': 'no-store' })
    const response = await api.post('/tokens/introspect', { token: 'test-only-token' })
    expect((response.headers as AxiosHeaders).get('Cache-Control')).toBe('no-store')
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })
})
