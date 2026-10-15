import { expect, it } from 'vitest'
import { scopeTokens, scopeWireValue, sameScopes, unmatchedScopes } from './resourceAccess'

it('serializes multiple scope tags to the Hydra string format', () => {
  expect(scopeTokens('orders.read  orders.write\nopenid')).toEqual(['orders.read', 'orders.write', 'openid'])
  expect(scopeWireValue(['orders.read', ' orders.write ', 'orders.read'])).toBe('orders.read orders.write')
  expect(sameScopes(['orders.write', 'orders.read'], 'orders.read orders.write')).toBe(true)
})

it('keeps unknown and orphaned scopes visible instead of dropping them', () => {
  const catalog = { global_scopes: ['openid'], audiences: [
    { audience: 'https://api.example.test/orders', scopes: ['orders.read', 'orders.write'] },
    { audience: 'https://api.example.test/billing', scopes: ['billing.read'] },
  ] }
  expect(unmatchedScopes(['orders.read', 'billing.read', 'openid', 'legacy'], ['https://api.example.test/orders'], catalog)).toEqual(['billing.read', 'legacy'])
})
