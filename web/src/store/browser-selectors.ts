import { createMemo } from "solid-js"
import { objStore } from "./browser"

export const useCurrentObj = () => createMemo(() => objStore.obj)
export const useCurrentObjs = () => createMemo(() => objStore.objs)
export const useCurrentProvider = () => createMemo(() => objStore.provider)
export const useCurrentRawUrl = () => createMemo(() => objStore.raw_url)
export const useCurrentRelated = () => createMemo(() => objStore.related)
export const useCurrentState = () => createMemo(() => objStore.state)
export const useCurrentWrite = () => createMemo(() => objStore.write)
export const useCurrentTotal = () => createMemo(() => objStore.total)
export const useCurrentError = () => createMemo(() => objStore.err)
