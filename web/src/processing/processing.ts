import { useQuery } from '@tanstack/react-query'
import { answer, deskFetch, FileRequestError } from '../files/client'
import { sourceMessage } from '../i18n/source'

/**
 * Document processing (OCR) settings are the local gateway's (gateway v0.10.0):
 * its connections companion keeps the processors and their credentials, and
 * the gateway reads the settings when it starts. Desk's route
 * `/api/document-processing/{status,configure,test}` carries these three
 * operations to this desk's own companion and to nothing else, and says beside
 * each answer which plan the running local gateway has (docs/document-processing.md).
 */
export type OCRKind = 'tesseract' | 'program' | 'google-document-ai' | 'azure-document-intelligence' | 'aws-textract'

export interface OCRConnection {
  id: string
  name: string
  kind: OCRKind
  program?: string
  enabled: boolean
  /** Status only: the processor's programs were found usable when the status was read. */
  ready?: boolean
  project?: string
  location?: string
  processor?: string
  endpoint?: string
  region?: string
  /**
   * Entered once, sent once, in a save's body, and never shown again: no
   * answer carries it. Blank keeps the saved one, where the destination is
   * unchanged.
   */
  credential?: string
  credentialConfigured?: boolean
}

export interface ProcessingSettings {
  version: 1
  mode: 'off' | 'auto'
  connection: string
  connections: OCRConnection[]
  timeoutSeconds?: number
  sha256: string
  state: string
}

/** What Desk says of its local gateway beside each answer. */
export interface RunningGateway {
  status: 'ready' | 'unavailable'
  /** Whether the plan the running gateway was started with reads scanned pages with OCR. */
  documentProcessing?: boolean
  /** This request restarted the local gateway. */
  restarted?: boolean
  problem?: string
}

export interface ProcessingTest {
  processing: { status: string; errors: { code: string; page: number | null }[] }
  extraction: string
  pageCount: number
  pages: { number: number; status: string; extraction: string; text: string }[]
}

export interface ProcessingAnswer<T> { result: T; localGateway: RunningGateway }

export const PROCESSING_KEY = ['document-processing'] as const
export const PROCESSING_TIMEOUT_BOUNDS = [10, 120] as const
/** The gateway's bound for a test's PDF. */
export const TEST_PDF_BYTES = 4 * 1024 * 1024

/** Each refusal of the companion, in Desk's words; any other word is the last. */
function refusal(word: string): string {
  switch (word) {
    case 'processing-changed': return sourceMessage('The document processing settings changed elsewhere. Reload them before saving.')
    case 'processing-unavailable': return sourceMessage('This processor is not available. Choose an enabled processor.')
    case 'program-not-allowed': return sourceMessage('The program must be in the OCR tools bundle beside the gateway, or in /usr/bin.')
    case 'processor-not-installed': return sourceMessage('This processor’s programs are not installed on this computer.')
    case 'invalid-request': return sourceMessage('The gateway refused these settings. Check each field and try again.')
    case 'private-storage-unavailable': return sourceMessage('The gateway could not read its private settings store.')
    case 'blocked-by-policy': return sourceMessage('Document processing is blocked on this computer.')
    default: return sourceMessage('Document processing could not complete this request.')
  }
}

export class ProcessingError extends Error {
  constructor(message: string, readonly word?: string, readonly localGateway?: RunningGateway) { super(message) }
}

export async function processingCall<T>(method: 'status' | 'configure' | 'test', params: object, signal?: AbortSignal): Promise<ProcessingAnswer<T>> {
  let answered: { result?: T; error?: string; localGateway?: RunningGateway }
  try {
    answered = await answer(await deskFetch(`/api/document-processing/${method}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(params), signal
    }))
  } catch (cause) {
    if (signal?.aborted) throw cause
    throw new ProcessingError(cause instanceof FileRequestError && cause.status === 413
      ? sourceMessage('This request is too large to send.')
      : sourceMessage('Document processing could not be reached. Check the local gateway, then try again.'))
  }
  const localGateway = answered.localGateway ?? { status: 'unavailable' }
  if (typeof answered.error === 'string') throw new ProcessingError(refusal(answered.error), answered.error, localGateway)
  if (answered.result === undefined) throw new ProcessingError(refusal(''), undefined, localGateway)
  return { result: answered.result, localGateway }
}

export async function readProcessing(signal?: AbortSignal): Promise<ProcessingAnswer<ProcessingSettings>> {
  const read = await processingCall<ProcessingSettings>('status', {}, signal)
  const value = read.result
  if (value.version !== 1 || typeof value.sha256 !== 'string' || !Array.isArray(value.connections) || !['off', 'auto'].includes(value.mode)) {
    throw new ProcessingError(sourceMessage('The document processing settings could not be read.'))
  }
  return read
}

export function useProcessing(enabled = true) {
  return useQuery({ queryKey: PROCESSING_KEY, queryFn: ({ signal }) => readProcessing(signal), enabled, retry: false, staleTime: 10_000 })
}

/**
 * A save's body. Status-only members are left out (the gateway refuses
 * them), and so is a blank credential, which keeps the saved one for an
 * unchanged destination. A typed credential travels here and nowhere else.
 */
export function configureRequest(base: ProcessingSettings, next: Pick<ProcessingSettings, 'mode' | 'connection' | 'connections' | 'timeoutSeconds'>) {
  return {
    ifMatch: base.sha256,
    config: {
      version: 1, mode: next.mode, connection: next.connection, timeoutSeconds: next.timeoutSeconds ?? PROCESSING_TIMEOUT_BOUNDS[1],
      connections: next.connections.map(({ ready: _ready, credentialConfigured: _configured, credential, ...connection }) => credential ? { ...connection, credential } : connection)
    }
  }
}

export function saveProcessing(base: ProcessingSettings, next: Pick<ProcessingSettings, 'mode' | 'connection' | 'connections' | 'timeoutSeconds'>) {
  return processingCall<ProcessingSettings>('configure', configureRequest(base, next))
}

/**
 * Whether saving settings in this mode restarts the local gateway: the plan
 * follows the settings only when the gateway starts, so Desk restarts it when
 * a save turns processing on or off relative to the plan it runs. Desk's
 * route decides the same way.
 */
export function restartsGateway(mode: 'off' | 'auto', running: RunningGateway | undefined): boolean {
  return running?.status === 'ready' && (mode === 'auto') !== (running.documentProcessing === true)
}

export function cloudOCR(kind: OCRKind) { return kind !== 'tesseract' && kind !== 'program' }

/** The members that say where a cloud processor sends a page, and its credential. */
export function sameDestination(a: OCRConnection, b: OCRConnection) {
  return a.kind === b.kind && (['program', 'project', 'location', 'processor', 'endpoint', 'region'] as const).every(key => (a[key] ?? '') === (b[key] ?? ''))
}

export function processorName(kind: OCRKind) {
  switch (kind) {
    case 'tesseract': return sourceMessage('Local OCR · Tesseract · English')
    case 'program': return sourceMessage('OCR program on this computer')
    case 'google-document-ai': return 'Google Document AI'
    case 'azure-document-intelligence': return 'Azure Document Intelligence'
    case 'aws-textract': return 'Amazon Textract'
  }
}
