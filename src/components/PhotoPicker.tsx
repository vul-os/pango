import { useEffect, useRef } from 'react'
import { CameraIcon, XIcon } from './icons'

/**
 * Client-side photo staging. The API has no upload/attachment endpoint yet —
 * `domain.Attachment` exists in the Go domain model (backend/internal/
 * domain/domain.go) but no route serves it (see api.go's route table). So
 * this stages files locally with previews and is explicit that nothing is
 * uploaded, rather than pretending to save them (docs/ARCHITECTURE.md §13:
 * "a feature that silently does nothing is not" an acceptable state).
 */
/**
 * One object URL per File, for as long as that File is staged. Held at module
 * scope (weakly) rather than in a ref so that reading it is not a render-time
 * ref access, and so the lookup is *idempotent*: asking twice for the same
 * File returns the same URL instead of allocating a second one. That is what
 * makes it safe to derive preview URLs during render, including under
 * StrictMode's deliberate double render.
 */
const urlForFile = new WeakMap<File, string>()

function previewUrl(file: File): string {
  let url = urlForFile.get(file)
  if (!url) {
    url = URL.createObjectURL(file)
    urlForFile.set(file, url)
  }
  return url
}

function release(file: File): void {
  const url = urlForFile.get(file)
  if (url) {
    URL.revokeObjectURL(url)
    urlForFile.delete(file)
  }
}

interface PhotoPickerProps {
  files: File[]
  onChange: (files: File[]) => void
}

export default function PhotoPicker({ files, onChange }: PhotoPickerProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  // Files this picker was showing as of the last commit. Only ever touched
  // from effects, never during render.
  const shownRef = useRef<File[]>([])
  // Typed via ReturnType rather than `number` because one of the toolchain's
  // own .d.ts files pulls in @types/node's ambient globals, which redeclare
  // the browser setTimeout/clearTimeout pair to return/accept NodeJS.Timeout
  // instead of number — this adapts to whichever one is actually active.
  const unmountRevoke = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  // Derived during render rather than published from an effect. The effect
  // version (setPreviews inside useEffect) left `previews` one render behind
  // `files`: straight after the parent removed a photo this component still
  // rendered the *old* preview list, so `previews[i]` no longer lined up with
  // `files[i]` and clicking remove in that frame dropped the wrong photo.
  const previews = files.map(previewUrl)

  // Free the URLs of files that have been unstaged. Runs after commit, so it
  // can never revoke a URL the live DOM still points at, and it has no cleanup
  // of its own, so re-running it is a no-op.
  useEffect(() => {
    const gone = shownRef.current.filter((f) => !files.includes(f))
    shownRef.current = files
    gone.forEach(release)
  }, [files])

  // Free whatever is left when the picker really goes away. StrictMode
  // double-invokes effects (setup -> cleanup -> setup) with no re-render in
  // between, so revoking straight from the cleanup would revoke the URLs the
  // remounted tree is still rendering and blank every preview in dev.
  // Deferring by a task, and cancelling it on re-setup, makes the dev-only
  // fake unmount a no-op while a real unmount still releases the URLs.
  useEffect(() => {
    clearTimeout(unmountRevoke.current)
    return () => {
      const doomed = shownRef.current
      unmountRevoke.current = setTimeout(() => doomed.forEach(release), 0)
    }
  }, [])

  function addFiles(fileList: FileList) {
    const next = [...files, ...Array.from(fileList).filter((f) => f.type.startsWith('image/'))]
    onChange(next)
  }

  function removeAt(i: number) {
    onChange(files.filter((_, idx) => idx !== i))
  }

  return (
    <div>
      <div
        role="button"
        tabIndex={0}
        onClick={() => inputRef.current?.click()}
        onKeyDown={(e) => e.key === 'Enter' && inputRef.current?.click()}
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault()
          addFiles(e.dataTransfer.files)
        }}
        className="flex cursor-pointer flex-col items-center justify-center gap-1.5 rounded-md border border-dashed border-line px-4 py-6 text-center hover:border-line-strong hover:bg-surface-sunk"
      >
        <CameraIcon width={22} height={22} className="text-ink-faint" />
        <p className="text-xs font-medium text-ink">Add photos</p>
        <p className="text-2xs text-ink-faint">Click or drop image files</p>
        <input
          ref={inputRef}
          type="file"
          accept="image/*"
          multiple
          className="hidden"
          onChange={(e) => e.target.files && addFiles(e.target.files)}
        />
      </div>

      {files.length > 0 && (
        <div className="mt-2.5 grid grid-cols-4 gap-2">
          {files.map((file, i) => (
            <div
              key={`${file.name}:${file.size}:${file.lastModified}:${i}`}
              className="group relative aspect-square overflow-hidden rounded-sm border border-line"
            >
              <img src={previews[i]} alt="" className="h-full w-full object-cover" />
              <button
                type="button"
                onClick={() => removeAt(i)}
                aria-label="Remove photo"
                className="absolute right-1 top-1 rounded-full bg-black/60 p-0.5 text-white opacity-0 transition-opacity group-hover:opacity-100"
              >
                <XIcon width={12} height={12} />
              </button>
            </div>
          ))}
        </div>
      )}

      <p className="mt-2 text-2xs text-ink-faint">
        {files.length > 0
          ? `${files.length} photo${files.length === 1 ? '' : 's'} staged on this device only — `
          : ''}
        photo upload isn&rsquo;t wired up on the backend yet, so nothing here is saved to the job.
      </p>
    </div>
  )
}
