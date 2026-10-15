import axios from 'axios'

let csrfToken: string | null = null
let unauthorized: (() => void) | null = null

export function setCsrfToken(value: string | null) { csrfToken = value }
export function clearAuthMemory() { csrfToken = null }
export function onUnauthorized(callback: (() => void) | null) { unauthorized = callback }

export const api = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
  timeout: 15000,
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.request.use(config => {
  const method = (config.method ?? 'get').toUpperCase()
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && csrfToken) {
    config.headers.set('X-CSRF-Token', csrfToken)
  }
  return config
})

api.interceptors.response.use(response => response, error => {
  if (axios.isAxiosError(error) && error.response?.status === 401) {
    clearAuthMemory()
    unauthorized?.()
  }
  return Promise.reject(error)
})
