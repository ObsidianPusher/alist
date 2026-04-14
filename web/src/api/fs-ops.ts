import type { EmptyResp, RenameObj } from "~/types"
import { r } from "~/utils"

export const mkdirFs = (path: string): Promise<EmptyResp> => {
  return r.post("/fs/mkdir", { path })
}

export const renameFs = (path: string, name: string): Promise<EmptyResp> => {
  return r.post("/fs/rename", { path, name })
}

export const batchRenameFs = (
  srcDir: string,
  renameObjects: RenameObj[],
): Promise<EmptyResp> => {
  return r.post("/fs/batch_rename", {
    src_dir: srcDir,
    rename_objects: renameObjects,
  })
}

export const moveFs = (
  srcDir: string,
  dstDir: string,
  names: string[],
): Promise<EmptyResp> => {
  return r.post("/fs/move", { src_dir: srcDir, dst_dir: dstDir, names })
}

export const recursiveMoveFs = (
  srcDir: string,
  dstDir: string,
): Promise<EmptyResp> => {
  return r.post("/fs/recursive_move", { src_dir: srcDir, dst_dir: dstDir })
}

export const copyFs = (
  srcDir: string,
  dstDir: string,
  names: string[],
): Promise<EmptyResp> => {
  return r.post("/fs/copy", { src_dir: srcDir, dst_dir: dstDir, names })
}

export const removeFs = (dir: string, names: string[]): Promise<EmptyResp> => {
  return r.post("/fs/remove", { dir, names })
}

export const removeEmptyDirectoryFs = (srcDir: string): Promise<EmptyResp> => {
  return r.post("/fs/remove_empty_directory", { src_dir: srcDir })
}

export const createEmptyFileFs = (
  path: string,
  password: string,
): Promise<EmptyResp> => {
  return r.put("/fs/put", undefined, {
    headers: {
      "File-Path": encodeURIComponent(path),
      Password: password,
    },
  })
}
