import { useEffect, useState } from 'react'
import { flushSync } from 'react-dom'
import { useBlocker, useNavigate } from 'react-router-dom'
import { CreatePackDialog } from '../shell/CreatePackDialog'

export function CreatePackPage() {
  const navigate = useNavigate()
  const [dirty, setDirty] = useState(false)
  const [writing, setWriting] = useState(false)
  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    (dirty || writing) && currentLocation.pathname !== nextLocation.pathname)
  useEffect(() => {
    if (blocker.state !== 'blocked') return
    if (writing) blocker.reset()
    else if (window.confirm('Leave without creating this pack? Your draft will be discarded.')) blocker.proceed()
    else blocker.reset()
  }, [blocker, writing])
  useEffect(() => {
    if (!dirty && !writing) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty, writing])
  return <div data-measure="wide" data-layout="page">
    <CreatePackDialog open presentation="page"
      onDirtyChange={setDirty} onWritingChange={setWriting}
      onOpenChange={(open) => { if (!open) navigate('/packs') }}
      onCreated={() => flushSync(() => { setDirty(false); setWriting(false) })} />
  </div>
}
