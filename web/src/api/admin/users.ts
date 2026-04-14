import type { PEmptyResp, PPageResp, PResp, User } from "~/types"
import { r } from "~/utils"
import { updateProfile as updateProfileApi } from "~/api/account"

export const listUsers = (): PPageResp<User> => {
  return r.get("/admin/user/list")
}

export const getUser = (id: string | number): PResp<User> => {
  return r.get(`/admin/user/get?id=${id}`)
}

export const createUser = (user: User): PEmptyResp => {
  return r.post("/admin/user/create", user)
}

export const updateUser = (user: User): PEmptyResp => {
  return r.post("/admin/user/update", user)
}

export const saveUser = (id: string | number | undefined, user: User): PEmptyResp => {
  return id ? updateUser(user) : createUser(user)
}

export const deleteUser = (id: number): PEmptyResp => {
  return r.post(`/admin/user/delete?id=${id}`)
}

export const cancelUser2FA = (id: number): PEmptyResp => {
  return r.post(`/admin/user/cancel_2fa?id=${id}`)
}

export const updateMyProfile = (data: {
  username: string
  password?: string
  sso_id?: string
}): PEmptyResp => {
  return updateProfileApi(data)
}
