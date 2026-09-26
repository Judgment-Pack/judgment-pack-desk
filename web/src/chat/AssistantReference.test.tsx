import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useState } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { AssistantReferenceProvider, DetailsWithAssistant, ReferenceChip, useChatReference } from './AssistantReference'
import { InspectorSlotContext } from '../shell/InspectorSlot'
afterEach(cleanup)

it('adds explicit context without changing the message, preserves it per chat, and opens the selected detail', () => {
  const open = vi.fn(), reveal = vi.fn()
  function Composer({ id }: { id: string }) {
    const { reference, remove } = useChatReference(id, true)
    return <>{reference && <ReferenceChip reference={reference} onRemove={remove} />}<textarea aria-label="Message" defaultValue="Keep my question" /></>
  }
  function Fixture() {
    const [id, setId] = useState('first')
    return <><DetailsWithAssistant reference={{ label: 'Pilot rule', text: '{"value":"3"}', onOpen: open }}><p>Rule</p></DetailsWithAssistant><Composer key={id} id={id} /><button onClick={() => setId(id === 'first' ? 'second' : 'first')}>Switch chat</button></>
  }
  render(<MemoryRouter><InspectorSlotContext.Provider value={{ target: null, open: true, size: 400, tab: null, setTab: () => {}, claim: () => () => {}, reveal }}><AssistantReferenceProvider><Fixture /></AssistantReferenceProvider></InspectorSlotContext.Provider></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', { name: 'Ask Assistant about this' }))
  expect(reveal).toHaveBeenCalledOnce()
  expect((screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement).value).toBe('Keep my question')
  fireEvent.click(screen.getByRole('button', { name: 'Open details: Pilot rule' }))
  expect(open).toHaveBeenCalledOnce()
  fireEvent.click(screen.getByRole('button', { name: 'Switch chat' }))
  expect(screen.queryByRole('button', { name: 'Remove reference' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Switch chat' }))
  fireEvent.click(screen.getByRole('button', { name: 'Remove reference' }))
  expect(screen.queryByRole('button', { name: 'Open details: Pilot rule' })).toBeNull()
})

it('keeps a reference requested before the first chat exists', () => {
  function Composer() {
    const { reference, remove } = useChatReference('new-chat', true)
    return reference ? <ReferenceChip reference={reference} onRemove={remove} /> : null
  }
  function Fixture() {
    const [ready, setReady] = useState(false)
    return <><DetailsWithAssistant reference={{ label: 'Before chat', text: '{}', onOpen: () => {} }}>Rule</DetailsWithAssistant>{ready && <Composer />}<button onClick={() => setReady(true)}>Create chat</button></>
  }
  render(<MemoryRouter><AssistantReferenceProvider><Fixture /></AssistantReferenceProvider></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', { name: 'Ask Assistant about this' }))
  fireEvent.click(screen.getByRole('button', { name: 'Create chat' }))
  expect(screen.getByRole('button', { name: 'Open details: Before chat' })).toBeTruthy()
})
