import { useEffect, useState } from 'react'
import { Alert, Button, DatePicker, Empty, Input, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../auth/api'
import { clientErrorMessage } from '../clients/api'
import { useHydraStatus } from '../clients/useHydraStatus'
import { Machine, ReasonBlock, StatusBadge } from './SpecApp'
import { PageHeading, reasonLabel } from './SpecOverview'

type UsageRow = { value: string; client_count: number; client_ids: string[] }
type CollectionResponse = {
  items: UsageRow[]; source_client_count: number; complete: false; notice: string
  capabilities: Record<string, { enabled: boolean; reason?: string }>
}
const usageColumns: ColumnsType<UsageRow> = [
  { title: '值', dataIndex: 'value', render: (value: string) => <Machine>{value}</Machine> },
  { title: '当前可读 Client 数', dataIndex: 'client_count', width: 170 },
  { title: 'Client ID', dataIndex: 'client_ids', render: (ids: string[]) => ids.map((id, index) => <span key={id}><Machine>{id}</Machine>{index < ids.length - 1 ? '、' : ''}</span>) },
]

export function SpecCollection({ kind }: { kind: 'scope' | 'audience' }) {
  const { status, error: statusError } = useHydraStatus()
  const capabilityKey = kind === 'scope' ? 'scopes' : 'audiences'
  const enabled = status?.capabilities[capabilityKey]?.enabled === true
  const name = kind === 'scope' ? 'Scope' : 'Audience'
  const [data, setData] = useState<CollectionResponse | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    setLoading(true); setError(''); setData(null)
    void api.get<CollectionResponse>(`/${capabilityKey}`, { signal: controller.signal }).then(response => setData(response.data)).catch(cause => {
      if (!controller.signal.aborted) setError(clientErrorMessage(cause))
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [capabilityKey, enabled, reload])
  return <>
    <PageHeading kicker={`${name} / 使用视图`} title={`${name} 使用情况`} description={`按当前可读 Client 聚合 ${name}，仅提供只读视图。`} action={enabled && <Button onClick={() => setReload(value => value + 1)} loading={loading}>刷新</Button>} />
    <Alert type="warning" showIcon className="spec-inline-alert" title="来自当前可读 Client、可能不完整" description="仅汇总 Hydra 返回的首批最多 100 个 Client；不代表实例中的全部配置。" />
    {statusError && <Alert type="error" showIcon className="spec-inline-alert" title="无法读取能力报告" />}
    {status && !enabled && <ReasonBlock title="聚合不可用">{reasonLabel(status.capabilities[capabilityKey]?.reason)}</ReasonBlock>}
    {error && <Alert type="error" showIcon className="spec-inline-alert" title={error} role="alert" />}
    {enabled && <section className="spec-panel spec-collection-panel"><div className="spec-panel-head"><div><h2>{name} 聚合</h2><p>仅汇总已读取的 Client，不提供独立创建入口</p></div><StatusBadge tone="warning">可能不完整</StatusBadge></div>
      <div className="spec-table-scroll"><Table<UsageRow> size="small" columns={usageColumns} dataSource={data?.items ?? []} rowKey="value" loading={loading} pagination={{ pageSize: 10, hideOnSinglePage: true }} scroll={{ x: 760 }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={error ? '读取失败' : '当前可读 Client 中没有该配置'} /> }} /></div>
      <div className="spec-table-footer">已读取 {data?.source_client_count ?? '—'} 个 Client；列表仅聚合这批记录。</div>
    </section>}
  </>
}

type AuditEvent = {
  timestamp: string; request_id?: string; actor?: string; source_ip?: string; action: string
  target_type?: string; target_id?: string; outcome: string; hydra_status?: number; changed_fields?: string[]
}
type AuditPage = { items: AuditEvent[]; limit: number; offset: number; has_more: boolean; scan_truncated: boolean; parse_errors: number }
const auditColumns: ColumnsType<AuditEvent> = [
  { title: '时间', dataIndex: 'timestamp', width: 190, render: (value: string) => <Machine>{new Date(value).toLocaleString('zh-CN')}</Machine> },
  { title: '动作', dataIndex: 'action', width: 170 },
  { title: '目标 Client ID', dataIndex: 'target_id', width: 200, render: (value?: string) => <Machine>{value || '—'}</Machine> },
  { title: '结果', dataIndex: 'outcome', width: 110 },
  { title: 'request_id', dataIndex: 'request_id', width: 220, render: (value?: string) => <Machine>{value || '—'}</Machine> },
]
type AuditFilters = { from?: string; to?: string; action?: string; client_id?: string }
export function SpecAudit() {
  const { status, error: statusError } = useHydraStatus()
  const enabled = status?.capabilities.audit_read?.enabled === true
  const [range, setRange] = useState<[string, string] | null>(null)
  const [action, setAction] = useState('')
  const [clientID, setClientID] = useState('')
  const [filters, setFilters] = useState<AuditFilters>({})
  const [offset, setOffset] = useState(0)
  const [reload, setReload] = useState(0)
  const [page, setPage] = useState<AuditPage | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    setLoading(true); setError('')
    void api.get<AuditPage>('/audit', { params: { ...filters, limit: 50, offset }, signal: controller.signal }).then(response => setPage(response.data)).catch(() => {
      if (!controller.signal.aborted) setError('审计记录读取失败，请检查筛选条件后重试。')
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [enabled, filters, offset, reload])
  function applyFilters() {
    setOffset(0)
    setFilters({ ...(range ? { from: range[0], to: range[1] } : {}), ...(action.trim() ? { action: action.trim() } : {}), ...(clientID.trim() ? { client_id: clientID.trim() } : {}) })
  }
  return <>
    <PageHeading kicker="安全 / 审计" title="审计记录" description="从服务端配置的审计日志及轮转文件查询；按时间、动作与 Client ID 筛选。" />
    {statusError && <Alert type="error" showIcon title="无法读取能力报告" className="spec-inline-alert" />}
    {status && !enabled && <ReasonBlock title="审计读取不可用">{reasonLabel(status.capabilities.audit_read?.reason)}</ReasonBlock>}
    {enabled && <section className="spec-panel"><div className="spec-table-toolbar"><div><strong>操作记录</strong><span>只读 · 最近记录优先</span></div><div className="spec-filter-group"><DatePicker.RangePicker aria-label="按时间筛选" showTime onChange={dates => setRange(dates?.[0] && dates?.[1] ? [dates[0].toISOString(), dates[1].toISOString()] : null)} /><Input aria-label="按动作筛选" placeholder="动作，如 client.update" value={action} onChange={event => setAction(event.target.value)} /><Input aria-label="按 Client ID 筛选" placeholder="Client ID" value={clientID} onChange={event => setClientID(event.target.value)} /><Button onClick={applyFilters}>查询</Button></div></div>
      {error && <Alert type="error" showIcon title={error} className="spec-inline-alert" role="alert" />}
      {page?.scan_truncated && <Alert type="warning" showIcon title="已达到审计扫描上限，结果可能不完整" className="spec-inline-alert" />}
      {!!page?.parse_errors && <Alert type="warning" showIcon title={`已跳过 ${page.parse_errors} 条格式错误的审计记录`} className="spec-inline-alert" />}
      <div className="spec-table-scroll"><Table<AuditEvent> size="small" columns={auditColumns} dataSource={page?.items ?? []} rowKey={(_, index) => `${page?.offset ?? 0}-${index}`} loading={loading} pagination={false} scroll={{ x: 900 }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={error ? '读取失败' : '当前筛选条件下没有审计记录'} /> }} /></div>
      <div className="spec-table-footer">仅查询固定审计文件及轮转归档，每次最多 50 条。<Button size="small" disabled={offset === 0 || loading} onClick={() => setOffset(value => Math.max(0, value - 50))}>上一页</Button> <Button size="small" disabled={!page?.has_more || loading} onClick={() => setOffset(value => value + 50)}>下一页</Button> <Button size="small" onClick={() => setReload(value => value + 1)} loading={loading}>刷新</Button></div>
    </section>}
  </>
}
