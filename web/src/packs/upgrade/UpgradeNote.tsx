/**
 * The optional safeguards offer lives in the bottom status strip and remains
 * available in Admin → Decision safeguards. It never displaces page content.
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
import { PopoverClose } from '../../ui/Popover'
import { DeskStatusNotice } from '../../shell/DeskNotices'
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
  return useQuery({ queryKey: [...UPGRADE_KEY, true, false], queryFn: ({ signal }) => readUpgrade(true, false, signal), retry: false, enabled })
}

/** Optional setup, shown only while no connection or configuration issue takes priority. */
export function ProjectSafeguardsStatus() {
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
  return <DeskStatusNotice id="project-safeguards" tone="warning" priority="setup" label={msg('Project safeguards off')} title={msg('Project safeguards')}
    actions={<><PopoverClose><ButtonLink to="/packs/_upgrade">{msg('Review setup')}</ButtonLink></PopoverClose><Button variant="quiet" onClick={dismiss}>{msg('Dismiss')}</Button></>}>
    {msg('Require reviewed packs and keep a decision history for this project.')}
  </DeskStatusNotice>
}

/** Admin → Decision safeguards: where the offer stays available. */
export function ProjectGates() {
  useLocale()
  const offer = useOffer(true)
  const upgrade = offer.data
  return <SettingsSection title={msg('Decision safeguards')} level={2} variant="standalone" description={msg('This desk · Require reviewed packs and record decisions.')}>
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
                {upgrade.signingKey?.state === 'offered' && <div><ButtonLink to="/packs/_upgrade">{msg('Review signing this project’s decisions')}</ButtonLink></div>}
                {upgrade.signingKey?.state === 'unavailable' && <p className={styles.quiet}>{systemMessage(upgrade.signingKey.reason)}</p>}
              </>}
      <div><ButtonLink to="/packs/_review">{msg('Review and lock')}</ButtonLink></div>
      <p className={styles.quiet}><Link to="/help#gates">{msg('What each gate holds, and whom it binds')}</Link></p>
    </div>
  </SettingsSection>
}
