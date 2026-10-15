import { useEffect, useState } from 'react'
import { api } from '../auth/api'

export type HydraStatus = {
  status: string
  observed_version?: string
  supported_version: string
  spec_sha256: string
  version_probe: string
  health_probe: string
  client_list_probe: string
  write_acceptance: string
  diagnostic?: string
  capabilities: Record<string, { enabled: boolean; reason?: string }>
  replicas?: {
    name: string
    status: string
    observed_version?: string
    version_probe: string
    health_probe: string
    client_list_probe: string
    eligible: boolean
  }[]
}

export function useHydraStatus() {
  const [status, setStatus] = useState<HydraStatus | null>(null)
  const [error, setError] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    const load = () => {
      void api.get<HydraStatus>('/system/status', { signal: controller.signal }).then(response => {
        setStatus(response.data)
        setError(false)
      }).catch(() => {
        if (!controller.signal.aborted) setError(true)
      })
    }
    load()
    const timer = window.setInterval(load, 15000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [])
  return { status, error }
}
