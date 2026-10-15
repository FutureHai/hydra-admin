import { afterEach, expect, it } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import MockAdapter from 'axios-mock-adapter'
import { AxiosHeaders } from 'axios'
import { AuthProvider, useAuth } from './AuthProvider'
import { api, clearAuthMemory, onUnauthorized } from './api'

const mock = new MockAdapter(api)
afterEach(() => { mock.reset(); clearAuthMemory(); onUnauthorized(null) })

function Controls() {
  const { loading, session, login, logout } = useAuth()
  const location = useLocation()
  return <div>
    <span data-testid="path">{location.pathname}</span>
    <span data-testid="session">{loading ? 'loading' : session?.username ?? 'anonymous'}</span>
    <button onClick={() => void login('admin', 'test-only-password')}>login</button>
    <button onClick={() => void logout()}>logout</button>
  </div>
}

it('logs in with a pre-auth nonce, rotates CSRF state, and logs out', async () => {
  let authenticated = false
  let csrfCount = 0
  mock.onGet('/auth/session').reply(() => authenticated ? [200, { username: 'admin', expires_at: '2099-01-01' }] : [401])
  mock.onGet('/auth/csrf').reply(() => [200, { csrf_token: ++csrfCount === 1 ? 'preauth-csrf' : 'session-csrf' }])
  mock.onPost('/auth/login').reply(config => {
    expect((config.headers as AxiosHeaders).get('X-CSRF-Token')).toBe('preauth-csrf')
    authenticated = true
    return [200]
  })
  mock.onPost('/auth/logout').reply(config => {
    expect((config.headers as AxiosHeaders).get('X-CSRF-Token')).toBe('session-csrf')
    authenticated = false
    return [204]
  })
  render(<MemoryRouter initialEntries={['/login']}><AuthProvider><Routes><Route path="*" element={<Controls />} /></Routes></AuthProvider></MemoryRouter>)
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('anonymous'))
  fireEvent.click(screen.getByText('login'))
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('admin'))
  expect(screen.getByTestId('path')).toHaveTextContent('/')
  fireEvent.click(screen.getByText('logout'))
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('anonymous'))
  await waitFor(() => expect(screen.getByTestId('path')).toHaveTextContent('/login'))
  expect(localStorage.length).toBe(0)
  expect(sessionStorage.length).toBe(0)
})

it('redirects after a protected request receives 401', async () => {
  mock.onGet('/auth/session').reply(200, { username: 'admin', expires_at: '2099-01-01' })
  mock.onGet('/auth/csrf').reply(200, { csrf_token: 'session-csrf' })
  mock.onGet('/clients').reply(401)
  render(<MemoryRouter initialEntries={['/clients']}><AuthProvider><Routes><Route path="*" element={<Controls />} /></Routes></AuthProvider></MemoryRouter>)
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('admin'))
  await expect(api.get('/clients')).rejects.toMatchObject({ response: { status: 401 } })
  await waitFor(() => expect(screen.getByTestId('path')).toHaveTextContent('/login'))
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('anonymous'))
})
