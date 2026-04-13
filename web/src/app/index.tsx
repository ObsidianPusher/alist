import { HopeProvider, NotificationsProvider } from "@hope-ui/solid"
import { I18nContext } from "@solid-primitives/i18n"
import { ErrorBoundary, Suspense } from "solid-js"
import { Error, FullScreenLoading } from "~/components"
import App from "./App"
import { i18n } from "./i18n"
import { globalStyles, theme } from "./theme"

let domCompatInstalled = false
let domRecoverWindowStart = 0
let domRecoverAttempts = 0

const isRecoverableDomError = (err: unknown) => {
  const msg =
    err instanceof Error ? err.message : typeof err === "string" ? err : String(err)
  return (
    msg.includes("Failed to execute 'insertBefore'") ||
    msg.includes("Failed to execute 'removeChild'") ||
    msg.includes("is not a child of this node")
  )
}

const canAutoRecoverDomError = () => {
  const now = Date.now()
  if (now-domRecoverWindowStart > 8000) {
    domRecoverWindowStart = now
    domRecoverAttempts = 0
  }
  if (domRecoverAttempts >= 2) return false
  domRecoverAttempts++
  return true
}

const installDomCompatGuards = () => {
  if (domCompatInstalled || typeof window === "undefined") return
  domCompatInstalled = true
  const proto = Node.prototype as Node & {
    insertBefore: (newNode: Node, referenceNode: Node | null) => Node
    removeChild: (child: Node) => Node
  }
  const rawInsertBefore = proto.insertBefore
  const rawRemoveChild = proto.removeChild
  proto.insertBefore = function (newNode: Node, referenceNode: Node | null) {
    if (referenceNode && referenceNode.parentNode !== this) {
      // Some mobile browsers/extensions mutate DOM unexpectedly.
      // Fallback to append to avoid fatal NotFoundError.
      return this.appendChild(newNode)
    }
    try {
      return rawInsertBefore.call(this, newNode, referenceNode)
    } catch (err) {
      if ((err as Error)?.name === "NotFoundError") {
        return this.appendChild(newNode)
      }
      throw err
    }
  }
  proto.removeChild = function (child: Node) {
    if (!child || child.parentNode !== this) {
      return child
    }
    try {
      return rawRemoveChild.call(this, child)
    } catch (err) {
      if ((err as Error)?.name === "NotFoundError") {
        return child
      }
      throw err
    }
  }
}

installDomCompatGuards()

const Index = () => {
  installDomCompatGuards()
  globalStyles()
  return (
    <HopeProvider config={theme}>
      <ErrorBoundary
        fallback={(err, reset) => {
          if (isRecoverableDomError(err) && canAutoRecoverDomError()) {
            queueMicrotask(() => reset())
            return <FullScreenLoading />
          }
          console.error("error", err)
          const message =
            err instanceof Error ? err.message : typeof err === "string" ? err : String(err)
          return <Error msg={`System error: ${message}`} h="100vh" />
        }}
      >
        <I18nContext.Provider value={i18n}>
          <NotificationsProvider duration={3000}>
            <Suspense fallback={<FullScreenLoading />}>
              <App />
            </Suspense>
          </NotificationsProvider>
        </I18nContext.Provider>
      </ErrorBoundary>
    </HopeProvider>
  )
}

export { Index }
