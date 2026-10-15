import { expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { SpecSystem } from './SpecOverview'

vi.mock('../clients/useHydraStatus', () => ({
  useHydraStatus: () => ({
    error: false,
    status: {
      status: 'supported', observed_version: 'v26.2.0', supported_version: 'v26.2.0',
      spec_sha256: 'test-digest', version_probe: 'ok', health_probe: 'ok', client_list_probe: 'ok',
      write_acceptance: 'not_verified_in_this_deployment', capabilities: { client_list: { enabled: true } },
      replicas: [
        { name: 'hydra-a', status: 'supported', observed_version: 'v26.2.0', version_probe: 'ok', health_probe: 'ok', client_list_probe: 'ok', eligible: true },
        { name: 'hydra-b', status: 'unavailable', version_probe: 'unavailable', health_probe: 'unavailable', client_list_probe: 'unavailable', eligible: false },
      ],
    },
  }),
}))

it('shows each Hydra replica version and health without exposing Admin URLs', () => {
  render(<MemoryRouter><SpecSystem /></MemoryRouter>)
  expect(screen.getByText('hydra-a')).toBeInTheDocument()
  expect(screen.getByText('hydra-b')).toBeInTheDocument()
  expect(screen.getByText('2 个副本')).toBeInTheDocument()
  expect(screen.getByText('已就绪')).toBeInTheDocument()
  expect(screen.getByText('已排除')).toBeInTheDocument()
  expect(screen.queryByText('Client 分页')).not.toBeInTheDocument()
  expect(screen.queryByText(/admin_base_url/)).not.toBeInTheDocument()
})
