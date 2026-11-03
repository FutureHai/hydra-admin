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
  skip_consent: '跳过授权同意页', skip_logout_consent: '跳过登出同意页',
  frontchannel_logout_uri: '前通道登出 URI', frontchannel_logout_session_required: '前通道需要会话标识',
  backchannel_logout_uri: '后通道登出 URI', backchannel_logout_session_required: '后通道需要会话标识',
}

const grantLabels: Record<string, string> = {
  authorization_code: '授权码', client_credentials: 'Client Credentials',
  device_authorization: '设备授权', implicit: 'Implicit', refresh_token: 'Refresh Token',
}
const tokenLabels: Record<string, string> = { access_token: 'Access Token', id_token: 'ID Token', refresh_token: 'Refresh Token' }

export function fieldLabel(name: string) {
  const lifespan = /^(.*?)_grant_(access_token|id_token|refresh_token)_lifespan$/.exec(name)
  if (lifespan) return `${grantLabels[lifespan[1]] ?? lifespan[1]} · ${tokenLabels[lifespan[2]]} 有效期`
  return fieldLabels[name] ?? name.replaceAll('_', ' ')
}

function hydraDurationNanos(value: unknown): bigint | null {
  if (typeof value !== 'string' || value.length > 64 || !/^([0-9]+(ns|us|ms|s|m|h))+$/.test(value)) return null
  const nanos: Record<string, bigint> = { ns: 1n, us: 1000n, ms: 1000000n, s: 1000000000n, m: 60000000000n, h: 3600000000000n }
  let total = 0n
  for (const match of value.matchAll(/([0-9]+)(ns|us|ms|s|m|h)/g)) total += BigInt(match[1]) * nanos[match[2]]
  return total > 0n && total <= 9223372036854775807n ? total : null
}

export function validHydraDuration(value: unknown): boolean { return hydraDurationNanos(value) !== null }
export function sameHydraDuration(first: unknown, second: unknown): boolean {
  const a = hydraDurationNanos(first)
  return a !== null && a === hydraDurationNanos(second)
}

function validLogoutURI(value: unknown): boolean {
  if (typeof value !== 'string') return false
  try {
    const url = new URL(value)
    return (url.protocol === 'https:' || url.protocol === 'http:') && Boolean(url.hostname) && !url.username && !url.password && !url.hash
  } catch { return false }
}

export function parseMetadataInput(value: unknown): unknown {
  if (typeof value !== 'string') throw new Error('请输入有效 JSON')
  return JSON.parse(value) as unknown
}

type FieldControlProps = {
  field: WritableField
  create?: boolean
  id?: string
  value?: string | string[] | boolean
  onChange?: (value: unknown) => void
}

export function FieldControl({ field, create = false, id, value, onChange }: FieldControlProps) {
  if (field.kind === 'select') return <Select id={id} value={value as string | undefined} onChange={onChange} allowClear={!field.required} placeholder={field.name === 'access_token_strategy' ? '沿用实例默认策略' : undefined} options={field.options?.map(item => ({ label: item, value: item }))} />
  if (field.kind === 'boolean') return <Select id={id} value={value as string | undefined} onChange={onChange} allowClear={create} placeholder="使用 Hydra 默认值" options={[{ label: '开启', value: 'true' }, { label: '关闭', value: 'false' }]} />
  if (field.name === 'scope') return <Select id={id} value={value as string[] | undefined} onChange={onChange} mode="tags" tokenSeparators={[' ', ',', '\n']} placeholder="输入 Scope 后按 Enter 添加" />
  if (field.kind === 'array') return <Select id={id} value={value as string[] | undefined} onChange={onChange} mode={field.options?.length ? 'multiple' : 'tags'} tokenSeparators={[',', '\n']} options={field.options?.map(item => ({ label: item, value: item }))} placeholder="输入后按 Enter 添加" />
  if (field.kind === 'json') return <Input.TextArea id={id} value={value as string | undefined} onChange={onChange} rows={5} placeholder={'{"department":"example"}'} spellCheck={false} className="spec-json-input" />
  if (field.kind === 'duration') return <Input id={id} value={value as string | undefined} onChange={onChange} placeholder="例如 1h30m" autoComplete="off" className="spec-mono-input" />
  return <Input id={id} value={value as string | undefined} onChange={onChange} autoComplete="off" className={field.name === 'client_id' ? 'spec-mono-input' : undefined} />
}

export function FieldList({ schema, create = false }: { schema: WritableField[]; create?: boolean }) {
  return <div className="spec-form-grid">{schema.map(field => <Form.Item key={field.name} name={field.name} preserve={field.kind !== 'duration'} label={fieldLabel(field.name)} extra={field.help ?? (field.kind === 'duration' ? '留空使用 Hydra 默认；格式如 1h、30m、1h30m。' : undefined)} className={field.kind === 'json' ? 'spec-json-field' : undefined} rules={field.kind === 'json' ? [{ validator: async (_, value: unknown) => {
    if (value === undefined || typeof value === 'string' && !value.trim()) return
    try { parseMetadataInput(value) } catch { throw new Error('请输入有效 JSON') }
  } }] : field.kind === 'duration' ? [{ validator: async (_, value: unknown) => {
    if (value === undefined || value === '') return
    if (!validHydraDuration(value)) throw new Error('请输入正数有效期，例如 1h30m')
  } }] : field.name === 'frontchannel_logout_uri' || field.name === 'backchannel_logout_uri' ? [{ validator: async (_, value: unknown) => {
    if (value === undefined || value === '') return
    if (!validLogoutURI(value)) throw new Error('请输入完整的 HTTP 或 HTTPS URI，且不能包含账号、密码或片段')
  } }] : create && field.required ? [{ required: true, message: `请填写 ${field.name}` }] : undefined}>
    <FieldControl field={field} create={create} />
  </Form.Item>)}</div>
}

const clientSections = [
  { key: 'identity', title: '基础信息', description: '用于识别和查找这个 Client', fields: ['client_id', 'client_name'] },
  { key: 'authorization', title: '授权与令牌', description: '选择授权流程和 Access Token 的生成方式', fields: ['grant_types', 'response_types', 'token_endpoint_auth_method', 'access_token_strategy', 'scope', 'audience'] },
  { key: 'redirects', title: '回调与来源', description: '配置允许的回调地址和浏览器来源', fields: ['redirect_uris', 'post_logout_redirect_uris', 'allowed_cors_origins'] },
  { key: 'metadata', title: '其他信息', description: '可选的联系人、展示地址和业务数据', fields: ['owner', 'contacts', 'client_uri', 'logo_uri', 'policy_uri', 'tos_uri', 'metadata'] },
  { key: 'advanced', title: '高级授权与登出设置', description: '留空采用 Hydra 默认值。跳过同意页只适合已充分信任的 Client。', fields: ['skip_consent', 'skip_logout_consent', 'frontchannel_logout_uri', 'frontchannel_logout_session_required', 'backchannel_logout_uri', 'backchannel_logout_session_required'] },
  { key: 'lifespans', title: '自定义 Token 有效期', description: '按授权方式设置；留空继承 Hydra 默认。若启用了 Token 定时清理，还需同步核对清理任务的最长有效期。', fields: [
    'authorization_code_grant_access_token_lifespan', 'authorization_code_grant_id_token_lifespan', 'authorization_code_grant_refresh_token_lifespan',
    'client_credentials_grant_access_token_lifespan',
    'device_authorization_grant_access_token_lifespan', 'device_authorization_grant_id_token_lifespan', 'device_authorization_grant_refresh_token_lifespan',
    'implicit_grant_access_token_lifespan', 'implicit_grant_id_token_lifespan',
    'refresh_token_grant_access_token_lifespan', 'refresh_token_grant_id_token_lifespan', 'refresh_token_grant_refresh_token_lifespan',
  ] },
]

export function ClientFieldSections({ schema, create = false, resourceCatalog, openAdvanced = false }: { schema: WritableField[]; create?: boolean; resourceCatalog?: ResourceCatalog; openAdvanced?: boolean }) {
  const selectedGrants = Form.useWatch('grant_types') as string[] | undefined
  const byName = new Map(schema.map(field => [field.name, field]))
  const placed = new Set(clientSections.flatMap(section => section.fields))
  const sections = clientSections.map(section => ({ ...section, schema: section.fields.map(name => byName.get(name)).filter((field): field is WritableField => Boolean(field)) })).filter(section => section.schema.length)
  const remaining = schema.filter(field => !placed.has(field.name))
  if (remaining.length) sections.push({ key: 'other', title: '其他字段', description: '当前版本额外开放的配置', fields: [], schema: remaining })
  const idPrefix = create ? 'spec-create' : 'spec-edit'
  const groupedAccess = catalogEnabled(resourceCatalog) && byName.has('scope') && byName.has('audience')
  return <>{sections.map(section => section.key === 'advanced' || section.key === 'lifespans' ? <details className="spec-client-advanced" key={section.key} open={section.key === 'advanced' && openAdvanced ? true : undefined}>
    <summary>{section.title}</summary><p>{section.description}</p>
    {section.key === 'lifespans' && create && byName.has('grant_types') && !selectedGrants?.length && <p>请先选择 Grant types，再设置对应的有效期。</p>}
    <FieldList schema={section.key === 'lifespans' && create && byName.has('grant_types') ? section.schema.filter(field => {
      const grant = field.name.split('_grant_')[0]
      return selectedGrants?.includes(grant === 'device_authorization' ? 'urn:ietf:params:oauth:grant-type:device_code' : grant)
    }) : section.schema} create={create} />
  </details> : <section className="spec-client-section" key={section.key} aria-labelledby={`${idPrefix}-${section.key}`}>
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
  return <><div className="spec-detail-list">{Object.entries(fields).map(([name, value]) => <div className="spec-detail-row" key={name}><span>{fieldLabel(name)}</span><div className="spec-detail-value">{name === 'metadata' ? <pre className="spec-summary-json">{JSON.stringify(value, null, 2)}</pre> : <Typography.Text code>{value === null && name.endsWith('_lifespan') ? '恢复 Hydra 默认值' : Array.isArray(value) ? value.join(', ') : typeof value === 'boolean' ? value ? '开启' : '关闭' : String(value)}</Typography.Text>}</div></div>)}</div>
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
      const value = field.name === 'scope' ? scopeWireValue(raw) : field.kind === 'boolean' && raw !== undefined ? raw === 'true' : raw
      if (value !== undefined && value !== '' && (!Array.isArray(value) || value.length > 0)) fields[field.name] = value
    }
    for (const channel of ['frontchannel', 'backchannel']) {
      if (fields[`${channel}_logout_session_required`] === true && !fields[`${channel}_logout_uri`]) {
        form.setFields([{ name: `${channel}_logout_uri`, errors: ['启用会话标识时需要填写对应的登出 URI'] }])
        return
      }
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
      <Form form={form} layout="vertical" requiredMark={false} className="spec-client-form" onFinish={review} onFinishFailed={({ errorFields }) => {
        const name = errorFields[0]?.name[0]
        if (typeof name !== 'string') return
        const control = document.getElementById(name)
        const details = control?.closest('details')
        if (details) details.open = true
        control?.focus()
      }}><ClientFieldSections schema={schema} resourceCatalog={resourceCatalog} create />
        <div className="spec-create-actions"><p>机密 Client 的 Secret 由服务端安全生成，只在创建成功后展示一次。</p><Button type="primary" htmlType="submit" disabled={saving}>检查创建摘要</Button></div>
      </Form>
    </Card>}
    <Modal title="确认创建 Client" open={preview !== null} onCancel={() => setPreview(null)} onOk={() => void save()} okText="确认创建" confirmLoading={saving} destroyOnHidden>
      <p>请核对将提交到 Hydra 的字段：</p>
      {preview?.skip_consent === true && <Alert type="warning" showIcon title="此 Client 将跳过用户授权同意页，请确认它是可信应用。" className="spec-inline-alert" />}
      {preview && Object.keys(preview).some(name => name.endsWith('_lifespan')) && <Alert type="info" showIcon title="若已启用 Hydra Token 定时清理，请同步核对清理任务的最长有效期。" className="spec-inline-alert" />}
      {preview && <Summary fields={preview} resourceCatalog={resourceCatalog} />}
    </Modal>
    <Modal title="Client 已创建" open={created !== null} closable={false} maskClosable={false} onOk={() => { const id = created?.id; setCreated(null); if (id) navigate(`/clients/${encodeURIComponent(id)}`) }} okText="已保存，关闭" cancelButtonProps={{ style: { display: 'none' } }}>
      <p>Client ID：<Typography.Text code>{created?.id}</Typography.Text></p>
      {created?.secret ? <><Alert type="warning" showIcon title="请立即复制并妥善保管。关闭或刷新后，本应用不再保存 Secret。" /><Input.Password readOnly value={created.secret} autoComplete="off" aria-label="一次性 Client Secret" /></> : <Alert type="info" showIcon title={created?.publicClient ? '公开 Client 不使用 Client Secret。' : 'Hydra 未返回可展示的 Secret，本应用不能恢复它。'} />}
    </Modal>
  </>
}
