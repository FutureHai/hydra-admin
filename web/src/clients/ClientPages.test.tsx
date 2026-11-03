import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { SpecClientDetail, SpecClients } from './ClientPages'

const { listClients, getClient, patchClient, setClientLifespans } = vi.hoisted(() => ({ listClients: vi.fn(), getClient: vi.fn(), patchClient: vi.fn(), setClientLifespans: vi.fn() }))
vi.mock('./api', async importOriginal => ({
  ...(await importOriginal<typeof import('./api')>()),
  listClients,
  getClient,
  patchClient,
  setClientLifespans,
}))

beforeEach(() => { listClients.mockReset(); getClient.mockReset(); patchClient.mockReset(); setClientLifespans.mockReset() })

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

it('shows Metadata in Client details and submits edited JSON as one explicit patch field', async () => {
  getClient.mockResolvedValue({
    client: { client_id: 'metadata-test', fields: { client_name: 'Metadata test', metadata: { department: 'old', priority: 2 }, client_credentials_grant_access_token_lifespan: '2h', skip_consent: true } },
    revision: 'baseline', editable_fields: ['metadata'], capabilities: { client_update: { enabled: true } },
    writable_schema: [{ name: 'metadata', kind: 'json' }, { name: 'client_credentials_grant_access_token_lifespan', kind: 'duration' }],
  })
  patchClient.mockResolvedValue({})
  render(<MemoryRouter initialEntries={['/clients/metadata-test']}><Routes><Route path="/clients/:clientId" element={<SpecClientDetail />} /></Routes></MemoryRouter>)
  fireEvent.click(await screen.findByRole('tab', { name: '高级配置' }))
  expect(screen.getByText('2h')).toBeInTheDocument()
  expect(screen.getByText('开启')).toBeInTheDocument()
  fireEvent.click(await screen.findByRole('tab', { name: 'Metadata（JSON）' }))
  expect(screen.getByText(/"department": "old"/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /编\s*辑/ }))
  const editor = await screen.findByRole('textbox', { name: 'Metadata（JSON）' })
  expect(editor).toHaveValue(JSON.stringify({ department: 'old', priority: 2 }, null, 2))
  fireEvent.change(editor, { target: { value: '{"department":' } })
  fireEvent.click(screen.getByRole('button', { name: '检查修改摘要' }))
  expect(await screen.findByText('请输入有效 JSON')).toBeInTheDocument()
  expect(patchClient).not.toHaveBeenCalled()
  fireEvent.change(editor, { target: { value: '{"department":"new","priority":2}' } })
  fireEvent.click(screen.getByRole('button', { name: '检查修改摘要' }))
  expect(await screen.findByText('确认保存修改')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '提交 JSON Patch' }))
  await waitFor(() => expect(patchClient).toHaveBeenCalledWith('metadata-test', 'baseline', { metadata: { department: 'new', priority: 2 } }))
})

it('does not prefill redacted Metadata into the edit form', async () => {
  getClient.mockResolvedValue({
    client: { client_id: 'metadata-test', fields: { metadata: { department: 'billing', api_key: '[redacted]' } } },
    revision: 'baseline', editable_fields: ['metadata'], capabilities: { client_update: { enabled: true } },
    writable_schema: [{ name: 'metadata', kind: 'json' }],
  })
  render(<MemoryRouter initialEntries={['/clients/metadata-test']}><Routes><Route path="/clients/:clientId" element={<SpecClientDetail />} /></Routes></MemoryRouter>)
  fireEvent.click(await screen.findByRole('button', { name: /编\s*辑/ }))
  expect(screen.getByRole('textbox', { name: 'Metadata（JSON）' })).toHaveValue('')
  expect(screen.getByText(/编辑框不会回填它/)).toBeInTheDocument()
})

it('edits advanced consent and logout fields through JSON Patch', async () => {
  getClient.mockResolvedValue({
    client: { client_id: 'advanced-edit', fields: { grant_types: ['client_credentials'], skip_consent: false, backchannel_logout_uri: 'https://example.invalid/logout' } },
    revision: 'advanced-baseline', editable_fields: ['skip_consent', 'backchannel_logout_uri'], capabilities: { client_update: { enabled: true } },
    writable_schema: [{ name: 'skip_consent', kind: 'boolean' }, { name: 'backchannel_logout_uri', kind: 'text' }],
  })
  patchClient.mockResolvedValue({})
  render(<MemoryRouter initialEntries={['/clients/advanced-edit']}><Routes><Route path="/clients/:clientId" element={<SpecClientDetail />} /></Routes></MemoryRouter>)
  fireEvent.click(await screen.findByRole('tab', { name: '高级配置' }))
  fireEvent.click(screen.getByRole('button', { name: '编辑授权与登出' }))
  const logoutURI = await screen.findByRole('textbox', { name: '后通道登出 URI' })
  expect(logoutURI).toHaveValue('https://example.invalid/logout')
  fireEvent.change(logoutURI, { target: { value: '' } })
  fireEvent.mouseDown(screen.getByRole('combobox', { name: '跳过授权同意页' }))
  fireEvent.click(await screen.findByTitle('开启'))
  fireEvent.click(screen.getByRole('button', { name: '检查修改摘要' }))
  expect(await screen.findByText('确认保存修改')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '提交 JSON Patch' }))
  await waitFor(() => expect(patchClient).toHaveBeenCalledWith('advanced-edit', 'advanced-baseline', { skip_consent: true, backchannel_logout_uri: '' }))
})

it('does not patch an unset boolean when editing a logout URI', async () => {
  getClient.mockResolvedValue({
    client: { client_id: 'advanced-unset', fields: { skip_logout_consent: null, frontchannel_logout_uri: null } },
    revision: 'unset-baseline', editable_fields: ['skip_logout_consent', 'frontchannel_logout_uri'], capabilities: { client_update: { enabled: true } },
    writable_schema: [{ name: 'skip_logout_consent', kind: 'boolean' }, { name: 'frontchannel_logout_uri', kind: 'text' }],
  })
  patchClient.mockResolvedValue({})
  render(<MemoryRouter initialEntries={['/clients/advanced-unset']}><Routes><Route path="/clients/:clientId" element={<SpecClientDetail />} /></Routes></MemoryRouter>)
  fireEvent.click(await screen.findByRole('tab', { name: '高级配置' }))
  fireEvent.click(screen.getByRole('button', { name: '编辑授权与登出' }))
  fireEvent.change(await screen.findByRole('textbox', { name: '前通道登出 URI' }), { target: { value: 'https://example.invalid/logout' } })
  fireEvent.click(screen.getByRole('button', { name: '检查修改摘要' }))
  expect(await screen.findByText('确认保存修改')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '提交 JSON Patch' }))
  await waitFor(() => expect(patchClient).toHaveBeenCalledWith('advanced-unset', 'unset-baseline', { frontchannel_logout_uri: 'https://example.invalid/logout' }))
})

it('changes one lifespan with a dedicated request and can reset it to the Hydra default', async () => {
  getClient.mockResolvedValue({
    client: { client_id: 'lifespan-edit', fields: { grant_types: ['client_credentials'], client_credentials_grant_access_token_lifespan: '2h0m0s' } },
    revision: 'lifespan-baseline', editable_fields: [], editable_lifespan_fields: ['client_credentials_grant_access_token_lifespan'],
    capabilities: { client_lifespans_update: { enabled: true } },
    writable_schema: [{ name: 'client_credentials_grant_access_token_lifespan', kind: 'duration' }],
  })
  setClientLifespans.mockResolvedValue({})
  render(<MemoryRouter initialEntries={['/clients/lifespan-edit']}><Routes><Route path="/clients/:clientId" element={<SpecClientDetail />} /></Routes></MemoryRouter>)
  fireEvent.click(await screen.findByRole('tab', { name: '高级配置' }))
  fireEvent.click(await screen.findByRole('button', { name: '调整 Token 有效期' }))
  const lifespan = await screen.findByRole('textbox', { name: 'Client Credentials · Access Token 有效期' })
  expect(lifespan).toHaveValue('2h0m0s')
  fireEvent.change(lifespan, { target: { value: '3h' } })
  fireEvent.click(screen.getByRole('button', { name: '检查有效期修改' }))
  expect(await screen.findByText('确认 Token 有效期修改')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '确认修改有效期' }))
  await waitFor(() => expect(setClientLifespans).toHaveBeenCalledWith('lifespan-edit', 'lifespan-baseline', { client_credentials_grant_access_token_lifespan: '3h' }))
  fireEvent.click(await screen.findByRole('button', { name: '调整 Token 有效期' }))
  const reopened = await screen.findByRole('textbox', { name: 'Client Credentials · Access Token 有效期' })
  fireEvent.change(reopened, { target: { value: '' } })
  fireEvent.click(screen.getByRole('button', { name: '检查有效期修改' }))
  expect(await screen.findByText('恢复 Hydra 默认值')).toBeInTheDocument()
})
