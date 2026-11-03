import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Alert, Button, Empty, Form, Input, Modal, Select, Table, Tabs, Tag } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ArrowLeftOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Machine, ReasonBlock } from '../spec/SpecApp'
import { PageHeading, reasonLabel } from '../spec/SpecOverview'
import { clientErrorMessage, deleteClient, getClient, listClients, patchClient, setClientLifespans, textArrayField, textField, type ClientDetailResponse, type ClientRead } from './api'
import { ClientFieldSections, FieldList, fieldLabel, parseMetadataInput, sameHydraDuration, Summary } from './ClientWritePages'
import { sameScopes, scopeTokens, scopeWireValue } from './resourceAccess'

const dash = '—'

function hasRedactedValue(value: unknown): boolean {
  if (value === '[redacted]') return true
  if (Array.isArray(value)) return value.some(hasRedactedValue)
  if (value && typeof value === 'object') return Object.values(value).some(hasRedactedValue)
  return false
}

function tags(values: string[] | undefined) {
  return values?.length ? <div className="spec-tag-list">{values.map(value => <Tag key={value}><Machine>{value}</Machine></Tag>)}</div> : dash
}

function DetailRow({ label, value }: { label: string; value: ReactNode }) {
  return <div className="spec-detail-row"><span>{label}</span><div className="spec-detail-value">{value || dash}</div></div>
}

const columns: ColumnsType<ClientRead> = [
  { title: 'Client Id', dataIndex: 'client_id', key: 'id', width: 220, render: (id: string) => <Machine title={id}>{id}</Machine> },
  { title: 'Client 名称', key: 'name', width: 300, render: (_, client) => textField(client, 'client_name') ?? dash },
  { title: 'Grant Types', key: 'grants', width: 220, render: (_, client) => tags(textArrayField(client, 'grant_types')) },
  { title: 'Scope', key: 'scope', width: 240, render: (_, client) => <Machine>{textField(client, 'scope') ?? dash}</Machine> },
  { title: '操作', key: 'action', width: 110, align: 'right', render: (_, client) => <Link to={`/clients/${encodeURIComponent(client.client_id)}`} className="spec-text-link">查看详情</Link> },
]

export function SpecClients() {
  const [items, setItems] = useState<ClientRead[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [grant, setGrant] = useState<string | undefined>()
  const [reload, setReload] = useState(0)
  const [canCreate, setCanCreate] = useState(false)
  const [pageTokens, setPageTokens] = useState([''])
  const [pageIndex, setPageIndex] = useState(0)
  const [nextPageToken, setNextPageToken] = useState<string | null>(null)
  const pageToken = pageTokens[pageIndex] ?? ''

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    setNextPageToken(null)
    void listClients(controller.signal, pageToken).then(data => {
      if (!controller.signal.aborted) {
        setItems(data.items)
        setNextPageToken(data.pagination_supported ? data.next_page_token || null : null)
        setCanCreate(Boolean(data.capabilities.client_create?.enabled))
      }
    }).catch(cause => {
      if (!controller.signal.aborted) { setItems([]); setError(clientErrorMessage(cause)) }
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [reload, pageToken])

  function refresh() {
    setPageTokens(['']); setPageIndex(0); setNextPageToken(null); setQuery(''); setGrant(undefined)
    setReload(value => value + 1)
  }
  function nextPage() {
    if (!nextPageToken || loading) return
    setPageTokens(tokens => [...tokens.slice(0, pageIndex + 1), nextPageToken])
    setPageIndex(index => index + 1)
    setQuery(''); setGrant(undefined)
  }
  function previousPage() {
    if (pageIndex === 0 || loading) return
    setPageIndex(index => index - 1)
    setQuery(''); setGrant(undefined)
  }

  const grants = useMemo(() => [...new Set(items.flatMap(client => textArrayField(client, 'grant_types') ?? []))].sort(), [items])
  const visible = useMemo(() => items.filter(client => {
    const name = textField(client, 'client_name') ?? ''
    return (!query || `${client.client_id} ${name}`.toLowerCase().includes(query.toLowerCase())) && (!grant || (textArrayField(client, 'grant_types') ?? []).includes(grant))
  }), [items, query, grant])

  return <>
    <PageHeading kicker="Client / 列表" title="Client" description="查看当前 Hydra 实例中的 Client。" action={<><Button icon={<ReloadOutlined />} onClick={refresh} loading={loading}>刷新</Button> {canCreate && <Link to="/clients/new"><Button type="primary">创建 Client</Button></Link>}</>} />
    {error && <Alert type="error" showIcon className="spec-inline-alert" title={error} role="alert" />}
    <section className="spec-panel">
      <div className="spec-table-toolbar"><div><strong>Client 列表</strong><span>第 {pageIndex + 1} 页 · 本页 {items.length} 条</span></div><div className="spec-filter-group"><Input prefix={<SearchOutlined />} aria-label="筛选当前页 Client" placeholder="筛选当前页的 ID 或名称" value={query} onChange={event => setQuery(event.target.value)} allowClear /><Select aria-label="按 Grant types 筛选当前页" placeholder="全部 Grant types" value={grant} onChange={setGrant} allowClear options={grants.map(value => ({ label: value, value }))} /></div></div>
      <div className="spec-table-scroll"><Table<ClientRead> size="small" columns={columns} dataSource={visible} rowKey="client_id" loading={loading} pagination={false} scroll={{ x: 1100 }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={error ? '读取失败' : '当前读取范围内没有 Client'} /> }} /></div>
      <div className="spec-client-pager"><span>按 Hydra 游标逐页读取；筛选只作用于当前页，总数未知。</span><div><Button onClick={previousPage} disabled={loading || pageIndex === 0}>上一页</Button><Button onClick={nextPage} disabled={loading || !nextPageToken}>下一页</Button></div></div>
    </section>
  </>
}

export function SpecClientDetail() {
  const { clientId } = useParams()
  const navigate = useNavigate()
  const [detail, setDetail] = useState<ClientDetailResponse | null>(null)
  const client = detail?.client ?? null
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const [activeTab, setActiveTab] = useState('basic')
  const [editOpen, setEditOpen] = useState(false)
  const [editAdvanced, setEditAdvanced] = useState(false)
  const [lifespanOpen, setLifespanOpen] = useState(false)
  const [lifespanPreview, setLifespanPreview] = useState<Record<string, unknown> | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteInput, setDeleteInput] = useState('')
  const [preview, setPreview] = useState<Record<string, unknown> | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<Record<string, unknown>>()
  const [lifespanForm] = Form.useForm<Record<string, unknown>>()

  useEffect(() => setActiveTab('basic'), [clientId])

  useEffect(() => {
    if (!clientId) { setLoading(false); setError('Client ID 无效。'); return }
    const controller = new AbortController()
    setDetail(null)
    setLoading(true)
    setError(null)
    void getClient(clientId, controller.signal).then(data => setDetail(data)).catch(cause => {
      if (!controller.signal.aborted) setError(clientErrorMessage(cause))
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [clientId, reload])

  const back = <Link to="/clients"><Button icon={<ArrowLeftOutlined />}>返回列表</Button></Link>
  if (loading) return <><PageHeading kicker="Client / 详情" title="正在读取 Client" description="从 Hydra Admin API 获取只读详情。" action={back} /><div role="status">正在加载…</div></>
  if (!client) return <><PageHeading kicker="Client / 详情" title="无法显示 Client" description="详情未加载。" action={back} /><Alert type="error" showIcon title={error ?? '无法读取 Client 数据。'} role="alert" /><Button icon={<ReloadOutlined />} onClick={() => setReload(value => value + 1)}>重试</Button></>

  const metadataRedacted = hasRedactedValue(client.fields.metadata)
  const editable = (detail?.writable_schema ?? []).filter(field => detail?.editable_fields.includes(field.name))
  const grantTypes = textArrayField(client, 'grant_types')
  const editableLifespans = (detail?.writable_schema ?? []).filter(field => field.kind === 'duration' && detail?.editable_lifespan_fields?.includes(field.name)).filter(field => {
    if (!grantTypes?.length) return true
    const grant = field.name.split('_grant_')[0]
    return grantTypes.includes(grant === 'device_authorization' ? 'urn:ietf:params:oauth:grant-type:device_code' : grant)
  })
  const advancedNames = ['skip_consent', 'skip_logout_consent', 'frontchannel_logout_uri', 'frontchannel_logout_session_required', 'backchannel_logout_uri', 'backchannel_logout_session_required']
  const configuredAdvanced = [...advancedNames, ...(detail?.writable_schema ?? []).filter(field => field.kind === 'duration').map(field => field.name)]
    .filter(name => client.fields[name] !== undefined && client.fields[name] !== null)
  function beginEdit(advanced = false) {
    const values: Record<string, unknown> = {}
    for (const field of editable) {
      if (client?.fields[field.name] == null) continue
      if (field.name === 'metadata') {
        if (!metadataRedacted) values.metadata = JSON.stringify(client.fields.metadata, null, 2)
      } else values[field.name] = field.name === 'scope' ? scopeTokens(client.fields[field.name]) : field.kind === 'boolean' && typeof client.fields[field.name] === 'boolean' ? String(client.fields[field.name]) : client.fields[field.name]
    }
    form.resetFields(); form.setFieldsValue(values); setEditAdvanced(advanced); setEditOpen(true)
  }
  function reviewEdit(values: Record<string, unknown>) {
    if (!client) return
    const changes: Record<string, unknown> = {}
    for (const field of editable) {
      const value = values[field.name]
      if (field.name === 'scope') {
        if (value !== undefined && !sameScopes(value, client?.fields.scope)) changes.scope = scopeWireValue(value)
      } else if (field.name === 'metadata') {
        if (typeof value === 'string' && value.trim()) {
          const parsed = parseMetadataInput(value)
          if (JSON.stringify(parsed) !== JSON.stringify(client?.fields.metadata)) changes.metadata = parsed
        }
      } else if (value !== undefined && !(field.kind === 'boolean' && value === null)) {
        const normalized = field.kind === 'boolean' ? value === 'true' : value
        if (JSON.stringify(normalized) !== JSON.stringify(client?.fields[field.name])) changes[field.name] = normalized
      }
    }
    for (const channel of ['frontchannel', 'backchannel']) {
      const required = changes[`${channel}_logout_session_required`] ?? client.fields[`${channel}_logout_session_required`]
      const uri = changes[`${channel}_logout_uri`] ?? client.fields[`${channel}_logout_uri`]
      if (required === true && !uri) {
        form.setFields([{ name: `${channel}_logout_uri`, errors: ['启用会话标识时需要填写对应的登出 URI'] }])
        return
      }
    }
    if (Object.keys(changes).length) { setPreview(changes); setEditOpen(false) }
  }

  function beginLifespanEdit() {
    if (!client) return
    const values: Record<string, unknown> = {}
    for (const field of editableLifespans) if (typeof client.fields[field.name] === 'string') values[field.name] = client.fields[field.name]
    lifespanForm.resetFields(); lifespanForm.setFieldsValue(values); setLifespanOpen(true)
  }

  function reviewLifespans(values: Record<string, unknown>) {
    if (!client) return
    const changes: Record<string, unknown> = {}
    for (const field of editableLifespans) {
      const value = values[field.name]
      const previous = client.fields[field.name]
      if (typeof value === 'string' && value !== '') {
        if (!sameHydraDuration(value, previous)) changes[field.name] = value
      } else if (typeof previous === 'string' && previous !== '') changes[field.name] = null
    }
    if (Object.keys(changes).length) { setLifespanPreview(changes); setLifespanOpen(false) }
  }

  async function saveLifespans() {
    if (!clientId || !detail || !lifespanPreview || saving) return
    setSaving(true); setError(null)
    try { await setClientLifespans(clientId, detail.revision, lifespanPreview); setLifespanPreview(null); setReload(value => value + 1) }
    catch (cause) { setLifespanPreview(null); setError(clientErrorMessage(cause)) }
    finally { setSaving(false) }
  }
  async function saveEdit() {
    if (!clientId || !detail || !preview || saving) return
    setSaving(true); setError(null)
    try { await patchClient(clientId, detail.revision, preview); setPreview(null); setReload(value => value + 1) }
    catch (cause) { setPreview(null); setError(clientErrorMessage(cause)) }
    finally { setSaving(false) }
  }
  async function confirmDelete() {
    if (!clientId || deleteInput !== clientId || saving) return
    setSaving(true); setError(null)
    try { await deleteClient(clientId); setDeleteOpen(false); navigate('/clients') }
    catch (cause) { setDeleteOpen(false); setError(clientErrorMessage(cause)) }
    finally { setSaving(false) }
  }

  const tabs = [
    { key: 'basic', label: '基本信息', children: <div className="spec-detail-list"><DetailRow label="Client Id" value={<Machine>{client.client_id}</Machine>} /><DetailRow label="Client名称" value={textField(client, 'client_name')} /><DetailRow label="Owner" value={textField(client, 'owner')} /><DetailRow label="状态" value={dash} /></div> },
    { key: 'auth', label: '授权与认证', children: <div className="spec-detail-list"><DetailRow label="Grant types" value={tags(textArrayField(client, 'grant_types'))} /><DetailRow label="Response types" value={tags(textArrayField(client, 'response_types'))} /><DetailRow label="认证方式" value={textField(client, 'token_endpoint_auth_method')} /></div> },
    { key: 'uris', label: 'Redirect URIs & CORS', children: <div className="spec-detail-list"><DetailRow label="Redirect URIs" value={tags(textArrayField(client, 'redirect_uris'))} /><DetailRow label="CORS origins" value={tags(textArrayField(client, 'allowed_cors_origins'))} /><DetailRow label="JWKS URI" value={textField(client, 'jwks_uri')} /></div> },
    { key: 'scope', label: 'Scope & Audience', children: <div className="spec-detail-list"><DetailRow label="Scope" value={<Machine>{textField(client, 'scope') ?? dash}</Machine>} /><DetailRow label="Audience" value={tags(textArrayField(client, 'audience'))} /></div> },
    { key: 'advanced', label: '高级配置', children: <>
      <div className="spec-advanced-actions">
        {detail?.capabilities.client_update?.enabled && advancedNames.some(name => detail.editable_fields.includes(name)) && <Button onClick={() => beginEdit(true)}>编辑授权与登出</Button>}
        {detail?.capabilities.client_lifespans_update?.enabled && editableLifespans.length > 0 && <Button onClick={beginLifespanEdit}>调整 Token 有效期</Button>}
      </div>
      {configuredAdvanced.length ? <div className="spec-detail-list">{configuredAdvanced.map(name => <DetailRow key={name} label={fieldLabel(name)} value={typeof client.fields[name] === 'boolean' ? client.fields[name] ? '开启' : '关闭' : typeof client.fields[name] === 'string' ? <Machine>{client.fields[name] as string}</Machine> : dash} />)}</div> : <p className="spec-field-note">未单独设置；使用 Hydra 实例默认值。</p>}
    </> },
    { key: 'metadata', label: 'Metadata（JSON）', children: <>{client.fields.metadata === undefined ? <p className="spec-field-note">{dash}</p> : <pre className="spec-json-view">{JSON.stringify(client.fields.metadata, null, 2)}</pre>}{metadataRedacted && <p className="spec-field-note">部分字段已脱敏。编辑时需提供完整的替换内容。</p>}</> },
    { key: 'json', label: '只读 JSON', children: <><p className="spec-field-note">保留 Hydra 返回的未声明字段；Secret 与私钥字段已脱敏。</p><pre className="spec-json-view">{JSON.stringify(client.fields, null, 2)}</pre></> },
  ]
  return <>
    <PageHeading kicker="Client / 详情" title={textField(client, 'client_name') ?? client.client_id} description="来自当前 Hydra 实例；缺失字段显示横线。" action={back} />
    {error && <Alert type="error" showIcon title={error} className="spec-inline-alert" role="alert" />}
    <section className="spec-panel spec-detail-panel"><div className="spec-panel-head"><div><h2><Machine>{client.client_id}</Machine></h2><p>可用操作以当前能力报告和可编辑字段为准</p></div><div>{detail?.capabilities.client_update?.enabled && editable.length > 0 && <Button onClick={() => beginEdit()}>编辑</Button>} {detail?.capabilities.client_delete?.enabled && <Button danger onClick={() => { setDeleteInput(''); setDeleteOpen(true) }}>删除</Button>}</div></div><Tabs activeKey={activeTab} onChange={setActiveTab} items={tabs} /></section>
    <ReasonBlock title="状态未声明">此版本 Client 响应没有统一状态字段，页面不推断启用或禁用状态。</ReasonBlock>
    {!detail?.capabilities.secret_rotate?.enabled && <ReasonBlock title="Secret 轮换不可用">{reasonLabel(detail?.capabilities.secret_rotate?.reason)}</ReasonBlock>}
    <Modal title="编辑 Client" className="spec-edit-modal" open={editOpen} onCancel={() => setEditOpen(false)} destroyOnHidden width={860}
      footer={<><Button onClick={() => setEditOpen(false)}>取消</Button><Button type="primary" onClick={() => form.submit()}>检查修改摘要</Button></>}>
      <p className="spec-edit-intro">正在编辑 <Machine>{client.client_id}</Machine>。仅显示当前版本允许修改的字段。</p>
      {metadataRedacted && editable.some(field => field.name === 'metadata') && <Alert type="warning" showIcon title="Metadata 含已脱敏字段，编辑框不会回填它。若要修改，请填写完整的新 JSON；提交后将整体替换原值。" className="spec-inline-alert" />}
      <Form form={form} layout="vertical" className="spec-client-form" onFinish={reviewEdit}><ClientFieldSections schema={editable} resourceCatalog={detail?.resource_catalog} openAdvanced={editAdvanced} /></Form>
    </Modal>
    <Modal title="确认保存修改" open={preview !== null} onCancel={() => setPreview(null)} onOk={() => void saveEdit()} okText="提交 JSON Patch" confirmLoading={saving} destroyOnHidden>
      <p>只提交以下显式修改的字段；不会提交 client_secret。</p>{preview && <Summary fields={preview} resourceCatalog={detail?.resource_catalog} current={client} />}
    </Modal>
    <Modal title="调整 Token 有效期" className="spec-edit-modal" open={lifespanOpen} onCancel={() => setLifespanOpen(false)} destroyOnHidden width={860}
      footer={<><Button onClick={() => setLifespanOpen(false)}>取消</Button><Button type="primary" onClick={() => lifespanForm.submit()}>检查有效期修改</Button></>}>
      <p className="spec-edit-intro">仅显示当前 Grant types 对应的有效期。清空已有值可恢复 Hydra 默认值。</p>
      <Alert type="info" showIcon title="Hydra 会整体替换有效期集合；服务端将保留所有未修改的现有值。若使用 Token 定时清理，请核对清理任务的最长有效期。" className="spec-inline-alert" />
      <Form form={lifespanForm} layout="vertical" className="spec-client-form" onFinish={reviewLifespans}><FieldList schema={editableLifespans} /></Form>
    </Modal>
    <Modal title="确认 Token 有效期修改" open={lifespanPreview !== null} onCancel={() => setLifespanPreview(null)} onOk={() => void saveLifespans()} okText="确认修改有效期" confirmLoading={saving} destroyOnHidden>
      <p>仅修改以下有效期；其他已设置的有效期由服务端保留。</p>{lifespanPreview && <Summary fields={lifespanPreview} />}
    </Modal>
    <Modal title="删除 Client" open={deleteOpen} onCancel={() => setDeleteOpen(false)} onOk={() => void confirmDelete()} okText="二次确认删除" okButtonProps={{ danger: true, disabled: deleteInput !== client.client_id }} confirmLoading={saving} destroyOnHidden>
      <Alert type="warning" showIcon title="删除不可撤销。请键入精确的 Client ID 后再次确认。" />
      <p><Machine>{client.client_id}</Machine></p>
      <Input value={deleteInput} onChange={event => setDeleteInput(event.target.value)} aria-label="键入 Client ID 确认删除" autoComplete="off" />
    </Modal>
  </>
}
