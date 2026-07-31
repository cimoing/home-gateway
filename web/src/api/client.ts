let unauthorizedHandler: (() => void) | undefined

export function setUnauthorizedHandler(handler: () => void) {
  unauthorizedHandler = handler
}

export async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (init?.body && !(init.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(url, { ...init, headers })
  if (response.status === 401) {
    unauthorizedHandler?.()
    throw new Error('登录已过期，请重新登录。')
  }
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(data.error || `请求失败（${response.status}）`)
  }
  return response.status === 204 ? (undefined as T) : ((await response.json()) as T)
}
