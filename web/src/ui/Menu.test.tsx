import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { MemoryRouter, Link } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { Menu } from './Menu'
import { IconFolder } from '../shell/icons'

afterEach(cleanup)

it('preserves links and selection callbacks through the shared row', async () => {
  const selected = vi.fn()
  render(<MemoryRouter><Menu.Root><Menu.Trigger>Actions</Menu.Trigger><Menu.Portal><Menu.Content>
    <Menu.Item asChild icon={<IconFolder/>} onSelect={selected}><Link to="/author">Project files</Link></Menu.Item>
  </Menu.Content></Menu.Portal></Menu.Root></MemoryRouter>)
  fireEvent.keyDown(screen.getByRole('button'), {key:'Enter'})
  const item = await screen.findByRole('menuitem', {name:'Project files'})
  expect(item.getAttribute('href')).toBe('/author')
  expect(item.tagName).toBe('A')
  fireEvent.click(item)
  expect(selected).toHaveBeenCalledOnce()
  await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
})

it('skips disabled commands and prevents activation of explained unavailable commands', async () => {
  const selected = vi.fn()
  render(<Menu.Root><Menu.Trigger>Actions</Menu.Trigger><Menu.Portal><Menu.Content>
    <Menu.Item disabled onSelect={selected}>Unavailable</Menu.Item>
    <Menu.Item aria-disabled onSelect={selected}>Compose graph</Menu.Item>
    <Menu.Item>Open</Menu.Item>
  </Menu.Content></Menu.Portal></Menu.Root>)
  fireEvent.keyDown(screen.getByRole('button'), {key:'Enter'})
  const explained = await screen.findByRole('menuitem', {name:'Compose graph'})
  await waitFor(() => expect(document.activeElement).toBe(explained))
  fireEvent.click(explained)
  expect(selected).not.toHaveBeenCalled()
  expect(screen.getByRole('menu')).toBeTruthy()
  fireEvent.keyDown(explained, {key:'ArrowDown'})
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('menuitem', {name:'Open'})))
})

it('keeps radio choices open when requested and returns focus on Escape', async () => {
  function Choices() {
    const [value,setValue] = useState('all')
    return <Menu.Root><Menu.Trigger>Type</Menu.Trigger><Menu.Portal><Menu.Content><Menu.RadioGroup value={value} onValueChange={setValue}>
      <Menu.RadioItem value="all" onSelect={event=>event.preventDefault()}>All types</Menu.RadioItem>
      <Menu.RadioItem value="pack" onSelect={event=>event.preventDefault()}>Packs</Menu.RadioItem>
    </Menu.RadioGroup></Menu.Content></Menu.Portal></Menu.Root>
  }
  render(<Choices/>)
  const trigger = screen.getByRole('button')
  fireEvent.keyDown(trigger, {key:'Enter'})
  const packs = await screen.findByRole('menuitemradio', {name:'Packs'})
  fireEvent.click(packs)
  expect(packs.getAttribute('aria-checked')).toBe('true')
  expect(screen.getByRole('menuitemradio', {name:'All types'}).getAttribute('aria-checked')).toBe('false')
  fireEvent.keyDown(packs, {key:'Escape'})
  await waitFor(() => expect(document.activeElement).toBe(trigger))
})

it('opens nested menus with the keyboard and closes back to their trigger', async () => {
  render(<Menu.Root><Menu.Trigger>Account</Menu.Trigger><Menu.Portal><Menu.Content>
    <Menu.Sub><Menu.SubTrigger>Language</Menu.SubTrigger><Menu.Portal><Menu.SubContent><Menu.Item>English</Menu.Item></Menu.SubContent></Menu.Portal></Menu.Sub>
  </Menu.Content></Menu.Portal></Menu.Root>)
  fireEvent.keyDown(screen.getByRole('button'), {key:'Enter'})
  const language = await screen.findByRole('menuitem', {name:'Language'})
  language.focus()
  fireEvent.keyDown(language, {key:'ArrowRight'})
  const english = await screen.findByRole('menuitem', {name:'English'})
  await waitFor(() => expect(document.activeElement).toBe(english))
  fireEvent.keyDown(english, {key:'ArrowLeft'})
  await waitFor(() => expect(document.activeElement).toBe(language))
})
