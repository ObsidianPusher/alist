import { Center, Icon } from "@hope-ui/solid"
import { BsPlayCircleFill } from "solid-icons/bs"
import { createSignal, JSXElement, onCleanup, onMount, Show } from "solid-js"

const MAX_THUMB_LOADING = 4
let activeThumbLoading = 0
const thumbQueue: Array<() => void> = []

const runNextThumb = () => {
  while (activeThumbLoading < MAX_THUMB_LOADING && thumbQueue.length > 0) {
    const fn = thumbQueue.shift()
    if (!fn) break
    activeThumbLoading += 1
    fn()
  }
}

const acquireThumbSlot = (fn: () => void) => {
  thumbQueue.push(fn)
  runNextThumb()
}

const releaseThumbSlot = () => {
  if (activeThumbLoading > 0) activeThumbLoading -= 1
  runNextThumb()
}

export const VideoThumbnail = (props: {
  src: string
  alt?: string
  fallbackErr?: JSXElement
}) => {
  const [ready, setReady] = createSignal(false)
  const [err, setErr] = createSignal(false)
  const [canLoad, setCanLoad] = createSignal(false)
  let rootEl: HTMLDivElement | undefined
  let released = false

  onMount(() => {
    if (!rootEl) return
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          io.disconnect()
          acquireThumbSlot(() => setCanLoad(true))
        }
      },
      { rootMargin: "200px 0px" },
    )
    io.observe(rootEl)
    onCleanup(() => io.disconnect())
  })

  onCleanup(() => {
    if (canLoad() && !released && !ready()) {
      released = true
      releaseThumbSlot()
    }
  })

  return (
    <Show when={!err()} fallback={props.fallbackErr}>
      <div
        ref={rootEl}
        style={{
          width: "100%",
          height: "100%",
          position: "relative",
          display: "flex",
          "align-items": "center",
          "justify-content": "center",
        }}
      >
        <video
          muted
          playsinline
          preload={canLoad() ? "metadata" : "none"}
          src={canLoad() ? props.src : undefined}
          title={props.alt}
          style={{
            "max-width": "100%",
            "max-height": "100%",
            "border-radius": "0.5rem",
            "box-shadow": "var(--hope-shadows-md)",
            visibility: ready() ? "visible" : "hidden",
          }}
          onLoadedMetadata={(e) => {
            const v = e.currentTarget
            if (!Number.isFinite(v.duration) || v.duration <= 0) return
            const target = Math.min(1, Math.max(0.1, v.duration * 0.01))
            if (target > 0) {
              try {
                v.currentTime = target
              } catch {
                // no-op
              }
            }
          }}
          onLoadedData={() => {
            setReady(true)
            if (!released) {
              released = true
              releaseThumbSlot()
            }
          }}
          onError={() => {
            setErr(true)
            if (!released) {
              released = true
              releaseThumbSlot()
            }
          }}
        />
        <Show when={!ready() && !err()}>
          <Center
            position="absolute"
            top="0"
            left="0"
            right="0"
            bottom="0"
            pointerEvents="none"
          >
            <Icon as={BsPlayCircleFill} boxSize="$8" color="$neutral9" opacity={0.85} />
          </Center>
        </Show>
      </div>
    </Show>
  )
}
