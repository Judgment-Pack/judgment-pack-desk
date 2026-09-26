import type { NodePositions } from '../components/RelationshipMap'
import { useEffect, useRef, useState } from 'react'
import type { Viewport } from '@xyflow/react'

export type LogicMode = 'map' | 'list'
const KEY = 'jp-desk:pack-logic-view:v1'
export function initialLogicMode(): LogicMode {
  try { const saved = localStorage.getItem(KEY); if (saved === 'map' || saved === 'list') return saved } catch { /* Viewing works without storage. */ }
  return 'list'
}
export function rememberLogicMode(mode: LogicMode) { try { localStorage.setItem(KEY, mode) } catch { /* Preference only. */ } }
const DISPLAY_KEY = 'jp-desk:pack-logic-display:v1'
export function initialLogicDisplay(defaultConditions = true): { conditions: boolean; grouped: boolean } {
  try {
    const value = JSON.parse(localStorage.getItem(DISPLAY_KEY) ?? 'null')
    if (value && typeof value.conditions === 'boolean' && typeof value.grouped === 'boolean') return { conditions: value.conditions, grouped: value.grouped }
  } catch { /* Malformed or unavailable preferences do not hide pack conditions. */ }
  return { conditions: defaultConditions, grouped: false }
}
export function rememberLogicDisplay(value: ReturnType<typeof initialLogicDisplay>) {
  try { localStorage.setItem(DISPLAY_KEY, JSON.stringify(value)) } catch { /* Preference only. */ }
}
export const DEFAULT_MAP_VIEWPORT: Viewport = { x: 0, y: 24, zoom: 1 }
export function useLogicState(packId?: string) {
  const [preferred, setPreferred] = useState(initialLogicMode)
  const [query, setQuery] = useState('')
  const [display, setDisplay] = useState(initialLogicDisplay)
  const [viewport, setViewport] = useState<Viewport>(DEFAULT_MAP_VIEWPORT)
  const [nodePositions, setNodePositions] = useState<NodePositions>({})
  const listScroll = useRef(0)
  useEffect(() => { setQuery(''); setNodePositions({}); setViewport(DEFAULT_MAP_VIEWPORT); listScroll.current = 0 }, [packId])
  return { preferred, setPreferred, query, setQuery, display, setDisplay, viewport, setViewport, nodePositions, setNodePositions, listScroll }
}
