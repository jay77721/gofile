// API 封装:统一 JSON 响应处理(code!==0 抛错、401 清除会话)
import { reactive } from 'vue';

// 全局会话状态(登录态变更由 AuthView 驱动)
export interface SessionState {
  username: string;
}

export const session = reactive<SessionState>({ username: '' });

export interface ApiError extends Error {
  code: number;
}

export function makeApiError(message: string, code: number): ApiError {
  const err = new Error(message) as ApiError;
  err.name = 'ApiError';
  err.code = code;
  return err;
}

export interface ApiResponse<T = unknown> {
  code: number;
  msg?: string;
  data?: T;
}

async function parse<T>(res: Response): Promise<T> {
  let data: ApiResponse<T>;
  try {
    data = await res.json();
  } catch {
    throw makeApiError('响应异常', -1);
  }
  if (data.code !== 0) {
    throw makeApiError(data.msg || '请求失败', data.code);
  }
  return data.data as T;
}

export async function api<T = unknown>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { credentials: 'include', signal });
  if (res.status === 401) {
    session.username = '';
    throw makeApiError('登录已过期，请重新登录', 1002);
  }
  return parse<T>(res);
}

export async function apiPost<T = unknown>(
  path: string,
  body: FormData | Record<string, string>,
  signal?: AbortSignal,
): Promise<T> {
  const opts: RequestInit = { method: 'POST', credentials: 'include', signal };
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
  return parse<T>(res);
}

// 通用 JSON 请求(POST/PUT/DELETE 用,AI 配置等结构化接口)
export async function apiJSON<T = unknown>(
  path: string,
  method = 'POST',
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const opts: RequestInit = {
    method,
    credentials: 'include',
    signal,
    headers: { 'Content-Type': 'application/json' },
  };
  if (body !== undefined) opts.body = JSON.stringify(body);
  const res = await fetch(path, opts);
  if (res.status === 401) {
    session.username = '';
    throw makeApiError('登录已过期，请重新登录', 1002);
  }
  return parse<T>(res);
}

// 上传走 XMLHttpRequest(需要上传进度)
export function apiUpload<T = unknown>(
  path: string,
  fd: FormData,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const x = new XMLHttpRequest();
    const onSignalAbort = () => x.abort();
    const cleanup = () => signal?.removeEventListener('abort', onSignalAbort);
    x.open('POST', path);
    x.withCredentials = true;
    x.upload.onprogress = (e: ProgressEvent) => {
      if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100));
    };
    x.onload = () => {
      cleanup();
      if (x.status === 401) {
        session.username = '';
        reject(makeApiError('登录已过期，请重新登录', 1002));
        return;
      }
      if (x.status < 200 || x.status >= 300) {
        reject(makeApiError(`请求失败 (${x.status})`, -1));
        return;
      }
      try {
        const d: ApiResponse<T> = JSON.parse(x.responseText);
        if (d.code === 0) {
          resolve(d.data as T);
        } else {
          reject(makeApiError(d.msg || '上传失败', d.code));
        }
      } catch {
        reject(makeApiError('响应异常', -1));
      }
    };
    x.onerror = () => { cleanup(); reject(makeApiError('网络错误', -1)); };
    x.onabort = () => { cleanup(); reject(makeApiError('cancelled', -1)); };
    if (signal) {
      if (signal.aborted) {
        x.abort();
        return;
      }
      signal.addEventListener('abort', onSignalAbort, { once: true });
    }
    x.send(fd);
  });
}

/** PUT a single S3/MinIO presigned part and return its ETag. */
export function uploadPart(
  url: string,
  blob: Blob,
  signal?: AbortSignal,
  onProgress?: (pct: number) => void,
): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const x = new XMLHttpRequest();
    const onSignalAbort = () => x.abort();
    const cleanup = () => signal?.removeEventListener('abort', onSignalAbort);
    x.open('PUT', url);
    x.upload.onprogress = (e: ProgressEvent) => {
      if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100));
    };
    x.onload = () => {
      cleanup();
      if (x.status < 200 || x.status >= 300) {
        reject(makeApiError(`分片上传失败 (${x.status})`, 1007));
        return;
      }
      const etag = x.getResponseHeader('ETag');
      if (!etag) {
        reject(makeApiError('分片上传成功但未返回 ETag', 1007));
        return;
      }
      resolve(etag);
    };
    x.onerror = () => { cleanup(); reject(makeApiError('分片网络错误', 1007)); };
    x.onabort = () => { cleanup(); reject(makeApiError('cancelled', -1)); };
    if (signal) {
      if (signal.aborted) {
        x.abort();
        return;
      }
      signal.addEventListener('abort', onSignalAbort, { once: true });
    }
    x.send(blob);
  });
}

// ==========================================
// 数据类型定义与专属 API 方法
// ==========================================

export interface UserInfo {
  Username?: string;
  username?: string;
  Email?: string;
  email?: string;
  Phone?: string;
  phone?: string;
  SignupAt?: string;
  signup_at?: string;
  LastActive?: string;
  last_active?: string;
}

export interface FileItem {
  id?: number;
  filehash: string;
  filename: string;
  size: number;
  username?: string;
  parent_id?: number;
  is_dir?: number;
  dir_path?: string;
  upload_time: string;
  summary?: string;
  tags?: string;
  score?: number;
}

export interface PagedList<T> {
  list: T[];
  total: number;
  page: number;
  size: number;
  breadcrumbs?: BreadcrumbItem[];
}

export interface BreadcrumbItem {
  id: number;
  name: string;
  path: string;
}

export interface ShareItem {
  ID: number;
  FileSha1: string;
  ShareToken: string;
  ExpireAt: string;
  HasPassword?: boolean;
}

export interface AIConfigData {
  configured: boolean;
  base_url?: string;
  has_key?: boolean;
  api_key_mask?: string;
  model?: string;
  embed_model?: string;
  mode?: 'openai' | 'mock' | string;
}

export interface AITestResult {
  ok: boolean;
  chat_ok?: boolean;
  embed_ok?: boolean;
  dim?: number;
  dim_mismatch?: boolean;
  error?: string;
}

export interface MultipartInitReq {
  filehash: string;
  filename: string;
  filesize: number;
  chunk_size?: number;
  parent_id?: number;
}

export interface MultipartInitResp {
  fast_upload: boolean;
  upload_id?: string;
  chunk_size?: number;
  chunk_count?: number;
  part_urls?: string[];
}

export interface CompletePart {
  part_number: number;
  etag: string;
}

export interface MultipartCompleteReq {
  upload_id: string;
  parts: CompletePart[];
}

export interface FolderCreateReq {
  name: string;
  parent_id?: number;
}

export interface FolderRenameReq {
  file_id: number;
  new_name: string;
}

export interface FolderMoveReq {
  file_id: number;
  target_parent_id: number;
}

// VFS 目录操作 API
export const vfsApi = {
  createFolder: (req: FolderCreateReq) => apiJSON<FileItem>('/file/folder/create', 'POST', req),
  rename: (req: FolderRenameReq) => apiJSON<void>('/file/folder/rename', 'POST', req),
  move: (req: FolderMoveReq) => apiJSON<void>('/file/folder/move', 'POST', req),
  queryDir: (parentId = 0, page = 1, size = 50) =>
    api<PagedList<FileItem>>(`/file/query?parent_id=${parentId}&page=${page}&size=${size}`),
};

// S3 Multipart 分片直传 API
export const multipartApi = {
  init: (req: MultipartInitReq, signal?: AbortSignal) => apiJSON<MultipartInitResp>('/file/upload/multipart/init', 'POST', req, signal),
  complete: (req: MultipartCompleteReq, signal?: AbortSignal) => apiJSON<FileItem>('/file/upload/multipart/complete', 'POST', req, signal),
  abort: (uploadId: string, signal?: AbortSignal) => apiJSON<void>('/file/upload/multipart/abort', 'POST', { upload_id: uploadId }, signal),
};

// AI 配置与检索 API
export const aiApi = {
  getConfig: () => api<AIConfigData>('/ai/config'),
  saveConfig: (config: { base_url: string; api_key: string; model: string; embed_model: string }) =>
    apiJSON<void>('/ai/config', 'POST', config),
  testConfig: (config: { base_url: string; api_key: string; model: string; embed_model: string }) =>
    apiJSON<AITestResult>('/ai/config/test', 'POST', config),
  clearConfig: () => apiJSON<void>('/ai/config', 'DELETE'),
  search: (query: string, page = 1, size = 20) =>
    api<PagedList<FileItem>>(`/file/ai/search?q=${encodeURIComponent(query)}&page=${page}&size=${size}`),
  similar: (filehash: string, limit = 5) =>
    api<FileItem[]>(`/file/ai/similar?filehash=${encodeURIComponent(filehash)}&limit=${limit}`),
  duplicates: () => api<FileItem[][]>('/file/ai/duplicates'),
};
