import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { SpecClients } from './ClientPages'

const { listClients } = vi.hoisted(() => ({ listClients: vi.fn() }))
vi.mock('./api', async importOriginal => ({
  ...(await importOriginal<typeof import('./api')>()),
  listClients,
}))

beforeEach(() => listClients.mockReset())

it('hides create when the active adapter disables it and shows it when enabled', async () => {
  listClients.mockResolvedValueOnce({ items: [], capabilities: { client_create: { enabled: false } }, writable_schema: [] })
  const first = render(<MemoryRouter><SpecClients /></MemoryRouter>)
  await waitFor(() => expect(screen.getByText(/本页 0 条/)).toBeInTheDocument())
  expect(screen.queryByRole('button', { name: '创建 Client' })).not.toBeInTheDocument()
  first.unmount()
  listClients.mockResolvedValueOnce({ items: [], capabilities: { client_create: { enabled: true } }, writable_schema: [] })
  render(<MemoryRouter><SpecClients /></MemoryRouter>)
  await waitFor(() => expect(screen.getByRole('button', { name: '创建 Client' })).toBeInTheDocument())
})

it('uses the returned cursor for the next page and the saved cursor for the previous page', async () => {
  const capabilities = { client_create: { enabled: false }, pagination: { enabled: true } }
  listClients
    .mockResolvedValueOnce({ items: [{ client_id: 'first-client', fields: {} }], next_page_token: 'cursor-2', pagination_supported: true, capabilities, writable_schema: [] })
    .mockResolvedValueOnce({ items: [{ client_id: 'second-client', fields: {} }], next_page_token: '', pagination_supported: true, capabilities, writable_schema: [] })
    .mockResolvedValueOnce({ items: [{ client_id: 'first-client', fields: {} }], next_page_token: 'cursor-2', pagination_supported: true, capabilities, writable_schema: [] })
  render(<MemoryRouter><SpecClients /></MemoryRouter>)
  expect(await screen.findByText('first-client')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '下一页' }))
  expect(await screen.findByText('second-client')).toBeInTheDocument()
  expect(listClients).toHaveBeenLastCalledWith(expect.any(AbortSignal), 'cursor-2')
  expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '上一页' }))
  await waitFor(() => expect(screen.getByText('first-client')).toBeInTheDocument())
  expect(listClients).toHaveBeenLastCalledWith(expect.any(AbortSignal), '')
})
