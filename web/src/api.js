// API 封装:统一 JSON 响应处理(code!==0 抛错、401 清除会话)
import { reactive } from 'vue'

// 全局会话状态(登录态变更由 AuthView 驱动)
export const session = reactive({ username: '' })

async function parse(res) {
  let data
  try { data = await res.json() } catch { throw new Error('响应异常') }
  if (data.code !== 0) {
    const err = new Error(data.msg || '请求失败')
    err.code = data.code
    throw err
  }
  return data.data
}

export async function api(path) {
  const res = await fetch(path, { credentials: 'include' })
  if (res.status === 401) { session.username = ''; throw new Error('登录已过期，请重新登录') }
  return parse(res)
}

export async function apiPost(path, body) {
  const opts = { method: 'POST', credentials: 'include' }
  if (body instanceof FormData) opts.body = body
  else {
    opts.headers = { 'Content-Type': 'application/x-www-form-urlencoded' }
    opts.body = new URLSearchParams(body)
  }
  const res = await fetch(path, opts)
  if (res.status === 401) { session.username = ''; throw new Error('登录已过期，请重新登录') }
  return parse(res)
}

// 通用 JSON 请求(POST/PUT/DELETE 用,AI 配置等结构化接口)
export async function apiJSON(path, method = 'POST', body) {
  const opts = { method, credentials: 'include', headers: { 'Content-Type': 'application/json' } }
  if (body !== undefined) opts.body = JSON.stringify(body)
  const res = await fetch(path, opts)
  if (res.status === 401) { session.username = ''; throw new Error('登录已过期，请重新登录') }
  return parse(res)
}

// 上传走 XMLHttpRequest(需要上传进度)
export function apiUpload(path, fd, onProgress) {
  return new Promise((resolve, reject) => {
    const x = new XMLHttpRequest()
    x.open('POST', path)
    x.withCredentials = true
    x.upload.onprogress = (e) => { if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100)) }
    x.onload = () => {
      try {
        const d = JSON.parse(x.responseText)
        d.code === 0 ? resolve(d.data) : reject(new Error(d.msg || '上传失败'))
      } catch { reject(new Error('响应异常')) }
    }
    x.onerror = () => reject(new Error('网络错误'))
    x.onabort = () => reject(new Error('cancelled'))
    x.send(fd)
  })
}
