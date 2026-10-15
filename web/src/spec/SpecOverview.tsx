import type { ReactNode } from 'react'
import { Alert, Table, Tooltip } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ApiOutlined, CloudServerOutlined, DatabaseOutlined, FileProtectOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { Machine, ReasonBlock, StatusBadge } from './SpecApp'
import { useHydraStatus, type HydraStatus } from '../clients/useHydraStatus'

export function PageHeading({ title, description, action }: { kicker?: string; title: string; description: string; action?: ReactNode }) {
  return <div className="spec-page-heading"><div><h1>{title}</h1><p>{description}</p></div>{action && <div className="spec-page-action">{action}</div>}</div>
}

type CapabilityRow = { key: string; name: string; enabled: boolean; reason: string }
type ReplicaRow = NonNullable<HydraStatus['replicas']>[number]
const capabilityNames: { key: string; name: string }[] = [
  { key: 'client_list', name: 'Client 列表' },
  { key: 'client_detail', name: 'Client 详情' },
  { key: 'client_create', name: '创建 Client' },
  { key: 'client_update', name: '更新 Client' },
  { key: 'client_delete', name: '删除 Client' },
  { key: 'secret_rotate', name: 'Secret 轮换' },
  { key: 'token_introspect', name: 'Token introspection' },
  { key: 'token_revoke', name: 'Token revoke' },
  { key: 'scopes', name: 'Scope 聚合' },
  { key: 'audiences', name: 'Audience 聚合' },
  { key: 'audit_read', name: '审计读取' },
]

export function reasonLabel(reason: string | undefined): string {
  switch (reason) {
    case 'unsupported_version': return '运行版本不匹配'
    case 'not_implemented_in_this_phase': return '当前阶段尚未实现'
    case 'client_list_probe_failed': return 'Client 列表只读探测未通过'
    case 'rotation_operation_absent': return '固定规范没有独立 Secret 轮换端点'
    case 'public_revoke_not_configured': return 'Public revoke 未启用或未配置 URL'
    case 'upstream_access_denied': return 'Hydra 上游拒绝访问'
    case 'health_not_ready': return 'Hydra 健康检查未就绪'
    case 'unavailable': return 'Hydra 暂不可达'
    case 'unexpected_response': return 'Hydra 响应不符合规范'
    case 'not_verified_in_this_deployment': return '当前部署尚未完成写入验收'
    default: return '当前能力不可用'
  }
}

const capabilityColumns: ColumnsType<CapabilityRow> = [
  { title: '能力', dataIndex: 'name', key: 'name', width: 220, render: (name: string) => <span className="spec-table-name">{name}</span> },
  { title: '状态', key: 'status', width: 120, render: (_, row) => <StatusBadge tone={row.enabled ? 'success' : 'unknown'}>{row.enabled ? '可用' : '不可用'}</StatusBadge> },
  { title: '说明', dataIndex: 'reason', key: 'reason', width: 380, render: (reason: string) => <span className="spec-muted">{reason}</span> },
]

const replicaColumns: ColumnsType<ReplicaRow> = [
  { title: '副本', dataIndex: 'name', key: 'name', width: 180, render: (name: string) => <Machine>{name}</Machine> },
  { title: '运行版本', key: 'version', width: 190, render: (_, row) => <Machine>{row.observed_version ?? '—'}</Machine> },
  { title: '健康状态', key: 'health', width: 145, render: (_, row) => <StatusBadge tone={row.health_probe === 'ok' ? 'success' : 'unknown'}>{row.health_probe === 'ok' ? '已就绪' : '不可用'}</StatusBadge> },
  { title: 'Client 探测', key: 'client', width: 145, render: (_, row) => <StatusBadge tone={row.client_list_probe === 'ok' ? 'success' : 'unknown'}>{row.client_list_probe === 'ok' ? '通过' : '未通过'}</StatusBadge> },
  { title: '可接收操作', key: 'eligible', width: 160, render: (_, row) => <StatusBadge tone={row.eligible ? 'success' : 'unknown'}>{row.eligible ? '可用' : '已排除'}</StatusBadge> },
]

function Metric({ icon, name, value, detail, tone = 'unknown' }: { icon: ReactNode; name: string; value: string; detail: string; tone?: 'success' | 'unknown' | 'warning' }) {
  return <div className="spec-metric"><div className="spec-metric-top"><span>{name}</span><span className="spec-metric-icon">{icon}</span></div><StatusBadge tone={tone}>{value}</StatusBadge><p>{detail}</p></div>
}

function capabilityRows(status: HydraStatus | null): CapabilityRow[] {
  return capabilityNames.map(item => {
    if (!status) return { ...item, enabled: false, reason: '正在读取能力报告' }
    const capability = status.capabilities[item.key]
    if (!capability) return { ...item, enabled: false, reason: '能力元数据缺失' }
    return { ...item, enabled: capability.enabled, reason: capability.enabled && !capability.reason ? '' : reasonLabel(capability.reason) }
  })
}

function StatusMetrics({ status }: { status: HydraStatus | null }) {
  const replicas = status?.replicas ?? []
  const ready = replicas.filter(replica => replica.eligible).length
  return <div className="spec-metric-grid">
    <Metric icon={<CloudServerOutlined />} name="可用副本" value={status ? (replicas.length ? `${ready}/${replicas.length} 副本可用` : status.health_probe === 'ok' ? '已就绪' : '未就绪') : '读取中'} detail="健康且版本匹配的 Admin 副本" tone={replicas.length ? ready > 0 ? 'success' : 'unknown' : status?.health_probe === 'ok' ? 'success' : 'unknown'} />
    <Metric icon={<DatabaseOutlined />} name="运行版本" value={status?.observed_version ?? '—'} detail={`内置适配版本 ${status?.supported_version ?? '—'}`} tone={status?.status === 'supported' ? 'success' : 'unknown'} />
    <Metric icon={<ApiOutlined />} name="Admin Client 读取" value={status ? (status.capabilities.client_list?.enabled ? '只读可用' : '不可用') : '读取中'} detail="读取前会再次检查实际版本" tone={status?.capabilities.client_list?.enabled ? 'success' : 'unknown'} />
    <Metric icon={<SafetyCertificateOutlined />} name="Public API 探测" value="未检测" detail="启动检查不对 Public API 发起写请求" />
  </div>
}

export function SpecOverview() {
  const { status, error } = useHydraStatus()
  return <>
    <PageHeading kicker="工作空间 / 概览" title="控制台概览" description="查看 Hydra 连接、运行版本和管理能力。" />
    {error && <Alert type="error" showIcon className="spec-inline-alert" title="无法读取状态报告" />}
    <div className="spec-section-intro"><span className="spec-section-title">运行概况</span></div>
    <StatusMetrics status={status} />
    <section className="spec-panel spec-capabilities" aria-labelledby="spec-capabilities-heading">
      <div className="spec-panel-head"><div><h2 id="spec-capabilities-heading">能力矩阵</h2><p>可用性由后端能力报告确认；未开放项显示原因。</p></div><span className="spec-panel-count">{capabilityNames.length} 项能力</span></div>
      <div className="spec-table-scroll"><Table<CapabilityRow> size="small" columns={capabilityColumns} dataSource={capabilityRows(status)} pagination={false} rowKey="key" scroll={{ x: 760 }} /></div>
    </section>
    <div className="spec-bottom-grid">
      <section className="spec-panel" aria-labelledby="spec-summary-heading"><div className="spec-panel-head"><div><h2 id="spec-summary-heading">规范摘要</h2><p>当前运行实例与固定规范</p></div><FileProtectOutlined className="spec-panel-head-icon" /></div>
        <div className="spec-detail-row"><span>运行版本</span><Machine>{status?.observed_version ?? '—'}</Machine></div>
        <div className="spec-detail-row"><span>规范 SHA-256</span><Tooltip title={status?.spec_sha256 ?? '正在读取'}><Machine>{status?.spec_sha256 ?? '—'}</Machine></Tooltip></div>
        <ReasonBlock title={status?.status === 'supported' ? '规范已匹配' : '能力关闭'}>{status?.diagnostic ?? (status?.status === 'supported' ? 'Client 和 Token 操作按能力矩阵开放；Secret 轮换没有独立端点。' : '等待或无法取得匹配的运行版本。')}</ReasonBlock>
      </section>
      <section className="spec-panel" aria-labelledby="spec-audit-heading"><div className="spec-panel-head"><div><h2 id="spec-audit-heading">审计记录</h2><p>查询实际管理操作</p></div><Link to="/audit" className="spec-text-link">查看审计</Link></div><p className="spec-muted">在审计页按时间、动作与 Client ID 筛选服务端审计记录。</p></section>
    </div>
  </>
}

export function SpecSystem() {
  const { status, error } = useHydraStatus()
  return <>
    <PageHeading kicker="工作空间 / 系统状态" title="系统状态" description="查看实例连接、规范匹配与当前能力。" />
    {error && <Alert type="error" showIcon className="spec-inline-alert" title="无法读取状态报告" />}
    <StatusMetrics status={status} />
    <section className="spec-panel spec-system-panel"><div className="spec-panel-head"><div><h2>能力与规范</h2><p>Client 与 Token 能力均以当前矩阵为准。</p></div></div>
      <div className="spec-detail-row"><span>运行版本</span><Machine>{status?.observed_version ?? '—'}</Machine></div>
      <div className="spec-detail-row"><span>规范 SHA-256</span><Machine>{status?.spec_sha256 ?? '—'}</Machine></div>
      <div className="spec-detail-row"><span>Client 列表探测</span><Machine>{status?.client_list_probe ?? '—'}</Machine></div>
      <div className="spec-detail-row"><span>生产写入验收</span><Machine>{status?.write_acceptance === 'not_verified_in_this_deployment' ? '当前部署未验证' : status?.write_acceptance ?? '—'}</Machine></div>
      <ReasonBlock title={status?.status === 'supported' ? '版本已匹配' : '能力关闭'}>{status?.diagnostic ?? 'Client 读取取决于版本与只读探测结果。'}</ReasonBlock>
    </section>
    <section className="spec-panel spec-capabilities" aria-labelledby="spec-replicas-heading"><div className="spec-panel-head"><div><h2 id="spec-replicas-heading">Hydra 副本</h2><p>每 15 秒读取状态；业务请求只选择健康且版本匹配的副本。</p></div><span className="spec-panel-count">{status?.replicas?.length ?? 0} 个副本</span></div><div className="spec-table-scroll"><Table<ReplicaRow> size="small" columns={replicaColumns} dataSource={status?.replicas ?? []} pagination={false} rowKey="name" scroll={{ x: 820 }} /></div></section>
    <section className="spec-panel spec-capabilities" aria-labelledby="spec-system-capabilities-heading"><div className="spec-panel-head"><div><h2 id="spec-system-capabilities-heading">能力矩阵与禁用原因</h2><p>当前进程内的版本适配与只读探测结果；开放能力不代表当前部署已完成生产写入验收。</p></div><span className="spec-panel-count">{capabilityNames.length} 项能力</span></div><div className="spec-table-scroll"><Table<CapabilityRow> size="small" columns={capabilityColumns} dataSource={capabilityRows(status)} pagination={false} rowKey="key" scroll={{ x: 760 }} /></div></section>
  </>
}
