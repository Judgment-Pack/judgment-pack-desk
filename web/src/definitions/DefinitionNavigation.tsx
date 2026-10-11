import { Link, useNavigate } from 'react-router-dom'
import { useTabOverflow, TabOverflowMenu } from '../ui/TabOverflow'
import styles from '../ui/Tabs.module.css'

export function DefinitionNavigation({label, current, items}: {label: string; current: string; items: readonly {value: string; label: string; href: string}[]}) {
  const navigate = useNavigate()
  const {container, visible, measurements} = useTabOverflow(items, current, true)
  return <nav className={styles.navigation} aria-label={label}><div ref={container} className={styles.routeList}>
    {measurements}
    {items.filter(item => visible.has(item.value)).map(item => <Link className={styles.trigger} key={item.value} to={item.href} aria-current={current === item.value ? 'page' : undefined}>{item.label}</Link>)}
    <TabOverflowMenu items={items.filter(item => !visible.has(item.value))} onSelect={value => {const item=items.find(item => item.value===value);if(item)navigate(item.href)}}/>
  </div></nav>
}
