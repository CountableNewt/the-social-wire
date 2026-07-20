import { BACKFILL_COLLECTION_OPTIONS } from "@/lib/backfill-collections"

export function BackfillCollectionSelector({
  onValueChange,
  value,
}: {
  onValueChange: (value: string[]) => void
  value: readonly string[]
}) {
  const options = Array.from(new Set([...BACKFILL_COLLECTION_OPTIONS, ...value]))

  return (
    <fieldset className="grid gap-1.5 rounded-md border p-2">
      <legend className="sr-only">Collection Filters</legend>
      {options.map((collection) => {
        const checked = value.includes(collection)
        return (
          <label key={collection} className="flex items-center gap-2 text-[10px]">
            <input
              type="checkbox"
              checked={checked}
              onChange={() => {
                onValueChange(
                  checked
                    ? value.filter((item) => item !== collection)
                    : [...value, collection],
                )
              }}
            />
            <span className="break-all font-mono">{collection}</span>
          </label>
        )
      })}
      {value.length === 0 ? (
        <p role="alert" className="mt-1 text-[10px] text-destructive">
          Scope unknown. Select at least one collection before running a dry-run.
        </p>
      ) : null}
    </fieldset>
  )
}
