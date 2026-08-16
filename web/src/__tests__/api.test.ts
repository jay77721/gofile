import { describe, it, expect, vi, beforeEach } from 'vitest';
import { makeApiError, session, api, apiPost, apiJSON, vfsApi, multipartApi, aiApi } from '../api';

describe('makeApiError', () => {
  it('should create error with message and code', () => {
    const err = makeApiError('test error', 1001);
    expect(err.message).toBe('test error');
    expect(err.code).toBe(1001);
    expect(err.name).toBe('ApiError');
    expect(err instanceof Error).toBe(true);
  });
});

describe('session state', () => {
  it('should update reactive session username', () => {
    session.username = 'testuser';
    expect(session.username).toBe('testuser');
    session.username = '';
  });
});

describe('api functions', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('should handle successful api request', async () => {
    const mockData = { list: [{ filehash: 'abc', filename: 'file.txt' }] };
    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      json: () => Promise.resolve({ code: 0, msg: 'ok', data: mockData }),
    } as any);
    vi.stubGlobal('fetch', mockFetch);

    const data = await api('/file/query');
    expect(data).toEqual(mockData);
    expect(mockFetch).toHaveBeenCalledWith('/file/query', { credentials: 'include' });
  });

  it('should throw ApiError when code is non-zero', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      json: () => Promise.resolve({ code: 1004, msg: '文件不存在' }),
    } as any);
    vi.stubGlobal('fetch', mockFetch);

    await expect(api('/file/meta?filehash=123')).rejects.toThrow('文件不存在');
  });

  it('should clear session on 401 response', async () => {
    session.username = 'active_user';
    const mockFetch = vi.fn().mockResolvedValue({
      status: 401,
      json: () => Promise.resolve({ code: 1002, msg: 'unauthorized' }),
    } as any);
    vi.stubGlobal('fetch', mockFetch);

    await expect(api('/file/query')).rejects.toThrow('登录已过期，请重新登录');
    expect(session.username).toBe('');
  });

  it('should perform apiPost with urlencoded params', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      json: () => Promise.resolve({ code: 0, msg: 'ok', data: { success: true } }),
    } as any);
    vi.stubGlobal('fetch', mockFetch);

    const res = await apiPost('/user/signin', { username: 'foo', password: 'bar' });
    expect(res).toEqual({ success: true });
  });

  it('should perform apiJSON with json body', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      json: () => Promise.resolve({ code: 0, msg: 'ok', data: { id: 1 } }),
    } as any);
    vi.stubGlobal('fetch', mockFetch);

    const res = await apiJSON('/file/folder/create', 'POST', { name: 'docs', parent_id: 0 });
    expect(res).toEqual({ id: 1 });
  });

  it('should have vfsApi, multipartApi, aiApi defined', () => {
    expect(typeof vfsApi.createFolder).toBe('function');
    expect(typeof vfsApi.rename).toBe('function');
    expect(typeof vfsApi.move).toBe('function');
    expect(typeof vfsApi.queryDir).toBe('function');

    expect(typeof multipartApi.init).toBe('function');
    expect(typeof multipartApi.complete).toBe('function');
    expect(typeof multipartApi.abort).toBe('function');

    expect(typeof aiApi.getConfig).toBe('function');
    expect(typeof aiApi.saveConfig).toBe('function');
    expect(typeof aiApi.testConfig).toBe('function');
    expect(typeof aiApi.clearConfig).toBe('function');
    expect(typeof aiApi.search).toBe('function');
    expect(typeof aiApi.similar).toBe('function');
    expect(typeof aiApi.duplicates).toBe('function');
  });
});
