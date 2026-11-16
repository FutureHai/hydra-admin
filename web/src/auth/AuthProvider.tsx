import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import axios from 'axios'
import { useNavigate } from 'react-router-dom'
import { api, clearAuthMemory, onUnauthorized, setCsrfToken } from './api'

type Session = { username: string; expires_at: string }
type AuthState = { loading: boolean; session: Session | null }
type AuthContextValue = AuthState & {
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  changePassword: (currentPassword: string, newPassword: string, confirmPassword: string) => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const navigateRef = useRef(navigate)
  useEffect(() => { navigateRef.current = navigate }, [navigate])
  const [state, setState] = useState<AuthState>({ loading: true, session: null })

  const loseSession = useCallback(() => {
    clearAuthMemory()
    setState({ loading: false, session: null })
    navigateRef.current('/login', { replace: true })
  }, [])

  useEffect(() => {
    onUnauthorized(loseSession)
    let active = true
    api.get<Session>('/auth/session').then(async response => {
      const csrf = await api.get<{ csrf_token: string }>('/auth/csrf')
      if (active) {
        setCsrfToken(csrf.data.csrf_token)
        setState({ loading: false, session: response.data })
      }
    }).catch(() => { if (active) setState({ loading: false, session: null }) })
    return () => { active = false; onUnauthorized(null) }
  }, [loseSession])

  const login = useCallback(async (username: string, password: string) => {
    const preAuth = await api.get<{ csrf_token: string }>('/auth/csrf')
    setCsrfToken(preAuth.data.csrf_token)
    try {
      await api.post('/auth/login', { username, password })
      setCsrfToken(null)
      const csrf = await api.get<{ csrf_token: string }>('/auth/csrf')
      setCsrfToken(csrf.data.csrf_token)
      const session = await api.get<Session>('/auth/session')
      setState({ loading: false, session: session.data })
      navigate('/', { replace: true })
    } catch (error) {
      clearAuthMemory()
      throw error
    }
  }, [navigate])

  const logout = useCallback(async () => {
    await api.post('/auth/logout')
    clearAuthMemory()
    setState({ loading: false, session: null })
    navigate('/login', { replace: true })
  }, [navigate])

  const changePassword = useCallback(async (currentPassword: string, newPassword: string, confirmPassword: string) => {
    let auditWarning = false
    try {
      await api.post('/auth/password', { current_password: currentPassword, new_password: newPassword, confirm_password: confirmPassword })
    } catch (error) {
      if (!axios.isAxiosError(error) || error.response?.data?.code !== 'password_changed_audit_failed') throw error
      auditWarning = true
    }
    clearAuthMemory()
    setState({ loading: false, session: null })
    navigate('/login', { replace: true, state: { passwordChanged: true, auditWarning } })
  }, [navigate])

  const value = useMemo(() => ({ ...state, login, logout, changePassword }), [state, login, logout, changePassword])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('AuthProvider is missing')
  return value
}
