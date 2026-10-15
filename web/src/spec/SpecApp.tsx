import { useState, type ReactNode } from 'react'
import { ConfigProvider } from 'antd'
import {
  AppstoreOutlined, AuditOutlined, DatabaseOutlined, HomeOutlined,
  KeyOutlined, LogoutOutlined, MenuOutlined, SafetyCertificateOutlined,
  SettingOutlined, TagsOutlined,
} from '@ant-design/icons'
import { Link, Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from '../auth/AuthProvider'
import { SpecLogin } from './SpecLogin'
import { SpecOverview, SpecSystem } from './SpecOverview'
import { SpecAudit, SpecCollection } from './SpecPages'
import { SpecClientDetail, SpecClients } from '../clients/ClientPages'
import { SpecClientCreate } from '../clients/ClientWritePages'
import { TokenPage } from '../tokens/TokenPage'

export const specTheme = {
  token: {
    colorPrimary: '#2563EB',
    colorInfo: '#2563EB',
    colorSuccess: '#15803D',
    colorWarning: '#B45309',
    colorError: '#B42318',
    colorText: '#111827',
    colorTextSecondary: '#6B7280',
    colorBgLayout: '#F5F6F8',
    colorBorder: '#E5E7EB',
    borderRadius: 4,
    controlHeight: 32,
    fontFamily: 'system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif',
  },
  components: {
    Card: { borderRadiusLG: 6 },
    Table: { cellPaddingBlockSM: 8 },
    Button: { primaryShadow: 'none' },
  },
}

type StatusTone = 'success' | 'warning' | 'danger' | 'unknown' | 'unverified'

export function StatusBadge({ tone, children }: { tone: StatusTone; children: ReactNode }) {
  return <span className={`spec-status spec-status-${tone}`}><span className="spec-status-dot" />{children}</span>
}

export function ReasonBlock({ title, children, tone = 'unknown' }: { title: string; children: ReactNode; tone?: 'unknown' | 'danger' | 'warning' }) {
  return <div className={`spec-reason spec-reason-${tone}`} role="note"><StatusBadge tone={tone}>{title}</StatusBadge><span>{children}</span></div>
}

export function Machine({ children, title }: { children: ReactNode; title?: string }) {
  return <span className="spec-mono" title={title}>{children}</span>
}

const nav = [
  { path: '/', label: '概览', icon: <HomeOutlined /> },
  { path: '/clients', label: 'Client', icon: <AppstoreOutlined /> },
  { path: '/scopes', label: 'Scope', icon: <TagsOutlined /> },
  { path: '/audiences', label: 'Audience', icon: <DatabaseOutlined /> },
  { path: '/token', label: 'Token 工具', icon: <KeyOutlined /> },
  { path: '/audit', label: '审计', icon: <AuditOutlined /> },
  { path: '/system', label: '系统状态', icon: <SettingOutlined /> },
]

const titles: Record<string, string> = {
  '/': '概览', '/clients': 'Client', '/clients/new': '创建 Client',
  '/scopes': 'Scope', '/audiences': 'Audience', '/token': 'Token 工具',
  '/audit': '审计', '/system': '系统状态',
}

function Shell() {
  const location = useLocation()
  const { logout } = useAuth()
  const [mobileNav, setMobileNav] = useState(false)
  const [logoutError, setLogoutError] = useState(false)
  const title = titles[location.pathname] ?? (location.pathname.startsWith('/clients/') ? 'Client 详情' : '概览')
  return <div className="spec-ui spec-shell">
    <aside className={`spec-sidebar ${mobileNav ? 'spec-sidebar-open' : ''}`}>
      <Link to="/" className="spec-brand" onClick={() => setMobileNav(false)}>
        <span className="spec-brand-mark"><SafetyCertificateOutlined /></span>
        <span><strong>Hydra Admin</strong><small>CONSOLE</small></span>
      </Link>
      <div className="spec-sidebar-rule" />
      <div className="spec-nav-caption">工作空间</div>
      <nav aria-label="主导航">
        {nav.map(item => <Link key={item.path} to={item.path} className={`spec-nav-item ${location.pathname === item.path || (item.path === '/clients' && location.pathname.startsWith('/clients/')) ? 'spec-nav-active' : ''}`} onClick={() => setMobileNav(false)} aria-current={location.pathname === item.path ? 'page' : undefined}>{item.icon}<span>{item.label}</span></Link>)}
      </nav>
    </aside>
    <div className="spec-main">
      <header className="spec-topbar">
        <button className="spec-menu-button" type="button" onClick={() => setMobileNav(!mobileNav)} aria-label="切换导航" aria-expanded={mobileNav}><MenuOutlined /></button>
        <div className="spec-crumb"><span>首页</span><span className="spec-crumb-separator">/</span><strong>{title}</strong></div>
        <div className="spec-header-actions">{logoutError && <span role="alert" className="spec-logout-error">退出失败，请重试</span>}<button type="button" className="spec-logout" aria-label="退出登录" onClick={() => { setLogoutError(false); void logout().catch(() => setLogoutError(true)) }}><LogoutOutlined /><span>退出登录</span></button></div>
      </header>
      <div className="spec-content"><div className="spec-content-inner"><Outlet /></div></div>
    </div>
  </div>
}

function PreviewRouteRedirect() {
  const { pathname } = useLocation()
  return <Navigate to={pathname.slice('/spec'.length) || '/'} replace />
}

export function SpecApp() {
  const nonce = document.querySelector('meta[name="csp-nonce"]')?.getAttribute('content') ?? ''
  return <ConfigProvider theme={specTheme} csp={{ nonce }}>
    <Routes>
      <Route path="login" element={<div className="spec-ui"><SpecLogin /></div>} />
      <Route element={<RequireAuth><Shell /></RequireAuth>}>
        <Route path="/" element={<SpecOverview />} />
        <Route path="clients" element={<SpecClients />} />
        <Route path="clients/new" element={<SpecClientCreate />} />
        <Route path="clients/:clientId" element={<SpecClientDetail />} />
        <Route path="scopes" element={<SpecCollection kind="scope" />} />
        <Route path="audiences" element={<SpecCollection kind="audience" />} />
        <Route path="token" element={<TokenPage />} />
        <Route path="audit" element={<SpecAudit />} />
        <Route path="system" element={<SpecSystem />} />
      </Route>
      <Route path="spec/*" element={<PreviewRouteRedirect />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  </ConfigProvider>
}

function RequireAuth({ children }: { children: ReactNode }) {
  const { loading, session } = useAuth()
  if (loading) return <div className="spec-ui spec-login" role="status">正在检查登录状态…</div>
  if (!session) return <Navigate to="/login" replace />
  return children
}
