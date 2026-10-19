import {cleanup, render, screen} from '@testing-library/react'
import {afterEach, expect, it} from 'vitest'
import {LoadingState} from './LoadingState'
import {SettingsFeedback} from './SettingsFeedback'
afterEach(cleanup)
it('shares one accessible indicator in block and inline loading states',()=>{
 const view=render(<LoadingState inset/>)
 expect(screen.getByRole('status').textContent).toBe('Loading…')
 expect(view.container.querySelectorAll('[data-spinner]')).toHaveLength(1)
 view.rerender(<LoadingState inline id="loading-models">Loading models…</LoadingState>)
 expect(screen.getByRole('status').tagName).toBe('SPAN')
 expect(screen.getByRole('status').id).toBe('loading-models')
 expect(screen.getByRole('status').textContent).toBe('Loading models…')
})
it('ends settings loading on success or failure without nesting live regions',()=>{
 const view=render(<SettingsFeedback pending>Loading settings…</SettingsFeedback>)
 expect(screen.getAllByRole('status')).toHaveLength(1)
 expect(view.container.querySelector('[data-spinner]')).not.toBeNull()
 view.rerender(<SettingsFeedback pending tone="error">Settings unavailable</SettingsFeedback>)
 expect(screen.getByRole('alert').textContent).toBe('Settings unavailable')
 expect(screen.queryByRole('status')).toBeNull()
 expect(view.container.querySelector('[data-spinner]')).toBeNull()
 view.rerender(<SettingsFeedback tone="success">Saved</SettingsFeedback>)
 expect(screen.getAllByRole('status')).toHaveLength(1)
 expect(view.container.querySelector('[data-spinner]')).toBeNull()
})
