import { useState, type ReactNode } from 'react'
import { msg } from '../i18n'
import { Disclosure } from '../ui/Disclosure'

/** Once opened, keep the brief mounted so generation and reading state survive collapse. */
export function ReviewBrief({children}: {children: ReactNode}) {
  const [visited,setVisited]=useState(false)
  return <Disclosure title={msg('Brief')} onToggle={event=>{if(event.currentTarget.open)setVisited(true)}}>{visited && children}</Disclosure>
}
