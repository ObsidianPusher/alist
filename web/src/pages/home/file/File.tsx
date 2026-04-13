import { HStack, VStack } from "@hope-ui/solid"
import { createEffect, createMemo, createSignal, Show, Suspense } from "solid-js"
import { Dynamic } from "solid-js/web"
import { FullLoading, SelectWrapper } from "~/components"
import { objStore } from "~/store"
import { Download } from "../previews/download"
import { getPreviews } from "../previews"

const File = () => {
  const previews = createMemo(() => {
    return getPreviews({ ...objStore.obj, provider: objStore.provider })
  })
  const [curName, setCurName] = createSignal("")
  createEffect(() => {
    const list = previews()
    if (!list.length) {
      setCurName("")
      return
    }
    const next =
      list.find((item) => item.name === curName())?.name ?? list[0].name
    setCurName(next)
  })
  const cur = createMemo(
    () => previews().find((item) => item.name === curName()) ?? previews()[0],
  )
  return (
    <Show when={previews().length > 1} fallback={<Download />}>
      <VStack w="$full" spacing="$2">
        <HStack w="$full" spacing="$2" direction={{ "@initial": "column", "@md": "row" }} alignItems="stretch">
          <SelectWrapper
            alwaysShowBorder
            value={cur()?.name}
            onChange={(name) => {
              setCurName(name)
            }}
            options={previews().map((item) => ({ value: item.name }))}
          />
        </HStack>
        <Suspense fallback={<FullLoading />}>
          <Dynamic component={cur()?.component ?? Download} />
        </Suspense>
      </VStack>
    </Show>
  )
}

export default File
