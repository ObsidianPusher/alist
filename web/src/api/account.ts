import type { RegistrationPublicKeyCredential } from "@github/webauthn-json/browser-ponyfill"
import {
  type Generate2FA,
  type PEmptyResp,
  type PResp,
  type WebauthnCredential,
  type WebauthnRegistrationSession,
} from "~/types"
import { r } from "~/utils"

export type UpdateProfilePayload = {
  username: string
  password?: string
  sso_id?: string
}

export type Verify2FAPayload = {
  code: string
  secret?: string
}

export const updateProfile = (data: UpdateProfilePayload): PEmptyResp => {
  return r.post("/me/update", data)
}

export const listWebauthnCredentials = (): PResp<WebauthnCredential[]> => {
  return r.get("/authn/getcredentials")
}

export const beginWebauthnRegistration = (): PResp<WebauthnRegistrationSession> => {
  return r.get("/authn/webauthn_begin_registration")
}

export const finishWebauthnRegistration = (
  session: string,
  credentials: RegistrationPublicKeyCredential,
): PEmptyResp => {
  return r.post("/authn/webauthn_finish_registration", JSON.stringify(credentials), {
    headers: {
      session,
    },
  })
}

export const deleteWebauthnCredential = (id: string): PEmptyResp => {
  return r.post("/authn/delete_authn", { id })
}

export const generate2FA = (): PResp<Generate2FA> => {
  return r.post("/auth/2fa/generate")
}

export const verify2FA = (data: Verify2FAPayload): PEmptyResp => {
  return r.post("/auth/2fa/verify", data)
}
