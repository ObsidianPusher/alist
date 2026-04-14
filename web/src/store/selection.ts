import { createSignal } from "solid-js"
import { produce } from "solid-js/store"
import { bus } from "~/utils"
import { keyPressed } from "./key-event"
import { objStore, setBrowserStore } from "./browser"

const [selectedNum, setSelectedNum] = createSignal(0)

let lastChecked = {
  index: -1,
  selected: false,
}

export const resetSelection = () => {
  setSelectedNum(0)
  lastChecked = { index: -1, selected: false }
}

bus.off("browser-objs-reset", resetSelection)
bus.on("browser-objs-reset", resetSelection)

export const selectIndex = (index: number, checked: boolean, one?: boolean) => {
  if (
    keyPressed["Shift"] &&
    lastChecked.index !== -1 &&
    lastChecked.selected === checked
  ) {
    const start = Math.min(lastChecked.index, index)
    const end = Math.max(lastChecked.index, index)
    const curCheckedNum = objStore.objs
      .slice(start, end + 1)
      .filter((o) => o.selected).length

    setBrowserStore("objs", { from: start, to: end }, () => ({
      selected: checked,
    }))
    const newSelectedNum =
      selectedNum() - curCheckedNum + (checked ? end - start + 1 : 0)
    setSelectedNum(newSelectedNum)
  } else {
    setBrowserStore(
      "objs",
      index,
      produce((obj: { selected?: boolean }) => {
        if (obj.selected !== checked) {
          setSelectedNum(checked ? selectedNum() + 1 : selectedNum() - 1)
        }
        obj.selected = checked
      }),
    )
  }
  lastChecked = { index, selected: checked }
  one && setSelectedNum(checked ? 1 : 0)
}

export const selectAll = (checked: boolean) => {
  setSelectedNum(checked ? objStore.objs.length : 0)
  setBrowserStore("objs", {}, (obj: { selected?: boolean }) => ({
    selected: checked,
  }))
}

export const selectedObjs = () => objStore.objs.filter((obj) => obj.selected)
export const allChecked = () => objStore.objs.length === selectedNum()
export const oneChecked = () => selectedNum() === 1
export const haveSelected = () => selectedNum() > 0
export const isIndeterminate = () =>
  selectedNum() > 0 && selectedNum() < objStore.objs.length
