import { Children, cloneElement, type ComponentPropsWithRef, type ReactElement, type ReactNode, type Ref } from 'react'
import { DropdownMenu as Primitive } from 'radix-ui'
import { IconCheck, IconChevronRight, IconChevronDown, IconMore } from '../shell/icons'
import { Button } from './Button'
import { Tooltip } from './Tooltip'
import styles from './Menu.module.css'

const classes = (...values: (string | undefined | false)[]) => values.filter(Boolean).join(' ')
type Row = { icon?: ReactNode; description?: ReactNode; shortcut?: ReactNode }
type ItemProps = ComponentPropsWithRef<typeof Primitive.Item> & Row & { destructive?: boolean }

/** The label, icon/check and trailing shortcut keep the same columns in every menu. */
function rowContent({ children, asChild, icon, description, shortcut }: Row & { children?: ReactNode; asChild?: boolean }) {
  const child = asChild ? Children.only(children) as ReactElement<{ children?: ReactNode }> : undefined
  const label = child ? child.props.children : children
  const content = <>
    <span className={styles.icon} aria-hidden="true">{icon}</span>
    <span className={styles.copy}><span>{label}</span>{description && <span className={styles.description}>{description}</span>}</span>
    {shortcut && <span className={styles.shortcut} aria-hidden="true">{shortcut}</span>}
  </>
  return child ? cloneElement(child, undefined, content) : content
}

function Item({ children, icon, description, shortcut, className, onSelect, destructive, ...props }: ItemProps) {
  const unavailable = props['aria-disabled'] === true || props['aria-disabled'] === 'true'
  return <Primitive.Item {...props} data-destructive={destructive || undefined} className={classes(styles.item, className)} onSelect={event => {
    // An explained unavailable command can retain keyboard focus for its tooltip.
    if (unavailable) { event.preventDefault(); return }
    onSelect?.(event)
  }}>{rowContent({ children, asChild: props.asChild, icon, description, shortcut })}</Primitive.Item>
}
function RadioItem({ children, description, className, ...props }: ComponentPropsWithRef<typeof Primitive.RadioItem> & Pick<Row, 'description'>) {
  return <Primitive.RadioItem {...props} className={classes(styles.item, className)}>{rowContent({children, asChild: props.asChild, description, icon:<Primitive.ItemIndicator><IconCheck/></Primitive.ItemIndicator>})}</Primitive.RadioItem>
}
function CheckboxItem({ children, description, className, ...props }: ComponentPropsWithRef<typeof Primitive.CheckboxItem> & Pick<Row, 'description'>) {
  return <Primitive.CheckboxItem {...props} className={classes(styles.item, className)}>{rowContent({children, asChild: props.asChild, description, icon:<Primitive.ItemIndicator><IconCheck/></Primitive.ItemIndicator>})}</Primitive.CheckboxItem>
}
function SubTrigger({ children, icon, className, ...props }: ComponentPropsWithRef<typeof Primitive.SubTrigger> & Pick<Row, 'icon'>) {
  return <Primitive.SubTrigger {...props} className={classes(styles.item, className)}>{rowContent({children, asChild: props.asChild, icon, shortcut:<IconChevronRight/>})}</Primitive.SubTrigger>
}
function Content({ className, sideOffset = 6, collisionPadding = 16, ...props }: ComponentPropsWithRef<typeof Primitive.Content>) {
  return <Primitive.Content {...props} sideOffset={sideOffset} collisionPadding={collisionPadding} className={classes(styles.content, className)}/>
}
function SubContent({ className, sideOffset = 6, collisionPadding = 16, ...props }: ComponentPropsWithRef<typeof Primitive.SubContent>) {
  return <Primitive.SubContent {...props} sideOffset={sideOffset} collisionPadding={collisionPadding} className={classes(styles.content, className)}/>
}
function Label({ className, ...props }: ComponentPropsWithRef<typeof Primitive.Label>) {
  return <Primitive.Label {...props} className={classes(styles.label, className)}/>
}
function Separator({ className, ...props }: ComponentPropsWithRef<typeof Primitive.Separator>) {
  return <Primitive.Separator {...props} className={classes(styles.separator, className)}/>
}

/** Radix owns keyboard navigation, focus, typeahead and portals; Desk owns presentation. */
export const Menu = { ...Primitive, Content, SubContent, Item, RadioItem, CheckboxItem, SubTrigger, Label, Separator }

/** A toolbar filter or sort picker; form fields continue to use Select. */
export function MenuChoice({ label, value, options, onValueChange, triggerLabel, triggerClassName }: {
  label: string
  value: string
  options: readonly { value: string; label: string; disabled?: boolean }[]
  onValueChange: (value: string) => void
  triggerLabel?: string
  triggerClassName?: string
}) {
  return <Menu.Root>
    <Menu.Trigger asChild><Button variant="quiet" className={classes(styles.choiceTrigger, triggerClassName)} aria-label={label}><span>{triggerLabel ?? options.find(option => option.value === value)?.label ?? label}</span><IconChevronDown/></Button></Menu.Trigger>
    <Menu.Portal><Menu.Content align="end" aria-label={label}>
      <Menu.RadioGroup value={value} onValueChange={onValueChange} aria-label={label}>
        {options.map(option => <Menu.RadioItem key={option.value} value={option.value} disabled={option.disabled}>{option.label}</Menu.RadioItem>)}
      </Menu.RadioGroup>
    </Menu.Content></Menu.Portal>
  </Menu.Root>
}


/** Consistent document overflow trigger; commands own any dialog focus handoff. */
export function OptionsMenu({ label, children, triggerRef, onOpenChange, onCloseAutoFocus }: {
  label: string
  children: ReactNode
  triggerRef?: Ref<HTMLButtonElement>
  onOpenChange?: (open: boolean) => void
  onCloseAutoFocus?: ComponentPropsWithRef<typeof Primitive.Content>['onCloseAutoFocus']
}) {
  return <Menu.Root onOpenChange={onOpenChange}>
    <Tooltip content={label}><Menu.Trigger asChild><Button ref={triggerRef} size="icon" variant="quiet" aria-label={label}><IconMore/></Button></Menu.Trigger></Tooltip>
    <Menu.Portal><Menu.Content align="end" onCloseAutoFocus={onCloseAutoFocus}>{children}</Menu.Content></Menu.Portal>
  </Menu.Root>
}
