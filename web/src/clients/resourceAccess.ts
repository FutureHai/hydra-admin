import type { ResourceCatalog } from './api'

export function scopeTokens(value: unknown): string[] {
  if (typeof value === 'string') return [...new Set(value.trim().split(/\s+/).filter(Boolean))]
  if (Array.isArray(value)) return [...new Set(value.filter((item): item is string => typeof item === 'string').map(item => item.trim()).filter(Boolean))]
  return []
}

export function scopeWireValue(value: unknown): string {
  return scopeTokens(value).join(' ')
}

export function sameScopes(left: unknown, right: unknown): boolean {
  const a = scopeTokens(left)
  const b = scopeTokens(right)
  return a.length === b.length && a.every(item => b.includes(item))
}

export function catalogEnabled(catalog?: ResourceCatalog): boolean {
  return Boolean(catalog?.audiences?.length || catalog?.global_scopes?.length)
}

export function unmatchedScopes(scopes: string[], audiences: string[], catalog: ResourceCatalog): string[] {
  const global = new Set(catalog.global_scopes ?? [])
  const selected = new Set(audiences)
  const matching = new Set((catalog.audiences ?? []).filter(resource => selected.has(resource.audience)).flatMap(resource => resource.scopes))
  return scopes.filter(scope => !global.has(scope) && !matching.has(scope))
}
