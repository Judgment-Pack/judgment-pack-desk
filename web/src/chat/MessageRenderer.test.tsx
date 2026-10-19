import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MessageRenderer } from './MessageRenderer'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import { signedDocument } from '../documents/__fixtures__/signedDocument'
import { verifyDocument } from '../documents/client'

const mocks = vi.hoisted(() => ({ load: vi.fn() }))
vi.mock('../documents/client', async original => ({ ...await original<typeof import('../documents/client')>(), loadDocument: mocks.load }))
afterEach(() => { cleanup(); vi.clearAllMocks() })

it('keeps a verified citation open when more response text arrives', async () => {
  const { object, pin, reference } = await signedDocument()
  mocks.load.mockResolvedValue(await verifyDocument(object, pin))
  const config = effectiveConfig(undefined)
  config.config = { ...config.config, research: { ...config.config.research, gateway: pin } }
  const documents = [{ id: reference.id, name: 'notes.txt', text: '', document: reference }]
  const text = `The source says [“line one”](attachment:${reference.id}/${reference.digest}/page/1).`
  const view = (response: string) => <DeskConfigFixture value={config}><MessageRenderer text={response} documents={documents} /></DeskConfigFixture>
  const rendered = render(view(text))
  const citation = screen.getByRole('button', { name: 'View source 1: notes.txt, page 1' })
  fireEvent.click(citation)
  await screen.findByText('Quote found on page 1')
  rendered.rerender(view(`${text}\n\nMore of the answer.`))
  expect(screen.getByRole('button', { name: 'View source 1: notes.txt, page 1' })).toBe(citation)
  expect(screen.getByRole('dialog', { name: 'notes.txt' })).toBeTruthy()
  expect(screen.getByText('Quote found on page 1')).toBeTruthy()
  expect(screen.queryByRole('checkbox')).toBeNull()
  expect(screen.getByText('line one', { selector: 'mark' })).toBeTruthy()
  expect(mocks.load).toHaveBeenCalledTimes(1)
})

it('preserves a reader’s code wrapping preference while the answer grows', () => {
  const text = '```json\n{"example":true}\n```'
  const rendered = render(<MessageRenderer text={text} />)
  fireEvent.click(screen.getByRole('button', { name: 'Wrap' }))
  expect(screen.getByRole('button', { name: 'Wrap' }).getAttribute('aria-pressed')).toBe('true')
  rendered.rerender(<MessageRenderer text={`${text}\n\nThe next paragraph.`} />)
  expect(screen.getByRole('button', { name: 'Wrap' }).getAttribute('aria-pressed')).toBe('true')
})


it('renders growing code, headings, lists, quotes and tables before the reply finishes', async () => {
  const {streamingProse}=await import('@judgment-pack/harness/engines/contract')
  const code='```typescript\nconst count = 1;'
  const rendered=render(<MessageRenderer text={streamingProse(code)}/>)
  const pre=screen.getByLabelText('typescript',{selector:'pre'})
  expect(pre.textContent).toBe('const count = 1;')
  fireEvent.click(screen.getByRole('button',{name:'Wrap'}))
  const next=code+'\nconst next = count + 1;\n```\n\n## Results\n\n- First\n- Second\n\n> Quoted text\n\n| Item | Count |\n| --- | --- |\n| Test | 2 |'
  rendered.rerender(<MessageRenderer text={streamingProse(next)}/>)
  expect(screen.getByLabelText('typescript',{selector:'pre'})).toBe(pre)
  expect(screen.getByRole('button',{name:'Wrap'}).getAttribute('aria-pressed')).toBe('true')
  expect(screen.getByRole('heading',{name:'Results'})).toBeTruthy()
  expect(screen.getAllByRole('listitem')).toHaveLength(2)
  expect(rendered.container.querySelector('blockquote')?.textContent).toContain('Quoted text')
  expect(screen.getByRole('table')).toBeTruthy()
})


it('opens the complete table in a named dialog and restores keyboard focus after Escape', async () => {
  render(<MessageRenderer text={'| Field | Value |\n| --- | --- |\n| `/compliance/frenchLanguageReady` | true |'} />)
  const trigger = screen.getByRole('button', {name: 'Show table'})
  trigger.focus()
  fireEvent.click(trigger)
  const dialog = await screen.findByRole('dialog', {name: 'Response table'})
  expect(within(dialog).getByRole('region', {name: 'Response table'}).tabIndex).toBe(0)
  expect(within(dialog).getAllByRole('columnheader')).toHaveLength(2)
  expect(within(dialog).getByText('/compliance/frenchLanguageReady')).toBeTruthy()
  fireEvent.keyDown(document.activeElement!, {key: 'Escape'})
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(document.activeElement).toBe(trigger)
})

it('keeps an expanded code block open as the message grows and copies the complete source', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  const previous = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
  Object.defineProperty(navigator, 'clipboard', {configurable:true, value:{writeText}})
  try {
    const text = '```json\n{"path":"/a/b/c"}\n```'
    const rendered = render(<MessageRenderer text={text} />)
    const trigger = screen.getByRole('button', {name: 'Show code'})
    expect(screen.getByRole('button', {name: 'Wrap'}).getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(trigger)
    const dialog = await screen.findByRole('dialog', {name: 'json'})
    rendered.rerender(<MessageRenderer text={'```json\n{"path":"/a/b/c","value":true}\n```\n\nMore text'} />)
    expect(screen.getByRole('dialog', {name: 'json'})).toBe(dialog)
    expect(within(dialog).getByLabelText('json', {selector:'pre'}).textContent).toBe(JSON.stringify({path:'/a/b/c',value:true},null,2))
    fireEvent.click(within(dialog).getByRole('button', {name:'Copy json'}))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('{"path":"/a/b/c","value":true}'))
    fireEvent.click(within(dialog).getByRole('button', {name:'Close'}))
    await waitFor(() => expect(document.activeElement).toBe(trigger))
  } finally { previous ? Object.defineProperty(navigator, 'clipboard', previous) : Reflect.deleteProperty(navigator, 'clipboard') }
})
