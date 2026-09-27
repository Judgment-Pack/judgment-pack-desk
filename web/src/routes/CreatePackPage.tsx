import { useDirtyGuard } from '../shell/useDirtyGuard'
import { msg } from '../i18n'
import { useState } from 'react'
import { flushSync } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { CreatePackDialog } from '../shell/CreatePackDialog'

export function CreatePackPage() {
  const navigate = useNavigate()
  const [dirty, setDirty] = useState(false)
  const [writing, setWriting] = useState(false)
  useDirtyGuard(dirty || writing, msg('Leave without creating this pack? Your draft will be discarded.'), { busy: writing })
  return <div data-layout="page">
    <CreatePackDialog open presentation="page"
      onDirtyChange={setDirty} onWritingChange={setWriting}
      onOpenChange={(open) => { if (!open) navigate('/packs') }}
      onCreated={() => flushSync(() => { setDirty(false); setWriting(false) })} />
  </div>
}
