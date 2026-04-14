import mitt from "mitt"

type Events = {
  to: string
  gallery: string
  tool: string
  pathname: string
  "browser-objs-reset": undefined
}

export const bus = mitt<Events>()
