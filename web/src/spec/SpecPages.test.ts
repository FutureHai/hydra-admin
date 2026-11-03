import { expect, test } from 'vitest'
import { mergeCollection } from './SpecPages'

test('collection pages merge repeated Scope values and Client IDs', () => {
  const first = {
    items: [{ value: 'read', client_count: 2, client_ids: ['a', 'b'] }],
    source_client_count: 2, complete: false, next_page_token: 'next', notice: '', capabilities: {},
  }
  const second = {
    items: [
      { value: 'read', client_count: 2, client_ids: ['b', 'c'] },
      { value: 'write', client_count: 1, client_ids: ['c'] },
    ],
    source_client_count: 2, complete: true, notice: '', capabilities: {},
  }
  expect(mergeCollection(first, second)).toMatchObject({
    complete: true,
    source_client_count: 4,
    items: [
      { value: 'read', client_count: 3, client_ids: ['a', 'b', 'c'] },
      { value: 'write', client_count: 1, client_ids: ['c'] },
    ],
  })
})
