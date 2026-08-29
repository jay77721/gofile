// Global toast state and trigger
import { reactive } from 'vue';

export type ToastType = 'ok' | 'err' | 'warn' | 'info';

export interface ToastItem {
  id: number;
  msg: string;
  type: ToastType;
}

export const toasts: ToastItem[] = reactive<ToastItem[]>([]);

let seq = 0;

export function toast(msg: string, type: ToastType = 'ok', duration = 2800): void {
  const id = ++seq;
  toasts.push({ id, msg, type });
  setTimeout(() => {
    const i = toasts.findIndex((t) => t.id === id);
    if (i >= 0) toasts.splice(i, 1);
  }, duration);
}

export function clearToasts(): void {
  toasts.splice(0, toasts.length);
}
