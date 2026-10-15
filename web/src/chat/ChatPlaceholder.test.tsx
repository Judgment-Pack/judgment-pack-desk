import {cleanup, render, screen} from '@testing-library/react'
import {afterEach, expect, it} from 'vitest'
import {ChatPlaceholder} from './ChatPlaceholder'
afterEach(cleanup)

it('ends the loading indicator when history fails, keeping recovery in the status strip', () => {
 const view=render(<ChatPlaceholder loading/>)
 expect(screen.getByRole('status').textContent).toBe('Loading chats…')
 expect(view.container.querySelector('[data-spinner]')).not.toBeNull()
 view.rerender(<ChatPlaceholder loading error/>)
 expect(screen.getByRole('status').textContent).toBe('Chat history unavailable')
 expect(view.container.querySelector('[data-spinner]')).toBeNull()
 expect(screen.queryByText('Loading chats…')).toBeNull()
 expect(screen.queryByRole('alert')).toBeNull()
 view.rerender(<ChatPlaceholder>Open Assistant to start a conversation.</ChatPlaceholder>)
 expect(screen.getByRole('status').textContent).toBe('Open Assistant to start a conversation.')
 expect(view.container.querySelector('[data-spinner]')).toBeNull()
})
