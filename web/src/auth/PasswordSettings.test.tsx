import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { PasswordSettings } from './PasswordSettings'
import { SignInEntry } from './SignInEntry'

const auth = vi.hoisted(() => ({ call: vi.fn() }))
vi.mock('./flow', async importOriginal => ({ ...await importOriginal<typeof import('./flow')>(), authCall: auth.call }))
import { SignInError } from './flow'
afterEach(() => { cleanup(); vi.clearAllMocks() })
beforeEach(() => { auth.call.mockReset() })
function fill(name: string, value: string) { fireEvent.change(screen.getByLabelText(name), {target:{value}}) }
function open(mode:'setup'|'password'='setup') {
 render(<PasswordSettings mode={mode} storageAvailable />)
 const trigger=screen.getByRole('button',{name:mode==='setup'?'Set password':'Change password'})
 fireEvent.click(trigger)
 return {trigger,dialog:screen.getByRole('dialog')}
}
const pass='desk1234'
it('offers set without a current password, uses shared fixed footer and restores focus on Cancel',async()=>{
 const {trigger,dialog}=open()
 expect(within(dialog).queryByLabelText('Current password')).toBeNull()
 expect(dialog.getAttribute('data-fixed-footer')).toBe('true')
 expect(screen.getByLabelText('New password').getAttribute('autocomplete')).toBe('new-password')
 fill('New password',pass)
 fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(document.activeElement).toBe(trigger))
 fireEvent.click(trigger)
 expect((screen.getByLabelText('New password') as HTMLInputElement).value).toBe('')
 expect(auth.call).not.toHaveBeenCalled()
})
it('validates length and confirmation without sending the password',async()=>{
 const {dialog}=open()
 fill('New password','abc');fill('Confirm new password','different')
 fireEvent.click(within(dialog).getByRole('button',{name:'Set password'}))
 expect(screen.getByText('Passwords do not match.')).toBeTruthy()
 expect(screen.getByLabelText('New password').getAttribute('aria-invalid')).toBe('true')
 expect(screen.getAllByText('Use 4 to 8 characters.')).toHaveLength(1)
 expect(auth.call).not.toHaveBeenCalled()
})
it('keeps current-password failures beside that field and does not send confirmation',async()=>{
 auth.call.mockRejectedValue(new SignInError('password-current-incorrect'))
 const {dialog}=open('password')
 fill('Current password','wrong password');fill('New password',pass);fill('Confirm new password',pass)
 fireEvent.click(within(dialog).getByRole('button',{name:'Change password'}))
 await screen.findByText('The current password is incorrect.')
 expect(screen.getByLabelText('Current password').getAttribute('aria-invalid')).toBe('true')
 expect(auth.call.mock.calls[0]?.[0]).toBe('password/change')
 expect(auth.call.mock.calls[0]?.[1]).toEqual({currentPassword:'wrong password',newPassword:pass})
 fill('Current password','correct password')
 expect(screen.queryByText('The current password is incorrect.')).toBeNull()
})
it('does not repeat an uncertain save and offers sign-in recovery',async()=>{
 auth.call.mockRejectedValue(new TypeError('response lost'))
 const {dialog}=open()
 fill('New password',pass);fill('Confirm new password',pass)
 fireEvent.click(within(dialog).getByRole('button',{name:'Set password'}))
 await screen.findByText('Could not confirm whether the password was saved. Return to sign-in and try the new password first.')
 expect(screen.getByRole('button',{name:'Return to sign-in'})).toBeTruthy()
 expect(within(dialog).queryByRole('button',{name:'Set password'})).toBeNull()
 expect((screen.getByLabelText('New password') as HTMLInputElement).value).toBe('')
 expect(auth.call).toHaveBeenCalledTimes(1)
})
it('keeps a failed storage write retryable without claiming success',async()=>{
 auth.call.mockRejectedValue(new SignInError('storage'))
 const {dialog}=open()
 fill('New password',pass);fill('Confirm new password',pass)
 fireEvent.click(within(dialog).getByRole('button',{name:'Set password'}))
 await screen.findByText('The sign-in configuration could not be saved securely. Check this computer’s configuration directory.')
 expect((within(dialog).getByRole('button',{name:'Set password'}) as HTMLButtonElement).disabled).toBe(false)
 expect((screen.getByLabelText('New password') as HTMLInputElement).value).toBe(pass)
})
it('never offers a local password bypass for provider sign-in',()=>{
 render(<PasswordSettings mode="provider" storageAvailable />)
 expect(screen.queryByRole('button')).toBeNull()
})
it('shows password entry without setup or automatic local entry, and exposes local recovery',async()=>{
 auth.call.mockResolvedValue({enabled:true,unavailable:false,mode:'password',localAccess:false})
 render(<SignInEntry />)
 await screen.findByLabelText('Desk password')
 expect(screen.getByLabelText('Desk password').getAttribute('autocomplete')).toBe('current-password')
 expect(screen.queryByLabelText('Owner setup code')).toBeNull()
 expect(screen.queryByRole('button',{name:'Continue'})).toBeNull()
 fireEvent.click(screen.getByText('Forgot password?'))
 expect(screen.getByText(/Stop Desk and run jpack-desk/)).toBeTruthy()
})

it.each(['1234','12345678','🔑🔑🔑🔑','🔑🔑🔑🔑🔑🔑🔑🔑'])('submits valid local password without changing its characters (%s)',async value=>{
 auth.call.mockRejectedValue(new SignInError('storage'))
 const {dialog}=open()
 fill('New password',value);fill('Confirm new password',value)
 fireEvent.click(within(dialog).getByRole('button',{name:'Set password'}))
 await waitFor(()=>expect(auth.call).toHaveBeenCalledTimes(1))
 expect(auth.call.mock.calls[0]?.[1]).toEqual({currentPassword:'',newPassword:value})
})
it.each(['abc','123456789','🔑🔑🔑','🔑🔑🔑🔑🔑🔑🔑🔑🔑'])('rejects values outside four to eight characters without truncating (%s)',value=>{
 const {dialog}=open()
 fill('New password',value);fill('Confirm new password',value)
 fireEvent.click(within(dialog).getByRole('button',{name:'Set password'}))
 expect(screen.getAllByText('Use 4 to 8 characters.')).toHaveLength(1)
 expect(screen.getByLabelText('New password').getAttribute('aria-invalid')).toBe('true')
 expect((screen.getByLabelText('New password') as HTMLInputElement).value).toBe(value)
 expect(auth.call).not.toHaveBeenCalled()
})
it('reveals fields independently without submitting, and closes with values cleared and masked',async()=>{
 const {dialog,trigger}=open('password')
 fill('Current password','a previously saved long password');fill('New password',pass);fill('Confirm new password',pass)
 for (const label of ['Current password','New password','Confirm new password']) expect(screen.getByLabelText(label).getAttribute('type')).toBe('password')
 fireEvent.click(within(dialog).getByRole('button',{name:'Show new password'}))
 expect(screen.getByLabelText('New password').getAttribute('type')).toBe('text')
 expect(screen.getByLabelText('Current password').getAttribute('type')).toBe('password')
 expect(screen.getByLabelText('Confirm new password').getAttribute('type')).toBe('password')
 fireEvent.click(within(dialog).getByRole('button',{name:'Show current password'}))
 expect(screen.getByLabelText('Current password').getAttribute('type')).toBe('text')
 expect((screen.getByLabelText('Current password') as HTMLInputElement).value).toBe('a previously saved long password')
 fireEvent.click(within(dialog).getByRole('button',{name:'Show confirmation password'}))
 expect(screen.getByLabelText('Confirm new password').getAttribute('type')).toBe('text')
 fireEvent.click(within(dialog).getByRole('button',{name:'Hide new password'}))
 expect(screen.getByLabelText('New password').getAttribute('type')).toBe('password')
 expect(auth.call).not.toHaveBeenCalled()
 fireEvent.click(within(dialog).getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(document.activeElement).toBe(trigger))
 fireEvent.click(trigger)
 for (const label of ['Current password','New password','Confirm new password']) {
  const input=screen.getByLabelText(label) as HTMLInputElement
  expect(input.type).toBe('password');expect(input.value).toBe('')
 }
})
it('offers show and hide on sign-in without triggering a login request',async()=>{
 auth.call.mockResolvedValue({enabled:true,unavailable:false,mode:'password',localAccess:false})
 render(<SignInEntry />)
 const field=await screen.findByLabelText('Desk password') as HTMLInputElement
 fill('Desk password','older passphrase remains supported')
 const calls=auth.call.mock.calls.length
 fireEvent.click(screen.getByRole('button',{name:'Show password'}))
 expect(field.type).toBe('text');expect(field.value).toBe('older passphrase remains supported')
 fireEvent.click(screen.getByRole('button',{name:'Hide password'}))
 expect(field.type).toBe('password');expect(auth.call).toHaveBeenCalledTimes(calls)
})
