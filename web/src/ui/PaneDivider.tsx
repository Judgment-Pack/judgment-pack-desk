import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import styles from './PaneDivider.module.css'

/** A vertical separator controlling the pane to its right. */
export function PaneDivider({ label, controls, value, min, max, onChange, onReset, onCollapse }: {
  label: string; controls: string; value: number; min: number; max: number
  onChange: (value: number) => void; onReset: () => void; onCollapse: () => void
}) {
  const drag = useRef<{ id: number; x: number; value: number } | null>(null)
  const [dragging, setDragging] = useState(false)
  const bound = (next: number) => Math.round(Math.max(min, Math.min(max, next)))
  const move = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current
    if (start?.id === event.pointerId) onChange(bound(start.value + start.x - event.clientX))
  }
  const stop = () => { drag.current = null; setDragging(false) }
  useEffect(() => {
    if (!dragging) return
    const body = document.body
    const cursor = body.style.cursor, selection = body.style.userSelect
    body.style.cursor = 'col-resize'; body.style.userSelect = 'none'
    return () => { body.style.cursor = cursor; body.style.userSelect = selection }
  }, [dragging])
  const key = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = event.shiftKey ? 32 : 8
    if (event.key === 'ArrowLeft') onChange(bound(value + step))
    else if (event.key === 'ArrowRight') onChange(bound(value - step))
    else if (event.key === 'Home') onChange(min)
    else if (event.key === 'End') onChange(max)
    else if (event.key === 'Enter') onCollapse()
    else if (event.key === 'Escape') { stop(); onReset() }
    else return
    event.preventDefault()
  }
  return <div className={styles.divider} data-dragging={dragging || undefined} role="separator" tabIndex={0}
    aria-label={label} aria-controls={controls} aria-orientation="vertical"
    aria-valuemin={min} aria-valuemax={max} aria-valuenow={value} aria-valuetext={`${value} pixels wide`}
    title="Drag to resize. Arrow keys adjust width; Shift moves faster. Double-click to reset."
    onKeyDown={key} onDoubleClick={onReset}
    onPointerDown={event => {
      if (event.button !== 0 || !event.isPrimary) return
      event.preventDefault(); event.currentTarget.focus()
      event.currentTarget.setPointerCapture(event.pointerId)
      drag.current = { id: event.pointerId, x: event.clientX, value }
      setDragging(true)
    }} onPointerMove={move} onPointerUp={event => {
      move(event); stop()
      if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    }} onPointerCancel={stop} onLostPointerCapture={stop} />
}
