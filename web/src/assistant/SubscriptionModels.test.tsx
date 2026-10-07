import { useState } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { SubscriptionModels } from './SubscriptionModels'
import type { ProviderModel } from './providers'
const models:ProviderModel[]=[{id:'alpha',name:'Alpha',efforts:['low'],defaultEffort:'low'},{id:'beta',name:'Beta',efforts:['high'],defaultEffort:'high'}]
afterEach(cleanup)
function Harness(){
 const [state,setState]=useState({allowed:['alpha'],model:'alpha'})
 return <><SubscriptionModels models={models} {...state} disabled={false} onChange={(allowed,model)=>setState({allowed,model})}/><output>{JSON.stringify(state)}</output></>
}
it('keeps multiple selections open, offers bulk actions, and moves the default when removed',()=>{
 render(<Harness/>);fireEvent.click(screen.getByRole('button',{name:'Allowed models'}))
 expect((screen.getByRole('checkbox',{name:'alpha'}) as HTMLInputElement).checked).toBe(true)
 fireEvent.click(screen.getByRole('checkbox',{name:'beta'}));expect(screen.getByRole('dialog')).toBeTruthy()
 expect(screen.getByRole('status').textContent).toBe('{"allowed":["alpha","beta"],"model":"alpha"}')
 fireEvent.click(screen.getByRole('checkbox',{name:'alpha'}))
 expect(screen.getByRole('status').textContent).toBe('{"allowed":["beta"],"model":"beta"}')
 fireEvent.click(screen.getByRole('button',{name:'Remove all'}))
 expect(screen.getByRole('status').textContent).toBe('{"allowed":[],"model":""}')
 expect((screen.getByRole('combobox',{name:'Default model'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Select all'}))
 expect(screen.getByRole('status').textContent).toBe('{"allowed":["alpha","beta"],"model":"alpha"}')
})
it('filters without losing hidden selections and supports keyboard navigation',()=>{
 render(<Harness/>);fireEvent.click(screen.getByRole('button',{name:'Allowed models'}))
 const search=screen.getByRole('searchbox',{name:'Search models'})
 fireEvent.change(search,{target:{value:'bet'}});expect(screen.queryByRole('checkbox',{name:'alpha'})).toBeNull()
 fireEvent.keyDown(search,{key:'ArrowDown'});expect(document.activeElement).toBe(screen.getByRole('checkbox',{name:'beta'}))
 fireEvent.click(screen.getByRole('checkbox',{name:'beta'}))
 expect(screen.getByRole('status').textContent).toBe('{"allowed":["alpha","beta"],"model":"alpha"}')
 fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'})
 expect(screen.queryByRole('dialog')).toBeNull()
})
