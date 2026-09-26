import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { INITIAL_STATE } from '../research/run'
import { workItems, TaskStatus, WorkSummary, candidateSummary } from './RunPresentation'
import { MessageRenderer } from './MessageRenderer'
afterEach(cleanup)

it('pairs parallel invocations by identity and preserves a pending or failed call', () => {
  const rows = workItems([
    { type: 'tool_call', callId: 'a', name: 'read_source', args: {} },
    { type: 'tool_call', callId: 'b', name: 'read_source', args: {} },
    { type: 'tool_result', callId: 'b', name: 'read_source', text: 'refused', isError: true }
  ], false)
  expect(rows).toEqual([{ id: 'a', name: 'read_source', status: 'interrupted' }, { id: 'b', name: 'read_source', status: 'failed' }])
})
it('prints one progress status and no infrastructure event count for a greeting', () => {
  const state = { ...INITIAL_STATE, status: 'running' as const, detail: 'Working…', events: [{ type: 'guardrail' as const, tool: 'get_schema', action: 'narrowed' as const, detail: 'Provider schema compatibility' }] }
  const rendered = render(<><TaskStatus state={state} /><WorkSummary state={state} /></>)
  expect(screen.getAllByText('Working…')).toHaveLength(1)
  expect(screen.queryByText(/events|Work ·|Provider schema/)).toBeNull()
  rendered.rerender(<TaskStatus state={{ ...state, status: 'complete' }} />)
  expect(screen.queryByRole('status')).toBeNull()
})
it('does not label a structural check as behavioral tests passed', () => {
  expect(candidateSummary({ ...INITIAL_STATE, candidates: [{ digest: 'd', check: { valid: true, documentDigest: 'd', cases: [] } } as any] })).toBe('Structure checked · Tests not run')
})
it('renders useful Markdown while refusing active HTML, remote images and unsafe links', () => {
  const rendered = render(<MessageRenderer text={'**Result**\n\n- First\n- Second\n\n[Source](https://example.org/page) [unsafe](javascript:alert(1))\n\n```json\n{"ok":true}\n```\n\n<img src=x onerror=alert(1)>\n\n![external](https://tracker.invalid/pixel)'} />)
  expect(rendered.container.querySelectorAll('strong')).toHaveLength(1)
  expect(rendered.container.querySelectorAll('li')).toHaveLength(2)
  expect(screen.getByRole('link', { name: 'Source' }).getAttribute('rel')).toContain('noopener')
  expect(rendered.container.querySelectorAll('img,script')).toHaveLength(0)
  expect(screen.queryByRole('link', { name: 'unsafe' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Copy json' })).toBeTruthy()
})


it('marks only running status for motion, leaving actionable errors static', () => {
 const view=render(<TaskStatus state={{...INITIAL_STATE,status:'running',detail:'Working…'}}/>)
 expect(screen.getByRole('status').getAttribute('data-running')).toBe('true')
 view.rerender(<TaskStatus state={{...INITIAL_STATE,status:'failed',detail:'Try again'}}/>)
 expect(screen.getByRole('alert').hasAttribute('data-running')).toBe(false)
 view.rerender(<TaskStatus state={{...INITIAL_STATE,status:'ready'}}/>)
 expect(screen.queryByRole('status')).toBeNull()
})
