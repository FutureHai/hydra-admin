import axios from 'axios'
import { api } from '../auth/api'

export type ClientRead = { client_id: string; fields: Record<string, unknown> }
export type WritableField = { name: string; kind: 'text' | 'array' | 'select' | 'json'; required?: boolean; options?: string[]; help?: string }
export type ResourceCatalog = { global_scopes?: string[]; audiences?: { audience: string; scopes: string[] }[] }
export type Capabilities = Record<string, { enabled: boolean; reason?: string }>
export type ClientListResponse = { items: ClientRead[]; page_size: number; next_page_token?: string; pagination_supported: boolean; capabilities: Capabilities; writable_schema: WritableField[]; resource_catalog?: ResourceCatalog }
export type ClientDetailResponse = { client: ClientRead; revision: string; editable_fields: string[]; capabilities: Capabilities; writable_schema: WritableField[]; resource_catalog?: ResourceCatalog }

export async function listClients(signal?: AbortSignal, pageToken?: string): Promise<ClientListResponse> {
  const response = await api.get<ClientListResponse>('/clients', { signal, params: pageToken ? { page_token: pageToken } : undefined })
  return response.data
}

export async function getClient(clientId: string, signal?: AbortSignal): Promise<ClientDetailResponse> {
  const response = await api.get<ClientDetailResponse>(`/clients/${encodeURIComponent(clientId)}`, { signal })
  return response.data
}

export async function createClient(fields: Record<string, unknown>): Promise<{ client: ClientRead; client_secret?: string }> {
  const response = await api.post('/clients', { fields })
  return response.data
}

export async function patchClient(clientId: string, baseRevision: string, changes: Record<string, unknown>): Promise<ClientDetailResponse> {
  const response = await api.patch(`/clients/${encodeURIComponent(clientId)}`, { base_revision: baseRevision, changes })
  return response.data
}

export async function deleteClient(clientId: string): Promise<void> {
  await api.delete(`/clients/${encodeURIComponent(clientId)}`, { headers: { 'X-Confirm-Client-ID': clientId } })
}

export function clientErrorMessage(error: unknown): string {
  const code = axios.isAxiosError(error) ? (error.response?.data as { code?: unknown } | undefined)?.code : undefined
  switch (code) {
    case 'unsupported_version': return 'Hydra 运行版本与内置适配器不匹配，Client 读取已关闭。'
    case 'unavailable': return '当前环境的 Client 能力不可用。'
    case 'hydra_timeout': return 'Hydra 请求超时，写入结果可能未确认；请先按 Client ID 核验，勿直接重复提交。'
    case 'hydra_conflict': return 'Hydra 返回冲突，请核验当前 Client 状态。'
    case 'hydra_upstream_denied': return 'Hydra 上游拒绝访问，当前部署可能需要额外认证。'
    case 'client_not_found': return '找不到该 Client。'
    case 'invalid_client_id': return 'Client ID 无效。'
    case 'invalid_page_token': return '分页游标无效，请刷新列表后重试。'
    case 'client_changed': return 'Client 已被其他操作修改，请刷新详情后重新编辑。'
    case 'hydra_validation_failed': return 'Hydra 拒绝了部分字段，请核对输入。'
    case 'validation_failed': return '字段格式或取值无效。'
    case 'resource_assignment_invalid': return 'Scope 或 Audience 不符合资源目录；请检查所属资源及未归属值。'
    case 'no_changes': return '没有可提交的修改。'
    case 'confirmation_required': return '请输入精确的 Client ID 确认删除。'
    case 'audit_write_failed': return 'Hydra 操作可能已成功，但审计写入失败。请按 Client ID 核验，勿直接重复提交。'
    case 'hydra_bad_gateway':
    case 'hydra_bad_response': return 'Hydra 操作结果未能确认，请按 Client ID 核验后再决定下一步。'
    default: return '无法读取 Client 数据，请稍后重试。'
  }
}

export function textField(client: ClientRead, name: string): string | undefined {
  const value = client.fields[name]
  return typeof value === 'string' && value.trim() !== '' ? value : undefined
}

export function textArrayField(client: ClientRead, name: string): string[] | undefined {
  const value = client.fields[name]
  return Array.isArray(value) && value.every(item => typeof item === 'string') ? value as string[] : undefined
}
