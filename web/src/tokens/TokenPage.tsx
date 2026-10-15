import { useState } from 'react'
import axios from 'axios'
import { Alert, Button, Form, Input, Modal, Tabs, Typography } from 'antd'
import { api } from '../auth/api'
import { useHydraStatus } from '../clients/useHydraStatus'
import { Machine, ReasonBlock } from '../spec/SpecApp'
import { PageHeading } from '../spec/SpecOverview'

type TokenInfo = { active: boolean; client_id?: string; sub?: string; scope?: string; aud?: string[]; exp?: number; iat?: number; iss?: string }
type RevokeDraft = { token: string; client_id: string; client_secret: string; confirm_client_id: string }
const visibleTokenFields = ['active', 'client_id', 'sub', 'scope', 'aud', 'exp', 'iat', 'iss'] as const

function tokenError(error: unknown): string {
  const code = axios.isAxiosError(error) ? (error.response?.data as { code?: unknown } | undefined)?.code : undefined
  switch (code) {
    case 'unsupported_version': return 'Hydra 运行版本不受支持。'
    case 'unavailable': case 'revoke_not_configured': return '当前环境未开放此 Token 能力。'
    case 'token_owner_unconfirmed': return 'Token 未激活或无法确认属于所填 Client；没有发送撤销请求。'
    case 'client_auth_method_unsupported': return '该 Client 不使用 client_secret_basic，当前适配器无法为它执行撤销。'
    case 'hydra_upstream_denied': return 'Hydra 拒绝访问或 Client 认证失败。'
    case 'validation_failed': case 'confirmation_required': return '请检查输入和 Client ID 确认。'
    case 'audit_write_failed': return 'Hydra 操作可能已成功但审计写入失败。请核验 Token 状态，勿直接重复提交。'
    default: return '操作结果无法确认。请先核验状态，勿直接重复提交。'
  }
}

function disabledReason(reason?: string): string {
  switch (reason) {
    case 'unsupported_version': return '运行版本与固定规范不匹配，Token 操作已关闭。'
    case 'public_revoke_not_configured': return '未启用 Public revoke 或未配置 Public URL。'
    case 'client_list_probe_failed': return 'Admin Client 只读探测未通过。'
    case 'health_not_ready': return 'Hydra 健康检查未就绪。'
    default: return '当前版本或部署未通过此能力的适配检查。'
  }
}

export function TokenPage() {
  const { status, error: statusError } = useHydraStatus()
  const [introspectForm] = Form.useForm<{ token: string }>()
  const [revokeForm] = Form.useForm<RevokeDraft>()
  const [info, setInfo] = useState<TokenInfo | null>(null)
  const [introspectError, setIntrospectError] = useState<string | null>(null)
  const [revokeError, setRevokeError] = useState<string | null>(null)
  const [accepted, setAccepted] = useState(false)
  const [busy, setBusy] = useState(false)
  const [draft, setDraft] = useState<RevokeDraft | null>(null)

  async function introspect(values: { token: string }) {
    setInfo(null); setIntrospectError(null); setBusy(true)
    introspectForm.resetFields()
    try { const response = await api.post<TokenInfo>('/tokens/introspect', { token: values.token }); setInfo(response.data) }
    catch (cause) { setIntrospectError(tokenError(cause)) }
    finally { values.token = ''; setBusy(false) }
  }

  function reviewRevoke(values: RevokeDraft) {
    setRevokeError(null); setAccepted(false)
    if (values.client_id !== values.confirm_client_id) { setRevokeError('两次输入的 Client ID 不一致。'); return }
    setDraft({ ...values }); revokeForm.resetFields()
    values.token = ''; values.client_secret = ''
  }

  async function revoke() {
    if (!draft || busy) return
    const current = draft
    setDraft(null); setBusy(true); setRevokeError(null)
    try { await api.post('/tokens/revoke', current); setAccepted(true); setInfo(null) }
    catch (cause) { setRevokeError(tokenError(cause)) }
    finally { current.token = ''; current.client_secret = ''; setBusy(false) }
  }

  const introspectEnabled = status?.capabilities.token_introspect?.enabled === true
  const revokeEnabled = status?.capabilities.token_revoke?.enabled === true
  return <>
    <PageHeading kicker="工具 / Token" title="Token 工具" description="仅处理本次粘贴的 Token；结果不包含原 Token。" />
    {statusError && <Alert type="error" showIcon title="无法读取能力报告" className="spec-inline-alert" />}
    <section className="spec-panel spec-token-panel"><Tabs items={[
      { key: 'introspect', label: 'Introspect', children: <div className="spec-tool-content"><h2>Token 内省</h2>
        {!introspectEnabled ? <ReasonBlock title="内省不可用">{disabledReason(status?.capabilities.token_introspect?.reason)}</ReasonBlock> : <>
          <Form form={introspectForm} layout="vertical" onFinish={values => void introspect(values)}><Form.Item name="token" label="Access 或 Refresh Token" rules={[{ required: true, message: '请输入 Token' }]}><Input.Password autoComplete="off" /></Form.Item><Button type="primary" htmlType="submit" loading={busy}>执行内省</Button></Form>
          {introspectError && <Alert type="error" showIcon title={introspectError} role="alert" />}
          {info && <div className="spec-detail-list" role="status">{visibleTokenFields.filter(key => info[key] !== undefined).map(key => { const value = info[key]; return <div className="spec-detail-row" key={key}><span>{key}</span><div className="spec-detail-value"><Machine>{Array.isArray(value) ? value.join(', ') : String(value)}</Machine></div></div> })}</div>}
        </>}</div> },
      { key: 'revoke', label: 'Revoke', children: <div className="spec-tool-content"><h2>Token 撤销</h2>
        {!revokeEnabled ? <ReasonBlock tone="warning" title="撤销不可用">{disabledReason(status?.capabilities.token_revoke?.reason)}</ReasonBlock> : <>
          <Alert type="warning" showIcon title="仅 Token 所属 Client 可撤销；成功表示 Hydra 接受请求，资源服务器可能不会立即感知。" />
          <Form form={revokeForm} layout="vertical" onFinish={reviewRevoke}>
            <Form.Item name="token" label="Access 或 Refresh Token" rules={[{ required: true }]}><Input.Password autoComplete="off" /></Form.Item>
            <Form.Item name="client_id" label="Token 所属 Client ID" rules={[{ required: true }]}><Input autoComplete="off" /></Form.Item>
            <Form.Item name="client_secret" label="本次 Client Secret" rules={[{ required: true }]}><Input.Password autoComplete="off" /></Form.Item>
            <Form.Item name="confirm_client_id" label="再次键入 Client ID" rules={[{ required: true }]}><Input autoComplete="off" /></Form.Item>
            <Button danger htmlType="submit" loading={busy}>检查撤销确认</Button>
          </Form>
          {revokeError && <Alert type="error" showIcon title={revokeError} role="alert" />}
          {accepted && <Alert type="success" showIcon title="Hydra 已接受撤销请求；请按资源服务器验证策略确认实际失效。" role="status" />}
        </>}</div> },
    ]} /></section>
    <Modal title="二次确认撤销" open={draft !== null} onCancel={() => { if (draft) { draft.token=''; draft.client_secret='' }; setDraft(null) }} onOk={() => void revoke()} okText="确认撤销" okButtonProps={{ danger: true }} confirmLoading={busy} destroyOnHidden>
      <p>仅使用本次输入的凭据，向 Public API 提交撤销请求。操作不会自动重试。</p>
      <p>Client ID：<Typography.Text code>{draft?.client_id}</Typography.Text></p>
    </Modal>
  </>
}
