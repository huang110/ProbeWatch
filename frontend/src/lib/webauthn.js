import { fetchCsrfToken } from './api'

/**
 * Check if the browser supports WebAuthn / Passkeys.
 */
export function isWebAuthnSupported() {
  return typeof window !== 'undefined' &&
    Boolean(window.PublicKeyCredential && navigator.credentials && navigator.credentials.create)
}

/**
 * Convert an ArrayBuffer to a URL-safe Base64 string.
 */
export function bufferToBase64URL(buffer) {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return btoa(binary)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')
}

/**
 * Convert a URL-safe Base64 string to an ArrayBuffer.
 */
export function base64URLToBuffer(base64url) {
  let str = (base64url || '').replace(/-/g, '+').replace(/_/g, '/')
  while (str.length % 4 !== 0) {
    str += '='
  }
  const binary = atob(str)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes.buffer
}

/**
 * Start Passkey registration (enrollment).
 */
export async function registerPasskey(name = '我的通行密钥') {
  if (!isWebAuthnSupported()) {
    throw new Error('当前浏览器或环境不支持通行密钥 (WebAuthn)')
  }

  // 1. Begin registration
  const csrfToken = await fetchCsrfToken()
  const beginRes = await fetch('/api/webauthn/register/begin', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrfToken,
    },
    credentials: 'same-origin',
    body: JSON.stringify({}),
  })

  if (!beginRes.ok) {
    const errJson = await beginRes.json().catch(() => ({}))
    throw new Error(errJson.error || `注册通行密钥请求失败 (${beginRes.status})`)
  }

  const { challenge_id, publicKey: pkOptions } = await beginRes.json()

  // 2. Decode options for navigator.credentials.create
  const creationOptions = {
    ...pkOptions,
    challenge: base64URLToBuffer(pkOptions.challenge),
    user: {
      ...pkOptions.user,
      id: base64URLToBuffer(pkOptions.user.id),
    },
    excludeCredentials: (pkOptions.excludeCredentials || []).map((cred) => ({
      ...cred,
      id: base64URLToBuffer(cred.id),
    })),
  }

  // 3. Prompt user for biometric / security key
  const credential = await navigator.credentials.create({
    publicKey: creationOptions,
  })

  if (!credential) {
    throw new Error('未创建有效的通行密钥凭证')
  }

  // 4. Finish registration
  const freshCsrf = await fetchCsrfToken()
  const finishRes = await fetch('/api/webauthn/register/finish', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': freshCsrf,
    },
    credentials: 'same-origin',
    body: JSON.stringify({
      challenge_id,
      name: name.trim() || '我的通行密钥',
      id: credential.id,
      rawId: bufferToBase64URL(credential.rawId),
      type: credential.type,
      response: {
        clientDataJSON: bufferToBase64URL(credential.response.clientDataJSON),
        attestationObject: bufferToBase64URL(credential.response.attestationObject),
      },
    }),
  })

  if (!finishRes.ok) {
    const errJson = await finishRes.json().catch(() => ({}))
    throw new Error(errJson.error || `保存通行密钥失败 (${finishRes.status})`)
  }

  return await finishRes.json()
}

/**
 * Start Passkey authentication (login).
 */
export async function loginWithPasskey() {
  if (!isWebAuthnSupported()) {
    throw new Error('当前浏览器不支持通行密钥登录')
  }

  // 1. Begin login
  const beginRes = await fetch('/api/webauthn/login/begin', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({}),
  })

  if (!beginRes.ok) {
    const errJson = await beginRes.json().catch(() => ({}))
    throw new Error(errJson.error || `发起通行密钥认证失败 (${beginRes.status})`)
  }

  const { challenge_id, publicKey: pkOptions } = await beginRes.json()

  // 2. Decode options for navigator.credentials.get
  const requestOptions = {
    ...pkOptions,
    challenge: base64URLToBuffer(pkOptions.challenge),
  }
  if (Array.isArray(pkOptions.allowCredentials)) {
    requestOptions.allowCredentials = pkOptions.allowCredentials.map((cred) => ({
      ...cred,
      id: base64URLToBuffer(cred.id),
    }))
  }

  // 3. Prompt user for biometric / security key
  const assertion = await navigator.credentials.get({
    publicKey: requestOptions,
  })

  if (!assertion) {
    throw new Error('未提供有效认证凭证')
  }

  // 4. Finish login
  const finishRes = await fetch('/api/webauthn/login/finish', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({
      challenge_id,
      id: assertion.id,
      rawId: bufferToBase64URL(assertion.rawId),
      type: assertion.type,
      response: {
        clientDataJSON: bufferToBase64URL(assertion.response.clientDataJSON),
        authenticatorData: bufferToBase64URL(assertion.response.authenticatorData),
        signature: bufferToBase64URL(assertion.response.signature),
        userHandle: assertion.response.userHandle ? bufferToBase64URL(assertion.response.userHandle) : undefined,
      },
    }),
  })

  if (!finishRes.ok) {
    const errJson = await finishRes.json().catch(() => ({}))
    throw new Error(errJson.error || `通行密钥认证失败 (${finishRes.status})`)
  }

  return await finishRes.json()
}

/**
 * Fetch all registered passkeys for the current user.
 */
export async function listPasskeys() {
  const res = await fetch('/api/webauthn/credentials', {
    credentials: 'same-origin',
  })
  if (!res.ok) {
    throw new Error(`获取通行密钥列表失败 (${res.status})`)
  }
  return await res.json()
}

/**
 * Delete a passkey by ID.
 */
export async function deletePasskey(id) {
  const csrfToken = await fetchCsrfToken()
  const res = await fetch(`/api/webauthn/credentials/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrfToken,
    },
    credentials: 'same-origin',
  })
  if (!res.ok) {
    const errJson = await res.json().catch(() => ({}))
    throw new Error(errJson.error || `删除通行密钥失败 (${res.status})`)
  }
  return true
}

/**
 * Rename a passkey.
 */
export async function renamePasskey(id, name) {
  const csrfToken = await fetchCsrfToken()
  const res = await fetch(`/api/webauthn/credentials/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrfToken,
    },
    credentials: 'same-origin',
    body: JSON.stringify({ name }),
  })
  if (!res.ok) {
    const errJson = await res.json().catch(() => ({}))
    throw new Error(errJson.error || `重命名通行密钥失败 (${res.status})`)
  }
  return true
}
