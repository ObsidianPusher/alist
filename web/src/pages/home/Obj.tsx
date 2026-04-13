import { Button, HStack, useColorModeValue, VStack } from "@hope-ui/solid"
import {
  Suspense,
  Switch,
  Match,
  lazy,
  createEffect,
  on,
  createSignal,
  createMemo,
} from "solid-js"
import { FullLoading, Error } from "~/components"
import { resetGlobalPage, useObjTitle, usePath, useRouter, useT } from "~/hooks"
import { objStore, recordScroll, /*layout,*/ State } from "~/store"

const Folder = lazy(() => import("./folder/Folder"))
const File = lazy(() => import("./file/File"))
const Password = lazy(() => import("./Password"))
const Favorites = lazy(() => import("./favorites/Favorites"))
const LocalBrowser = lazy(() => import("./local-browser/Browser"))
// const ListSkeleton = lazy(() => import("./Folder/ListSkeleton"));
// const GridSkeleton = lazy(() => import("./Folder/GridSkeleton"));

const [objBoxRef, setObjBoxRef] = createSignal<HTMLDivElement>()
export { objBoxRef }

let first = true
export const Obj = () => {
  const cardBg = useColorModeValue("white", "$neutral3")
  const { pathname, searchParams, setSearchParams } = useRouter()
  const { handlePathChange } = usePath()
  const t = useT()
  const isFavoritesView = createMemo(() => searchParams.view === "favorites")
  const isLocalBrowserView = createMemo(() => searchParams.view === "local_browser")
  let lastPathname = pathname()
  createEffect(
    on(pathname, (pathname) => {
      useObjTitle()
      if (!first) {
        resetGlobalPage()
      }
      first = false
      recordScroll(lastPathname, window.scrollY)
      handlePathChange(pathname)
      lastPathname = pathname
    }),
  )
  return (
    <VStack
      ref={(el: HTMLDivElement) => setObjBoxRef(el)}
      class="obj-box"
      w="$full"
      rounded="$xl"
      bgColor={cardBg()}
      p="$2"
      shadow="$lg"
      spacing="$2"
    >
      <HStack w="$full" justifyContent="space-between" spacing="$2">
        <HStack spacing="$2">
          <Button
            size="sm"
            variant={isFavoritesView() || isLocalBrowserView() ? "outline" : "solid"}
            onClick={() => setSearchParams({ view: undefined })}
          >
            {t("manage.sidemenu.home")}
          </Button>
          <Button
            size="sm"
            colorScheme="danger"
            variant={isFavoritesView() ? "solid" : "outline"}
            onClick={() => setSearchParams({ view: "favorites" })}
          >
            {t("home.favorites.tab")}
          </Button>
          <Button
            size="sm"
            colorScheme="accent"
            variant={isLocalBrowserView() ? "solid" : "outline"}
            onClick={() => setSearchParams({ view: "local_browser" })}
          >
            {t("home.local_browser.tab")}
          </Button>
        </HStack>
      </HStack>
      <Suspense fallback={<FullLoading />}>
        <Switch>
          <Match when={isFavoritesView()}>
            <Favorites />
          </Match>
          <Match when={isLocalBrowserView()}>
            <LocalBrowser />
          </Match>
          <Match when={objStore.err}>
            <Error msg={objStore.err} disableColor />
          </Match>
          <Match
            when={[State.FetchingObj, State.FetchingObjs].includes(
              objStore.state,
            )}
          >
            <FullLoading />
            {/* <Show when={layout() === "list"} fallback={<GridSkeleton />}>
              <ListSkeleton />
            </Show> */}
          </Match>
          <Match when={objStore.state === State.NeedPassword}>
            <Password />
          </Match>
          <Match
            when={[State.Folder, State.FetchingMore].includes(objStore.state)}
          >
            <Folder />
          </Match>
          <Match when={objStore.state === State.File}>
            <File />
          </Match>
        </Switch>
      </Suspense>
    </VStack>
  )
}
