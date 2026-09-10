/**
 * The model this run uses, on the run.
 *
 * **A Select of the enabled set, with the default preselected.** It offers what
 * Admin enabled and nothing else: an id outside the set is one this desk is not
 * configured for, and a control that could reach one would be a request nobody
 * authorised. What it changes is this run and the tab's memory of the choice —
 * never the file.
 *
 * It renders where there is a set. With an empty one the surface around it says
 * what it already says about an endpoint with no model chosen, and there is no
 * picker: a menu with nothing in it is a control that refuses.
 */
import { Select } from '../ui/Select'
import type { PickedModel } from './pickedModel'

export function ModelPicker({
  picked,
  id,
  className
}: {
  picked: PickedModel
  /** The id this picker's own label points at. */
  id: string
  className?: string
}) {
  if (picked.models.length === 0) return null
  return (
    <span className={className}>
      <label htmlFor={id}>Model</label>{' '}
      <Select
        id={id}
        value={picked.model}
        onValueChange={picked.pick}
        options={picked.models.map((model) => ({ value: model, label: model }))}
      />
    </span>
  )
}
