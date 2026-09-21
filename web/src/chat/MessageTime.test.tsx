import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { languageReady, setLanguage } from '../i18n'
import { MessageTime } from './MessageTime'
import { formatMessageTime } from './timestamps'
import { MessageDetails } from './ReadingDetails'
import type { Turn } from '../research/run'

afterEach(async () => { cleanup(); setLanguage('system'); await languageReady() })
const turn: Turn = { role: 'user', kind: 'message', text: 'Hello', at: '2026-09-21T18:14:37.000Z' }
it('shows the speaker and short time, exposes precision on focus, and opens details from the same control', async () => {
  const formatted = formatMessageTime(turn.at, 'en-US', 'America/Toronto')!, open = vi.fn()
  const { container } = render(<MessageTime turn={turn} formatted={formatted} onOpen={open} />)
  expect(screen.getByText('You')).toBeTruthy()
  expect(container.querySelector('time')?.getAttribute('datetime')).toBe(turn.at)
  const button = screen.getByRole('button', { name: `Message details: ${formatted.full}` })
  expect(button.textContent).toBe('2:14 PM')
  expect(button.hasAttribute('title')).toBe(false)
  fireEvent.focus(button)
  expect(await screen.findByRole('tooltip')).toBeTruthy()
  fireEvent.keyDown(button, { key: 'Escape' })
  expect(screen.queryByRole('tooltip')).toBeNull()
  fireEvent.click(button)
  expect(open).toHaveBeenCalledWith(button)
})
it('keeps invalid legacy times inspectable without invalid time markup', () => {
  const open = vi.fn(), { container } = render(<MessageTime turn={{ ...turn, at: 'unknown' }} onOpen={open} />)
  expect(container.querySelector('time')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Message details: Time unavailable' }))
  expect(open).toHaveBeenCalledOnce()
})
it.each([
  [turn, 'You', 'Sent'],
  [{ ...turn, role: 'assistant' as const, interrupted: true }, 'Assistant', 'Recorded'],
  [{ ...turn, role: 'assistant' as const, kind: 'note' as const }, 'Desk', 'Recorded']
])('shows persisted metadata without fabricating sent context for %j', (message, speaker, label) => {
  render(<MessageDetails text={message.text} turn={message} />)
  expect(screen.getByText(speaker)).toBeTruthy()
  expect(screen.getByText(label)).toBeTruthy()
  expect(screen.getByRole('region', { name: 'Message details' }).querySelector('time')?.getAttribute('datetime')).toBe(turn.at)
  expect(screen.queryByText('Exact sent context')).toBeNull()
  if (message.interrupted) expect(screen.getByText('Response interrupted')).toBeTruthy()
})
it('updates metadata when the app language changes without remounting the trigger', async () => {
  const { container } = render(<MessageTime turn={turn} formatted={formatMessageTime(turn.at, 'en', 'UTC')} onOpen={() => {}} />)
  const button = container.querySelector('button')
  await act(async () => { setLanguage('fr'); await languageReady() })
  expect(screen.getByText('Vous')).toBeTruthy()
  expect(container.querySelector('button')).toBe(button)
})
