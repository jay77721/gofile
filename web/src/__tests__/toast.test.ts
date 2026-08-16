import { describe, it, expect, beforeEach, vi } from 'vitest';
import { toast, toasts, clearToasts } from '../toast';

describe('toast', () => {
  beforeEach(() => {
    clearToasts();
    vi.useFakeTimers();
  });

  it('should add a toast item', () => {
    toast('Hello world', 'ok');
    expect(toasts.length).toBe(1);
    expect(toasts[0].msg).toBe('Hello world');
    expect(toasts[0].type).toBe('ok');
    expect(typeof toasts[0].id).toBe('number');
  });

  it('should auto-remove toast after specified duration', () => {
    toast('Auto dismiss', 'warn', 1000);
    expect(toasts.length).toBe(1);

    vi.advanceTimersByTime(999);
    expect(toasts.length).toBe(1);

    vi.advanceTimersByTime(2);
    expect(toasts.length).toBe(0);
  });

  it('should clear all toasts with clearToasts', () => {
    toast('Toast 1');
    toast('Toast 2');
    expect(toasts.length).toBe(2);

    clearToasts();
    expect(toasts.length).toBe(0);
  });
});
