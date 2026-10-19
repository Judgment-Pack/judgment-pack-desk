import { afterEach, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { SettingsActions, SettingsFeedback } from './SettingsFeedback'
import { Button } from './Button'
afterEach(cleanup)
it('announces changes in place and keeps recovery controls outside the live region',()=>{
 const v=render(<SettingsActions feedback={<SettingsFeedback/>}><Button>Save</Button></SettingsActions>)
 const status=screen.getByRole('status')
 screen.getByRole('button',{name:'Save'}).focus()
 v.rerender(<SettingsActions feedback={<SettingsFeedback tone="success">Changes saved</SettingsFeedback>}><Button>Save</Button></SettingsActions>)
 expect(screen.getByRole('status')).toBe(status)
 expect(status.getAttribute('aria-atomic')).toBe('true')
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'Save'}))
 v.rerender(<SettingsFeedback tone="error" actions={<Button>Reload settings</Button>}>Could not confirm the save</SettingsFeedback>)
 expect(screen.getByRole('alert').contains(screen.getByRole('button'))).toBe(false)
 fireEvent.click(screen.getByRole('button'))
 expect(screen.getByRole('alert').textContent).toBe('Could not confirm the save')
})
