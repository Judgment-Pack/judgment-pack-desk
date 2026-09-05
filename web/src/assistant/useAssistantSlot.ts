/**
 * The assistant slot, as one reading for whatever comes to depend on it.
 *
 * **Nothing renders a tab, and this hook does not make one appear.** It is the
 * surface the assistant pane will read when it is built: one hook, so that the
 * question "is there an assistant on this desk" has a single answer rather
 * than a configuration read and a key read reassembled at each call site into
 * whatever that site happened to think the states were.
 *
 * **`state` is two values and not three.** There is no `bring-your-own` and no
 * `supplied`, because the desk cannot tell them apart and must not try: an
 * endpoint someone operates for you and an endpoint you run are the same
 * object with a different URL in it, and a third state would be a place for
 * one of them to acquire something the other lacks. What a reader gets is
 * whether an endpoint is configured, what it is, and whether there is a key —
 * and `configured` deliberately does not fold the key in, because "an endpoint
 * with no key yet" is a real and reportable state rather than the absence of
 * one.
 */
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useAssistantKey } from './queries'
import type { AssistantEndpointConfig } from '../config/deskConfig'

export interface AssistantSlot {
  /** `configured` exactly where an endpoint is; the key is reported apart. */
  state: 'none' | 'configured'
  /** Null exactly where `state` is `none`. There is no third value. */
  endpoint: AssistantEndpointConfig | null
  /**
   * Whether a key is stored on this machine.
   *
   * `false` while the read has not answered, which is the honest reading: the
   * page has not been told there is one. Nothing branches on it except to say
   * so, and nothing gates on it — the chassis refuses a probe with no key by
   * name, which is where that decision belongs.
   */
  keyPresent: boolean
}

export function useAssistantSlot(): AssistantSlot {
  const { config } = useEffectiveConfig()
  const key = useAssistantKey()
  const endpoint = config.assistant.endpoint
  return {
    state: endpoint === null ? 'none' : 'configured',
    endpoint,
    keyPresent: key.data?.present ?? false
  }
}
