import { Tooltip as Primitive } from 'radix-ui'
import { cloneElement, createContext, useContext, useEffect, useId, useImperativeHandle, useLayoutEffect, useState, type HTMLAttributes, type Ref, type RefObject, type ReactElement, type ReactNode } from 'react'
import { useInRouterContext, useLocation } from 'react-router-dom'
import styles from './Tooltip.module.css'

const Provided = createContext(false)
const DISMISS = 'desk-tooltip-dismiss'

/** One delay/skip-delay policy; standalone primitives retain a safe fallback. */
export function TooltipProvider({ children, delayDuration = 300 }: { children: ReactNode; delayDuration?: number }) {
  const routed = useInRouterContext()
  return <Provided.Provider value><Primitive.Provider delayDuration={delayDuration} skipDelayDuration={300}>
    {routed && <DismissOnNavigation />}{children}
  </Primitive.Provider></Provided.Provider>
}

function DismissOnNavigation() {
  const location = useLocation()
  useEffect(() => { document.dispatchEvent(new Event(DISMISS)) }, [location.key])
  return null
}

interface TooltipProps {
  children: ReactElement
  content?: string
  shortcut?: string
  disabled?: boolean
  /** Suppress hints on controls that a dialog focuses automatically. */
  openOnFocus?: boolean
  side?: 'top' | 'right' | 'bottom' | 'left'
  /** Internal to OverflowTooltip: measure these descendants, or the trigger. */
  overflow?: { selector?: string; fallback?: string }
}

/** Brief supplemental text. The child owns its role, name, focus and actions. */
export function Tooltip(props: TooltipProps) {
  const provided = useContext(Provided)
  return provided ? <TooltipContent {...props} /> : <TooltipProvider><TooltipContent {...props} /></TooltipProvider>
}

/** Uses existing focusable row controls; no nested buttons or extra tab stops. */
export function OverflowTooltip({ children, selector, fallback, content }: {
  children: ReactElement; selector?: string; fallback?: string; content?: string
}) {
  return <Tooltip content={content} overflow={{ selector, fallback }}>{children}</Tooltip>
}

function TooltipContent({ children, content, shortcut, disabled, openOnFocus = true, side = 'top', overflow }: TooltipProps) {
  const [trigger, setTrigger] = useState<HTMLElement | null>(null)
  const [clipped, setClipped] = useState('')
  const [open, setOpen] = useState(false)
  const id = useId()
  const selector = overflow?.selector
  const onlyOverflow = overflow !== undefined

  useLayoutEffect(() => {
    if (!trigger || !onlyOverflow) return
    let disposed = false
    const targets = () => selector ? [...trigger.querySelectorAll<HTMLElement>(selector)] : [trigger]
    const measure = () => {
      if (disposed) return
      setClipped(targets().filter(node => node.clientWidth > 0 && node.clientHeight > 0 &&
        (node.scrollWidth > node.clientWidth + 1 || node.scrollHeight > node.clientHeight + 1))
        .map(node => node.textContent?.trim()).filter(Boolean).join('\n'))
    }
    const resize = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(measure)
    const observe = () => { resize?.disconnect(); resize?.observe(trigger); targets().forEach(node => resize?.observe(node)); measure() }
    const changes = new MutationObserver(observe)
    changes.observe(trigger, { childList: true, characterData: true, subtree: true, attributes: true, attributeFilter: ['class', 'style', 'hidden'] })
    const appearance = new MutationObserver(measure)
    appearance.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style', 'data-theme', 'data-density'] })
    observe()
    window.addEventListener('resize', measure)
    document.fonts?.addEventListener('loadingdone', measure)
    void document.fonts?.ready.then(measure)
    return () => {
      disposed = true; resize?.disconnect(); changes.disconnect(); appearance.disconnect()
      window.removeEventListener('resize', measure); document.fonts?.removeEventListener('loadingdone', measure)
    }
  }, [trigger, onlyOverflow, selector])

  const value = onlyOverflow ? clipped ? content ?? clipped : '' : content ?? ''
  // Long questions are read in Preview; a tooltip never becomes a document.
  const text = overflow?.fallback && value.length > 320 ? overflow.fallback : value
  const enabled = !disabled && text.trim() !== ''
  useEffect(() => { setOpen(false) }, [enabled, text])
  useEffect(() => {
    const dismiss = () => setOpen(false)
    document.addEventListener(DISMISS, dismiss)
    window.addEventListener('resize', dismiss)
    return () => { document.removeEventListener(DISMISS, dismiss); window.removeEventListener('resize', dismiss) }
  }, [])
  const childDescription = (children.props as { 'aria-describedby'?: string })['aria-describedby']
  const description = [childDescription, open && enabled ? id : undefined].filter(Boolean).join(' ') || undefined
  return <Primitive.Root open={open && enabled} onOpenChange={next => setOpen(next && enabled)}>
    <Primitive.Trigger asChild ref={setTrigger} aria-describedby={description} onFocus={event => { if (!openOnFocus) event.preventDefault() }}>
      {cloneElement(children as ReactElement<{ 'aria-describedby'?: string }>, { 'aria-describedby': description })}
    </Primitive.Trigger>
    <Primitive.Portal>
      <Primitive.Content id={id} className={styles.content} side={side} sideOffset={8}
        collisionPadding={12} hideWhenDetached>
        <span>{text}</span>{shortcut && <kbd className={styles.shortcut}>{shortcut}</kbd>}
      </Primitive.Content>
    </Primitive.Portal>
  </Primitive.Root>
}


/** The same Radix tooltip for DOM owned by an embedded editor. No extra roots,
 * wrappers or tab stops: the existing element keeps its listeners and focus. */
export function DOMTooltips({ container }: { container: RefObject<HTMLElement | null> }) {
  const [targets, setTargets] = useState<{ element: HTMLElement; text: string; disabled: boolean }[]>([])
  useLayoutEffect(() => {
    const host = container.current
    if (!host) return
    const collect = () => {
      const next = [...host.querySelectorAll<HTMLElement>('[data-tooltip]')].map(element => ({
        element, text: element.dataset.tooltip ?? '', disabled: element.matches(':disabled') || !!element.closest('[hidden]')
      }))
      setTargets(previous => previous.length === next.length && previous.every((item, index) =>
        item.element === next[index]!.element && item.text === next[index]!.text && item.disabled === next[index]!.disabled) ? previous : next)
    }
    const observer = new MutationObserver(collect)
    observer.observe(host, { childList: true, subtree: true, attributes: true, attributeFilter: ['data-tooltip', 'disabled', 'hidden'] })
    collect()
    return () => observer.disconnect()
  }, [container])
  return targets.map(({ element, text, disabled }) => <DOMTooltip key={elementKey(element)} element={element} text={text} disabled={disabled} />)
}

const keys = new WeakMap<HTMLElement, number>()
let sequence = 0
function elementKey(element: HTMLElement) {
  if (!keys.has(element)) keys.set(element, ++sequence)
  return keys.get(element)!
}
function DOMTooltip({ element, text, disabled }: { element: HTMLElement; text: string; disabled: boolean }) {
  const [description] = useState(() => element.getAttribute('aria-describedby') ?? undefined)
  return <Tooltip content={text} disabled={disabled}><DOMTrigger element={element} aria-describedby={description} /></Tooltip>
}

/** Radix asChild supplies its trigger events/ref. Bind them to the existing DOM
 * instead of rendering a replacement and destroying the editor's own handlers. */
function DOMTrigger({ element, ref, ...props }: HTMLAttributes<HTMLElement> & { element: HTMLElement; ref?: Ref<HTMLElement> }) {
  useImperativeHandle(ref, () => element, [element])
  useLayoutEffect(() => {
    const disposers: (() => void)[] = []
    for (const [name, value] of Object.entries(props)) {
      if (/^on[A-Z]/.test(name) && typeof value === 'function') {
        const event = name.slice(2).toLowerCase()
        const listener = value as EventListener
        element.addEventListener(event, listener)
        disposers.push(() => element.removeEventListener(event, listener))
      } else if (name === 'aria-describedby' || name === 'data-state') {
        const before = element.getAttribute(name)
        if (value == null) element.removeAttribute(name)
        else element.setAttribute(name, String(value))
        disposers.push(() => { if (before === null) element.removeAttribute(name); else element.setAttribute(name, before) })
      }
    }
    return () => disposers.forEach(dispose => dispose())
  }, [element, props])
  return null
}
