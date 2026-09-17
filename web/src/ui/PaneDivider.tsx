import { msg } from '../i18n'
import { Tooltip } from './Tooltip'
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import styles from './PaneDivider.module.css'

/** Resize the pane to the right or below with the same pointer and keyboard behavior. */
export function PaneDivider({ label, controls, value, min, max, onChange, onReset, onCollapse, orientation = 'vertical' }: {
  orientation?: 'vertical' | 'horizontal'
  label: string; controls: string; value: number; min: number; max: number
  onChange: (value: number) => void; onReset: () => void; onCollapse: () => void
}) {
  const horizontal = orientation === 'horizontal'
  const dimension = horizontal ? 'height' : 'width'
  const help = msg("Drag to resize. Arrow keys adjust {{value0}}; Shift moves faster. Double-click to reset.", { value0: dimension })
  const drag = useRef<{ id: number; x: number; value: number } | null>(null)
  const [dragging, setDragging] = useState(false)
  const [keyboardFocus, setKeyboardFocus] = useState(false)
  const bound = (next: number) => Math.round(Math.max(min, Math.min(max, next)))
  const move = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current
    if (start?.id === event.pointerId) onChange(bound(start.value + start.x - (horizontal ? event.clientY : event.clientX)))
  }
  const stop = () => { drag.current = null; setDragging(false) }
  useEffect(() => {
    if (!dragging) return
    const body = document.body
    const cursor = body.style.cursor, selection = body.style.userSelect
    body.style.cursor = horizontal ? 'row-resize' : 'col-resize'; body.style.userSelect = 'none'
    return () => { body.style.cursor = cursor; body.style.userSelect = selection }
  }, [dragging, horizontal])
  const key = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.defaultPrevented) return
    setKeyboardFocus(true)
    const step = event.shiftKey ? 32 : 8
    if (event.key === (horizontal ? 'ArrowUp' : 'ArrowLeft')) onChange(bound(value + step))
    else if (event.key === (horizontal ? 'ArrowDown' : 'ArrowRight')) onChange(bound(value - step))
    else if (event.key === 'Home') onChange(min)
    else if (event.key === 'End') onChange(max)
    else if (event.key === 'Enter') onCollapse()
    else if (event.key === 'Escape') { stop(); onReset() }
    else return
    event.preventDefault()
  }
  return <Tooltip disabled={dragging || keyboardFocus} openOnFocus={false} side="left" content={help}><div className={styles.divider} data-orientation={orientation} data-dragging={dragging || undefined}
    data-keyboard-focus={keyboardFocus || undefined} role="separator" tabIndex={0}
    aria-label={label} aria-controls={controls} aria-orientation={orientation}
    aria-valuemin={min} aria-valuemax={max} aria-valuenow={value} aria-valuetext={`${value} pixels ${horizontal ? 'high' : 'wide'}`}
    aria-description={help}
    onKeyDown={key} onDoubleClick={onReset}
    onFocus={event => setKeyboardFocus(event.currentTarget.matches(':focus-visible'))}
    onBlur={() => setKeyboardFocus(false)}
    onPointerDown={event => {
      if (event.button !== 0 || !event.isPrimary) return
      event.preventDefault(); event.currentTarget.focus()
      // Programmatic focus can inherit :focus-visible from a text field or
      // keyboard interaction. A pointer gesture must not leave that paint behind.
      setKeyboardFocus(false)
      event.currentTarget.setPointerCapture(event.pointerId)
      drag.current = { id: event.pointerId, x: horizontal ? event.clientY : event.clientX, value }
      setDragging(true)
    }} onPointerMove={move} onPointerUp={event => {
      move(event); stop()
      if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    }} onPointerCancel={stop} onLostPointerCapture={stop} /></Tooltip>
}
