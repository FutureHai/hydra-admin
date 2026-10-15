import { expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TokenPage } from './TokenPage'

vi.mock('../clients/useHydraStatus', () => ({
  useHydraStatus: () => ({
    status: { capabilities: {
      token_introspect: { enabled: false, reason: 'unsupported_version' },
      token_revoke: { enabled: false, reason: 'public_revoke_not_configured' },
    } },
    error: null,
  }),
}))

it('omits token forms when capabilities are disabled', () => {
  render(<MemoryRouter><TokenPage /></MemoryRouter>)
  expect(screen.getByText('内省不可用')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '执行内省' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '检查撤销确认' })).not.toBeInTheDocument()
  expect(localStorage.length).toBe(0)
  expect(sessionStorage.length).toBe(0)
})
