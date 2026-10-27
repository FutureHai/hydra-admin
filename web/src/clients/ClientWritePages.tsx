import { useEffect, useState } from 'react'
import { Alert, Button, Card, Form, Input, Modal, Select, Spin, Typography } from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'
import { Link, useNavigate } from 'react-router-dom'
import { PageHeading } from '../spec/SpecOverview'
import { clientErrorMessage, createClient, listClients, type ClientRead, type ResourceCatalog, type WritableField } from './api'
import { ResourceAccessFields } from './ResourceAccessFields'
import { catalogEnabled, scopeTokens, scopeWireValue, unmatchedScopes } from './resourceAccess'

const fieldLabels: Record<string, string> = {
  client_id: 'Client ID', client_name: 'Client 名称', grant_types: 'Grant types',
  response_types: 'Response types', token_endpoint_auth_method: 'Token 认证方式',
  access_token_strategy: 'Access Token 策略', scope: 'Scopes', audience: 'Audiences',
  redirect_uris: 'Redirect URIs', post_logout_redirect_uris: '登出回调 URIs',
  allowed_cors_origins: 'CORS origins', owner: 'Owner', contacts: 'Contacts',
  client_uri: 'Client URI', logo_uri: 'Logo URI', policy_uri: 'Policy URI', tos_uri: 'Terms URI',
  metadata: 'Metadata（JSON）',
}

function fieldLabel(name: string) { return fieldLabels[name] ?? name.replaceAll('_', ' ') }

export function parseMetadataInput(value: unknown): unknown {
  if (typeof value !== 'string') throw new Error('请输入有效 JSON')
  return JSON.parse(value) as unknown
}

type FieldControlProps = {
  field: WritableField
  id?: string
  value?: string | string[]
  onChange?: (value: unknown) => void
}

export function FieldControl({ field, id, value, onChange }: FieldControlProps) {
  if (field.kind === 'select') return <Select id={id} value={value as string | undefined} onChange={onChange} allowClear={!field.required} placeholder={field.name === 'access_token_strategy' ? '沿用实例默认策略' : undefined} options={field.options?.map(item => ({ label: item, value: item }))} />
  if (field.name === 'scope') return <Select id={id} value={value as string[] | undefined} onChange={onChange} mode="tags" tokenSeparators={[' ', ',', '\n']} placeholder="输入 Scope 后按 Enter 添加" />
  if (field.kind === 'array') return <Select id={id} value={value as string[] | undefined} onChange={onChange} mode={field.options?.length ? 'multiple' : 'tags'} tokenSeparators={[',', '\n']} options={field.options?.map(item => ({ label: item, value: item }))} placeholder="输入后按 Enter 添加" />
  if (field.kind === 'json') return <Input.TextArea id={id} value={value as string | undefined} onChange={onChange} rows={5} placeholder={'{"department":"example"}'} spellCheck={false} className="spec-json-input" />
  return <Input id={id} value={value as string | undefined} onChange={onChange} autoComplete="off" className={field.name === 'client_id' ? 'spec-mono-input' : undefined} />
}

export function FieldList({ schema, create = false }: { schema: WritableField[]; create?: boolean }) {
  return <div className="spec-form-grid">{schema.map(field => <Form.Item key={field.name} name={field.name} label={fieldLabel(field.name)} extra={field.help} className={field.kind === 'json' ? 'spec-json-field' : undefined} rules={field.kind === 'json' ? [{ validator: async (_, value: unknown) => {
    if (value === undefined || typeof value === 'string' && !value.trim()) return
    try { parseMetadataInput(value) } catch { throw new Error('请输入有效 JSON') }
  } }] : create && field.required ? [{ required: true, message: `请填写 ${field.name}` }] : undefined}>
    <FieldControl field={field} />
  </Form.Item>)}</div>
}

const clientSections = [
  { key: 'identity', title: '基础信息', description: '用于识别和查找这个 Client', fields: ['client_id', 'client_name'] },
  { key: 'authorization', title: '授权与令牌', description: '选择授权流程和 Access Token 的生成方式', fields: ['grant_types', 'response_types', 'token_endpoint_auth_method', 'access_token_strategy', 'scope', 'audience'] },
  { key: 'redirects', title: '回调与来源', description: '配置允许的回调地址和浏览器来源', fields: ['redirect_uris', 'post_logout_redirect_uris', 'allowed_cors_origins'] },
  { key: 'metadata', title: '其他信息', description: '可选的联系人、展示地址和业务数据', fields: ['owner', 'contacts', 'client_uri', 'logo_uri', 'policy_uri', 'tos_uri', 'metadata'] },
]

export function ClientFieldSections({ schema, create = false, resourceCatalog }: { schema: WritableField[]; create?: boolean; resourceCatalog?: ResourceCatalog }) {
  const byName = new Map(schema.map(field => [field.name, field]))
  const placed = new Set(clientSections.flatMap(section => section.fields))
  const sections = clientSections.map(section => ({ ...section, schema: section.fields.map(name => byName.get(name)).filter((field): field is WritableField => Boolean(field)) })).filter(section => section.schema.length)
  const remaining = schema.filter(field => !placed.has(field.name))
  if (remaining.length) sections.push({ key: 'other', title: '其他字段', description: '当前版本额外开放的配置', fields: [], schema: remaining })
  const idPrefix = create ? 'spec-create' : 'spec-edit'
  const groupedAccess = catalogEnabled(resourceCatalog) && byName.has('scope') && byName.has('audience')
  return <>{sections.map(section => <section className="spec-client-section" key={section.key} aria-labelledby={`${idPrefix}-${section.key}`}>
    <div className="spec-client-section-head"><h2 id={`${idPrefix}-${section.key}`}>{section.title}</h2><p>{section.description}</p></div>
    <FieldList schema={section.key === 'authorization' && groupedAccess ? section.schema.filter(field => field.name !== 'scope' && field.name !== 'audience') : section.schema} create={create} />
    {section.key === 'authorization' && groupedAccess && <ResourceAccessFields catalog={resourceCatalog!} />}
  </section>)}</>
}

export function Summary({ fields, resourceCatalog, current }: { fields: Record<string, unknown>; resourceCatalog?: ResourceCatalog; current?: ClientRead }) {
  const showAccess = catalogEnabled(resourceCatalog) && ('scope' in fields || 'audience' in fields)
  const scopes = scopeTokens(fields.scope ?? current?.fields.scope)
  const audiences = (fields.audience ?? current?.fields.audience) as string[] | undefined
  const unmatched = showAccess ? unmatchedScopes(scopes, audiences ?? [], resourceCatalog!) : []
  const listedAudiences = new Set(resourceCatalog?.audiences?.map(resource => resource.audience) ?? [])
  const unknownAudiences = audiences?.filter(audience => !listedAudiences.has(audience)) ?? []
  return <><div className="spec-detail-list">{Object.entries(fields).map(([name, value]) => <div className="spec-detail-row" key={name}><span>{name}</span><div className="spec-detail-value">{name === 'metadata' ? <pre className="spec-summary-json">{JSON.stringify(value, null, 2)}</pre> : <Typography.Text code>{Array.isArray(value) ? value.join(', ') : String(value)}</Typography.Text>}</div></div>)}</div>
    {showAccess && <div className="spec-resource-preview"><strong>按资源查看最终配置</strong>
      {(resourceCatalog!.audiences ?? []).filter(resource => audiences?.includes(resource.audience)).map(resource => <p key={resource.audience}><Typography.Text code>{resource.audience}</Typography.Text>：{scopes.filter(scope => resource.scopes.includes(scope)).join('、') || '未选 Scope'}</p>)}
      {(resourceCatalog!.global_scopes ?? []).some(scope => scopes.includes(scope)) && <p>通用 Scope：{scopes.filter(scope => resourceCatalog!.global_scopes?.includes(scope)).join('、')}</p>}
      {unknownAudiences.length > 0 && <p>目录外 Audience：{unknownAudiences.join('、')}</p>}
      {unmatched.length > 0 && <p>未归属 Scope：{unmatched.join('、')}</p>}
    </div>}
  </>
}

export function SpecClientCreate() {
  const [form] = Form.useForm<Record<string, unknown>>()
  const navigate = useNavigate()
  const [schema, setSchema] = useState<WritableField[]>([])
  const [resourceCatalog, setResourceCatalog] = useState<ResourceCatalog>()
  const [available, setAvailable] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [preview, setPreview] = useState<Record<string, unknown> | null>(null)
  const [created, setCreated] = useState<{ id: string; secret?: string; publicClient: boolean } | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    void listClients(controller.signal).then(data => {
      setAvailable(Boolean(data.capabilities.client_create?.enabled))
      setSchema(data.writable_schema)
      setResourceCatalog(data.resource_catalog)
    }).catch(cause => { if (!controller.signal.aborted) setError(clientErrorMessage(cause)) }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [])

  function review(values: Record<string, unknown>) {
    const fields: Record<string, unknown> = {}
    for (const field of schema) {
      const raw = values[field.name]
      if (field.name === 'metadata') {
        if (typeof raw === 'string' && raw.trim()) fields.metadata = parseMetadataInput(raw)
        continue
      }
      const value = field.name === 'scope' ? scopeWireValue(raw) : raw
      if (value !== undefined && value !== '' && (!Array.isArray(value) || value.length > 0)) fields[field.name] = value
    }
    setPreview(fields)
  }

  async function save() {
    if (!preview || saving) return
    setSaving(true); setError(null)
    try {
      const result = await createClient(preview)
      form.resetFields(); setPreview(null)
      setCreated({ id: result.client.client_id, secret: result.client_secret, publicClient: preview.token_endpoint_auth_method === 'none' })
    } catch (cause) { setError(clientErrorMessage(cause)); setPreview(null) }
    finally { setSaving(false) }
  }

  return <>
    <PageHeading kicker="Client / 创建" title="创建 Client" description="字段由当前版本的可写 schema 控制，提交前检查摘要。" action={<Link to="/clients"><Button icon={<ArrowLeftOutlined />}>返回列表</Button></Link>} />
    {loading && <Spin aria-label="正在检查写入能力" />}
    {error && <Alert type="error" showIcon title={error} className="spec-inline-alert" role="alert" />}
    {!loading && !available && <Alert type="warning" showIcon title="当前环境未开放 Client 创建" />}
    {!loading && available && <Card className="spec-form-card spec-create-card" title="Client 配置">
      <Form form={form} layout="vertical" requiredMark={false} className="spec-client-form" onFinish={review}><ClientFieldSections schema={schema} resourceCatalog={resourceCatalog} create />
        <div className="spec-create-actions"><p>机密 Client 的 Secret 由服务端安全生成，只在创建成功后展示一次。</p><Button type="primary" htmlType="submit" disabled={saving}>检查创建摘要</Button></div>
      </Form>
    </Card>}
    <Modal title="确认创建 Client" open={preview !== null} onCancel={() => setPreview(null)} onOk={() => void save()} okText="确认创建" confirmLoading={saving} destroyOnHidden>
      <p>请核对将提交到 Hydra 的字段：</p>{preview && <Summary fields={preview} resourceCatalog={resourceCatalog} />}
    </Modal>
    <Modal title="Client 已创建" open={created !== null} closable={false} maskClosable={false} onOk={() => { const id = created?.id; setCreated(null); if (id) navigate(`/clients/${encodeURIComponent(id)}`) }} okText="已保存，关闭" cancelButtonProps={{ style: { display: 'none' } }}>
      <p>Client ID：<Typography.Text code>{created?.id}</Typography.Text></p>
      {created?.secret ? <><Alert type="warning" showIcon title="请立即复制并妥善保管。关闭或刷新后，本应用不再保存 Secret。" /><Input.Password readOnly value={created.secret} autoComplete="off" aria-label="一次性 Client Secret" /></> : <Alert type="info" showIcon title={created?.publicClient ? '公开 Client 不使用 Client Secret。' : 'Hydra 未返回可展示的 Secret，本应用不能恢复它。'} />}
    </Modal>
  </>
}
