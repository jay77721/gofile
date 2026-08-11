// API 封装:统一 JSON 响应处理(code!==0 抛错、401 清除会话)
import { reactive } from 'vue';

// 全局会话状态(登录态变更由 AuthView 驱动)
export const session = reactive({ username: '' });

export interface ApiError extends Error {
  code: number;
}

export function makeApiError(message: string, code: number): ApiError {
  const err = new Error(message) as ApiError;
  err.name = 'ApiError';
  err.code = code;
  return err;
}

async function parse(res: Response) {
  let data: { code: number; msg?: string; data?: unknown };
  try {
    data = await res.json();
  } catch {
    throw makeApiError('响应异常', -1);
  }
  if (data.code !== 0) {
    throw makeApiError(data.msg || '请求失败', data.code);
  }
  return data.data;
}

export async function api(path: string) {
  const res = await fetch(path, { credentials: 'include' });
  if (res.status === 401) {
    session.username = '';
    throw makeApiError('登录已过期，请重新登录', 1002);
  }
  return parse(res);
}

export async function apiPost(path: string, body: FormData | Record<string, string>) {
  const opts: RequestInit = { method: 'POST', credentials: 'include' };
  if (body instanceof FormData) {
    opts.body = body;
  } else {
    opts.headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
    opts.body = new URLSearchParams(body);
  }
  const res = await fetch(path, opts);
  if (res.status === 401) {
    session.username = '';
    throw makeApiError('登录已过期，请重新登录', 1002);
  }
  return parse(res);
}

// 通用 JSON 请求(POST/PUT/DELETE 用,AI 配置等结构化接口)
export async function apiJSON(path: string, method = 'POST', body?: unknown) {
  const opts: RequestInit = {
    method,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
  };
  if (body !== undefined) opts.body = JSON.stringify(body);
  const res = await fetch(path, opts);
  if (res.status === 401) {
    session.username = '';
    throw makeApiError('登录已过期，请重新登录', 1002);
  }
  return parse(res);
}

// 上传走 XMLHttpRequest(需要上传进度)
export function apiUpload(path: string, fd: FormData, onProgress?: (pct: number) => void) {
  return new Promise((resolve, reject) => {
    const x = new XMLHttpRequest();
    x.open('POST', path);
    x.withCredentials = true;
    x.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100));
    };
    x.onload = () => {
      try {
        const d = JSON.parse(x.responseText);
        d.code === 0 ? resolve(d.data) : reject(makeApiError(d.msg || '上传失败', d.code));
      } catch {
        reject(makeApiError('响应异常', -1));
      }
    };
    x.onerror = () => reject(makeApiError('网络错误', -1));
    x.onabort = () => reject(makeApiError('cancelled', -1));
    x.send(fd);
  });
}
