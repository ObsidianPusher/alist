import naturalSort from "typescript-natural-sort"
import { cookieStorage, createStorageSignal } from "@solid-primitives/storage"
import { createSignal } from "solid-js"
import { createStore, produce } from "solid-js/store"
import { Obj, StoreObj } from "~/types"
import { bus, log } from "~/utils"
import { local } from "./local_settings"

export enum State {
  Initial,
  FetchingObj,
  FetchingObjs,
  FetchingMore,
  Folder,
  File,
  NeedPassword,
}

const [browserStore, setBrowserStore] = createStore<{
  obj: Obj
  raw_url: string
  related: Obj[]
  objs: StoreObj[]
  total: number
  write?: boolean
  readme: string
  header: string
  provider: string
  state: State
  err: string
}>({
  obj: {} as Obj,
  raw_url: "",
  related: [],
  objs: [],
  total: 0,
  readme: "",
  header: "",
  provider: "",
  state: State.Initial,
  err: "",
})

const setObjs = (objs: Obj[]) => {
  bus.emit("browser-objs-reset")
  setBrowserStore("objs", objs)
  setBrowserStore("obj", "is_dir", true)
}

export const BrowserStore = {
  setObj: (obj: Obj) => setBrowserStore("obj", obj),
  setRawUrl: (raw_url: string) => setBrowserStore("raw_url", raw_url),
  setProvider: (provider: string) => setBrowserStore("provider", provider),
  setObjs,
  setTotal: (total: number) => setBrowserStore("total", total),
  setReadme: (readme: string) => setBrowserStore("readme", readme),
  setHeader: (header: string) => setBrowserStore("header", header),
  setRelated: (related: Obj[]) => setBrowserStore("related", related),
  setWrite: (write: boolean) => setBrowserStore("write", write),
  setState: (state: State) => setBrowserStore("state", state),
  setErr: (err: string) => setBrowserStore("err", err),
}

export const ObjStore = BrowserStore

export type OrderBy = "name" | "size" | "modified"

export const sortObjs = (orderBy: OrderBy, reverse?: boolean) => {
  log("sort:", orderBy, reverse)
  setBrowserStore(
    "objs",
    produce((objs) =>
      objs.sort((a, b) => {
        return (reverse ? -1 : 1) * naturalSort(a[orderBy], b[orderBy])
      }),
    ),
  )
}

export const appendObjs = (objs: Obj[]) => {
  setBrowserStore(
    "objs",
    produce((prev) => prev.push(...objs)),
  )
}

export type LayoutType = "list" | "grid" | "image"
const [pathname, setPathname] = createSignal<string>(location.pathname)
const layoutRecord: Record<string, LayoutType> = (() => {
  try {
    return JSON.parse(localStorage.getItem("layoutRecord") || "{}")
  } catch {
    return {}
  }
})()

bus.on("pathname", (p) => setPathname(p))
const [_layout, _setLayout] = createSignal<LayoutType>(
  layoutRecord[pathname()] || local["global_default_layout"],
)
export const layout = () => {
  const layout = layoutRecord[pathname()]
  _setLayout(layout || local["global_default_layout"])
  return _layout()
}
export const setLayout = (layout: LayoutType) => {
  layoutRecord[pathname()] = layout
  localStorage.setItem("layoutRecord", JSON.stringify(layoutRecord))
  _setLayout(layout)
}

export { browserStore as objStore }
export { setBrowserStore }

const [_password, _setPassword] = createSignal<string>(
  cookieStorage.getItem("browser-password") || "",
)
export { _password as password }
export const setPassword = (password: string) => {
  _setPassword(password)
  cookieStorage.setItem("browser-password", password)
}

const [_checkboxOpen, setCheckboxOpen] = createStorageSignal<string>(
  "checkbox-open",
  "false",
)
export const checkboxOpen = () => _checkboxOpen() === "true"

export const toggleCheckbox = () => {
  setCheckboxOpen(checkboxOpen() ? "false" : "true")
}
