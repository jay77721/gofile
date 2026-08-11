import { describe, it, expect } from 'vitest';
import { makeApiError } from '../api';

describe('makeApiError', () => {
  it('should create error with message and code', () => {
    const err = makeApiError('test error', 1001);
    expect(err.message).toBe('test error');
    expect(err.code).toBe(1001);
    expect(err.name).toBe('ApiError');
    expect(err instanceof Error).toBe(true);
  });
});
