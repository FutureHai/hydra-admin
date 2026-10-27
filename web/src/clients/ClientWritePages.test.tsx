import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Form } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import { ClientFieldSections, FieldList, SpecClientCreate } from './ClientWritePages'

const { listClients, createClient } = vi.hoisted(() => ({ listClients: vi.fn(), createClient: vi.fn() }))
vi.mock('./api', async importOriginal => ({
  ...(await importOriginal<typeof import('./api')>()),
  listClients,
  createClient,
}))

beforeEach(() => { listClients.mockReset(); createClient.mockReset() })

it('validates and submits metadata as a JSON object when creating a Client', async () => {
  listClients.mockResolvedValue({ items: [], capabilities: { client_create: { enabled: true } }, writable_schema: [
    { name: 'client_id', kind: 'text', required: true },
    { name: 'metadata', kind: 'json' },
  ] })
  createClient.mockResolvedValue({ client: { client_id: 'metadata-test', fields: {} } })
  render(<MemoryRouter><SpecClientCreate /></MemoryRouter>)
  const metadata = await screen.findByRole('textbox', { name: 'Metadata（JSON）' })
  fireEvent.change(screen.getByRole('textbox', { name: 'Client ID' }), { target: { value: 'metadata-test' } })
  fireEvent.change(metadata, { target: { value: '{"incomplete":' } })
  fireEvent.click(screen.getByRole('button', { name: '检查创建摘要' }))
  expect(await screen.findByText('请输入有效 JSON')).toBeInTheDocument()
  expect(screen.queryByRole('dialog', { name: '确认创建 Client' })).not.toBeInTheDocument()

  fireEvent.change(metadata, { target: { value: '{"department":"billing","priority":2}' } })
  fireEvent.click(screen.getByRole('button', { name: '检查创建摘要' }))
  const dialog = await screen.findByRole('dialog', { name: '确认创建 Client' })
  expect(within(dialog).getByText(/"department": "billing"/)).toBeInTheDocument()
  fireEvent.click(within(dialog).getByRole('button', { name: '确认创建' }))
  await waitFor(() => expect(createClient).toHaveBeenCalledWith({
    client_id: 'metadata-test', metadata: { department: 'billing', priority: 2 },
  }))
})

it('shows adapter-provided grant and response type choices on the create form', async () => {
  render(<Form><FieldList create schema={[
    { name: 'grant_types', kind: 'array', required: true, options: ['authorization_code', 'implicit'] },
    { name: 'response_types', kind: 'array', options: ['code', 'token', 'code id_token'] },
  ]} /></Form>)
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Response types' }))
  expect(await screen.findByTitle('token')).toBeInTheDocument()
  expect(screen.getByTitle('code id_token')).toBeInTheDocument()
})

it('offers the versioned access token strategy without preselecting an override', async () => {
  render(<Form><ClientFieldSections create schema={[
    { name: 'client_id', kind: 'text', required: true },
    { name: 'access_token_strategy', kind: 'select', options: ['opaque', 'jwt'], help: '留空沿用 Hydra 全局策略。' },
  ]} /></Form>)
  expect(screen.getByRole('heading', { name: '授权与令牌' })).toBeInTheDocument()
  expect(screen.getByText('沿用实例默认策略')).toBeInTheDocument()
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Access Token 策略' }))
  expect(await screen.findByTitle('opaque')).toBeInTheDocument()
  expect(screen.getByTitle('jwt')).toBeInTheDocument()
})

it('groups only the fields allowed by the edit schema', () => {
  render(<Form><ClientFieldSections schema={[
    { name: 'client_name', kind: 'text' },
    { name: 'grant_types', kind: 'array', options: ['authorization_code'] },
    { name: 'redirect_uris', kind: 'array' },
  ]} /></Form>)
  expect(screen.getByRole('heading', { name: '基础信息' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '授权与令牌' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '回调与来源' })).toBeInTheDocument()
  expect(screen.queryByRole('combobox', { name: 'Access Token 策略' })).not.toBeInTheDocument()
  expect(screen.queryByRole('textbox', { name: 'Client ID' })).not.toBeInTheDocument()
})

it('shows resource scopes under their audience and preserves existing unmatched values', async () => {
  const onFinish = vi.fn()
  render(<Form onFinish={onFinish} initialValues={{
    audience: ['https://api.example.test/orders'], scope: ['orders.read', 'legacy.permission'],
  }}><ClientFieldSections schema={[
    { name: 'scope', kind: 'text' }, { name: 'audience', kind: 'array' },
  ]} resourceCatalog={{ global_scopes: ['openid'], audiences: [
    { audience: 'https://api.example.test/orders', scopes: ['orders.read', 'orders.write'] },
  ] }} /><button type="submit">提交</button></Form>)
  expect(screen.getByRole('combobox', { name: 'https://api.example.test/orders 的 Scope' })).toBeInTheDocument()
  expect(screen.getByRole('combobox', { name: '未归属 Scope' })).toBeInTheDocument()
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'https://api.example.test/orders 的 Scope' }))
  fireEvent.click(await screen.findByTitle('orders.write'))
  fireEvent.click(screen.getByRole('button', { name: '提交' }))
  await waitFor(() => expect(onFinish).toHaveBeenCalledWith(expect.objectContaining({
    audience: ['https://api.example.test/orders'], scope: expect.arrayContaining(['orders.read', 'orders.write', 'legacy.permission']),
  })))
})

it('adds scopes for a second audience and removes owned scopes with the audience', async () => {
  const onFinish = vi.fn()
  render(<Form onFinish={onFinish} initialValues={{ audience: ['https://api.example.test/orders'], scope: ['orders.read'] }}>
    <ClientFieldSections schema={[{ name: 'scope', kind: 'text' }, { name: 'audience', kind: 'array' }]} resourceCatalog={{ audiences: [
      { audience: 'https://api.example.test/orders', scopes: ['orders.read'] },
      { audience: 'https://api.example.test/billing', scopes: ['billing.read'] },
    ] }} /><button type="submit">提交</button>
  </Form>)
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Audience（目标资源）' }))
  fireEvent.click(await screen.findByTitle('https://api.example.test/billing'))
  expect(await screen.findByRole('combobox', { name: 'https://api.example.test/billing 的 Scope' })).toBeInTheDocument()
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'https://api.example.test/billing 的 Scope' }))
  fireEvent.click(await screen.findByTitle('billing.read'))
  fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Audience（目标资源）' }))
  const ordersOption = screen.getAllByTitle('https://api.example.test/orders').find(node => node.classList.contains('ant-select-item-option'))
  expect(ordersOption).toBeDefined()
  fireEvent.click(ordersOption!)
  fireEvent.click(screen.getByRole('button', { name: '提交' }))
  await waitFor(() => expect(onFinish).toHaveBeenCalledWith(expect.objectContaining({
    audience: ['https://api.example.test/billing'], scope: ['billing.read'],
  })))
})
