/**
 * Where the upgrade offer appears (ADR-0009, section 4): once, as a note on
 * Packs the owner can dismiss, and for as long as it applies, in Admin →
 * Project.
 *
 * The note shows only where the project's gates are off and the desk can
 * offer the upgrade. Dismissing it is remembered in this browser, per
 * project, and writes nothing in the project: a project that declines keeps
 * every file as it is.
 */
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { msg, systemMessage, useLocale } from '../../i18n'
import { Button, ButtonLink } from '../../ui/Button'
import { SettingsSection } from '../../ui/SettingsSection'
import { useFileListing } from '../../files/queries'
import { identityIsResolved, projectKey } from '../../shell/paneState'
import { readUpgrade, UPGRADE_KEY } from './client'
import { gatesSay } from './UpgradeView'
import styles from './Upgrade.module.css'

/** The browser's record that the note was dismissed for one project. */
export function dismissedKey(projectRoot: string): string {
  return `jpack-desk:gates-offer-dismissed:v1:${projectKey(projectRoot)}`
}

function readDismissed(key: string): boolean {
  try { return localStorage.getItem(key) === '1' } catch { return false }
}

/** The offer, read once for the note and the Admin card alike. */
function useOffer(enabled: boolean) {
  return useQuery({ queryKey: [...UPGRADE_KEY, true], queryFn: ({ signal }) => readUpgrade(true, signal), retry: false, enabled })
}

/** The note on Packs: shown until the owner dismisses it, or takes the upgrade. */
export function UpgradeNote() {
  useLocale()
  const root = useFileListing().data?.root
  const resolved = identityIsResolved(root)
  const key = resolved ? dismissedKey(root!) : ''
  const [dismissed, setDismissed] = useState<string[]>([])
  const hidden = !resolved || dismissed.includes(key) || readDismissed(key)
  const offer = useOffer(!hidden)
  if (hidden || offer.data?.state !== 'offer' || offer.data.gated) return null
  const dismiss = () => {
    try { localStorage.setItem(key, '1') } catch { /* Dismissed for this page, then. */ }
    setDismissed(keys => [...keys, key])
  }
  return <aside className={styles.note} aria-label={msg('Turn on this project’s gates')}>
    <p><strong>{msg('This project’s gates are off.')}</strong> {msg('Desk can hold its deciding runs to a reviewed set of packs and record each one. Nothing changes until you confirm, and the offer stays in Admin → Project.')}</p>
    <ButtonLink to="/packs/_upgrade">{msg('See what changes')}</ButtonLink>
    <Button variant="quiet" onClick={dismiss}>{msg('Dismiss')}</Button>
  </aside>
}

/** Admin → Project: where the offer stays available. */
export function ProjectGates() {
  useLocale()
  const offer = useOffer(true)
  const upgrade = offer.data
  return <SettingsSection title={msg('Gates')} variant="plain" description={msg('Whether this project holds deciding runs to its reviewed set of packs, and records them.')}>
    <div className={styles.card}>
      {offer.isPending ? <p role="status" className={styles.quiet}>{msg('Asking the runtime…')}</p>
        : offer.error ? <div role="alert"><p>{systemMessage(offer.error.message)}</p><Button onClick={() => void offer.refetch()} disabled={offer.isFetching}>{msg('Retry')}</Button></div>
          : upgrade?.state === 'unavailable' ? <p>{msg('Desk offers no upgrade here.')} {systemMessage(upgrade.reason ?? '')}</p>
            : upgrade && !upgrade.gated ? <>
              <p>{msg('This project’s gates are off: the runtime does not hold its deciding runs to a reviewed set, and records none.')}</p>
              <div><ButtonLink to="/packs/_upgrade">{msg('Review the upgrade')}</ButtonLink></div>
            </>
              : upgrade && <>
                <p>{gatesSay(upgrade)}</p>
                {upgrade.comparableFacts === 'off' && <div><ButtonLink to="/packs/_upgrade">{msg('Review turning requireComparableFacts on')}</ButtonLink></div>}
              </>}
      <p className={styles.quiet}><Link to="/help#gates">{msg('What each gate holds, and whom it binds')}</Link></p>
    </div>
  </SettingsSection>
}
