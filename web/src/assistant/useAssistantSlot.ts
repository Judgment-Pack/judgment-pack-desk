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
import type {
  AssistantEndpointConfig,
  AssistantEngine,
  ThinkingTier
} from '../config/deskConfig'

export interface AssistantSlot {
  /**
   * `configured` exactly where an endpoint is, `none` where the file says
   * there is none — and `unavailable` where **this desk could not read the
   * file that would say**.
   *
   * The third is not a third deployment state and is not a shape: it is the
   * absence of an answer, reported as one. A read that failed used to fall
   * through to the built-in defaults, so a desk whose configuration could not
   * be read told every consumer of this hook that no assistant was configured
   * — after a write the chassis had just confirmed. "Absence was not
   * established" is the desk's own doctrine everywhere else, and this is it
   * here.
   */
  state: 'none' | 'configured' | 'unavailable'
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
  /**
   * Which engine would run the loop, and at what depth.
   *
   * Reported whatever `state` is, and defaulted rather than optional: "the
   * file said nothing" and "the file said `vercel`" describe the same desk,
   * and a consumer that had to tell them apart would be a consumer inventing
   * a fourth state. Nothing acts on either in this release — `thinking` in
   * particular is stored and shown, and no request is shaped by it.
   */
  engine: AssistantEngine
  thinking: ThinkingTier
}

export function useAssistantSlot(): AssistantSlot {
  const { config, desk } = useEffectiveConfig()
  const key = useAssistantKey()
  const endpoint = config.assistant.endpoint
  // **A read that did not produce a file is not a file that says none.** A
  // refused *decode* is different and is deliberately not here: that file was
  // read, this desk will not honour it, and the defaults are what apply — which
  // is `none`, truthfully. What this covers is the read that never produced
  // one at all.
  const unread = desk?.readFailure !== undefined
  return {
    state: unread ? 'unavailable' : endpoint === null ? 'none' : 'configured',
    endpoint,
    keyPresent: key.data?.present ?? false,
    engine: config.assistant.engine,
    thinking: config.assistant.thinking
  }
}
