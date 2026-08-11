import { describe, it, expect } from 'vitest';
import * as api from '../api';

describe('debug', () => {
  it('should import makeApiError', () => {
    console.log('api exports:', Object.keys(api));
    console.log('makeApiError:', typeof api.makeApiError);
    expect(typeof api.makeApiError).toBe('function');
  });
});
