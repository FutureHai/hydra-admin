import { Alert, Form, Select } from 'antd'
import type { ResourceCatalog } from './api'
import { scopeTokens, unmatchedScopes } from './resourceAccess'

export function ResourceAccessFields({ catalog }: { catalog: ResourceCatalog }) {
  const form = Form.useFormInstance<Record<string, unknown>>()
  const audiences = (Form.useWatch('audience', form) as string[] | undefined) ?? []
  const scopes = scopeTokens(Form.useWatch('scope', form))
  const resources = catalog.audiences ?? []
  const globalScopes = catalog.global_scopes ?? []
  const catalogAudiences = new Set(resources.map(resource => resource.audience))
  const unknownAudiences = audiences.filter(value => !catalogAudiences.has(value))
  const unmatched = unmatchedScopes(scopes, audiences, catalog)

  function setGroupScope(options: string[], selected: string[]) {
    const owned = new Set(options)
    const latestScopes = scopeTokens(form.getFieldValue('scope'))
    form.setFieldValue('scope', [...latestScopes.filter(scope => !owned.has(scope)), ...selected])
  }

  function setAudiences(selected: string[]) {
    const latestScopes = scopeTokens(form.getFieldValue('scope'))
    const removed = resources.filter(resource => audiences.includes(resource.audience) && !selected.includes(resource.audience))
    const removedScopes = new Set(removed.flatMap(resource => resource.scopes))
    form.setFieldsValue({ audience: selected, scope: latestScopes.filter(scope => !removedScopes.has(scope)) })
  }

  return <div className="spec-resource-access">
    <p className="spec-resource-intro">先选择目标资源，再选择该资源可请求的 Scope。Hydra 会分别保存 Audience 和 Scope 列表；资源服务仍需验证令牌权限。</p>
    <Form.Item name="audience" label="Audience（目标资源）" extra="可选择多个资源；每个资源下可配置多个 Scope。">
      <Select mode="multiple" options={[...resources.map(resource => ({ label: resource.audience, value: resource.audience })), ...unknownAudiences.map(value => ({ label: `${value}（目录外）`, value }))]} onChange={setAudiences} placeholder="选择目标资源" />
    </Form.Item>
    <Form.Item name="scope" hidden><Select mode="multiple" /></Form.Item>
    <div className="spec-resource-grid">
      {resources.filter(resource => audiences.includes(resource.audience)).map(resource => <div className="spec-resource-card" key={resource.audience}>
        <strong title={resource.audience}>{resource.audience}</strong>
        <label htmlFor={`scope-${resources.indexOf(resource)}`}>该资源的 Scope</label>
        <Select id={`scope-${resources.indexOf(resource)}`} aria-label={`${resource.audience} 的 Scope`} mode="multiple" value={scopes.filter(scope => resource.scopes.includes(scope))} options={resource.scopes.map(scope => ({ label: scope, value: scope }))} onChange={selected => setGroupScope(resource.scopes, selected)} placeholder="选择 Scope" />
      </div>)}
      {globalScopes.length > 0 && <div className="spec-resource-card">
        <strong>通用 Scope</strong>
        <label htmlFor="global-scopes">无需绑定 Audience</label>
        <Select id="global-scopes" aria-label="通用 Scope" mode="multiple" value={scopes.filter(scope => globalScopes.includes(scope))} options={globalScopes.map(scope => ({ label: scope, value: scope }))} onChange={selected => setGroupScope(globalScopes, selected)} placeholder="选择通用 Scope" />
      </div>}
    </div>
    {(unknownAudiences.length > 0 || unmatched.length > 0) && <Alert type="warning" showIcon className="spec-resource-warning" title="以下是资源目录无法归属的现有值；保存其他字段时会保留它们。" />}
    {unmatched.length > 0 && <Form.Item label="未归属 Scope" extra="可移除旧值；不能从这里新增目录外 Scope。">
      <Select aria-label="未归属 Scope" mode="multiple" value={unmatched} options={unmatched.map(scope => ({ label: scope, value: scope }))} onChange={selected => {
        const old = new Set(unmatched)
        const latestScopes = scopeTokens(form.getFieldValue('scope'))
        form.setFieldValue('scope', [...latestScopes.filter(scope => !old.has(scope)), ...selected])
      }} />
    </Form.Item>}
  </div>
}
