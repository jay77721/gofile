// 工具函数:格式化、文件类型、分片哈希

export type FileKind = 'image' | 'video' | 'audio' | 'pdf' | 'code' | 'file';

export function fmtSize(b: number | string | undefined | null): string {
  const bytes = typeof b === 'number' ? b : Number(b) || 0;
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1073741824) return `${(bytes / 1048576).toFixed(1)} MB`;
  return `${(bytes / 1073741824).toFixed(2)} GB`;
}

export function fmtDate(s: string | number | Date | undefined | null): string {
  if (!s) return '';
  try {
    const d = new Date(s);
    if (isNaN(d.getTime())) return String(s);
    return d.toLocaleString('zh-CN', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return String(s);
  }
}

export const IMAGE_EXTS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico'] as const;
export const VIDEO_EXTS = ['mp4', 'webm', 'mov'] as const;
export const AUDIO_EXTS = ['mp3', 'wav', 'ogg', 'flac'] as const;
export const CODE_EXTS = [
  'txt', 'md', 'log', 'json', 'xml', 'csv', 'go', 'js', 'ts', 'jsx', 'tsx',
  'py', 'css', 'html', 'sh', 'bash', 'bat', 'env', 'conf', 'ini', 'toml',
  'sql', 'java', 'rb', 'php', 'rs', 'swift', 'kt', 'yaml', 'yml',
] as const;

export function extOf(fn: string): string {
  if (!fn) return '';
  const parts = fn.split('.');
  return parts.length > 1 ? (parts.pop() || '').toLowerCase() : '';
}

export function canPreview(fn: string): boolean {
  const ext = extOf(fn);
  return (
    IMAGE_EXTS.includes(ext as any) ||
    VIDEO_EXTS.includes(ext as any) ||
    AUDIO_EXTS.includes(ext as any) ||
    ext === 'pdf' ||
    CODE_EXTS.includes(ext as any)
  );
}

// 文件类型分类(图标/标签)
export function fileKind(fn: string): FileKind {
  const ext = extOf(fn);
  if (IMAGE_EXTS.includes(ext as any)) return 'image';
  if (VIDEO_EXTS.includes(ext as any)) return 'video';
  if (AUDIO_EXTS.includes(ext as any)) return 'audio';
  if (ext === 'pdf') return 'pdf';
  if (CODE_EXTS.includes(ext as any)) return 'code';
  return 'file';
}

export function kindIcon(kind: FileKind | string): string {
  const icons: Record<string, string> = {
    image: '🖼',
    video: '🎥',
    audio: '🎵',
    pdf: '📄',
    code: '📝',
    file: '📄',
  };
  return icons[kind] || '📄';
}

export function kindColor(kind: FileKind | string): string {
  const colors: Record<string, string> = {
    image: '#7c3aed',
    video: '#dc2626',
    audio: '#d97706',
    pdf: '#dc2626',
    code: '#2563eb',
    file: '#64748b',
  };
  return colors[kind] || '#64748b';
}

// 分片哈希(与后端 FastUpload 一致的 head+tail+size 方案)
export const CHUNK_SIZE = 5 * 1024 * 1024;
export const CHUNK_MIN = 10 * 1024 * 1024;
export const MAX_FILE = 100 * 1024 * 1024;

export function bufHex(b: ArrayBuffer | ArrayBufferView): string {
  const bytes = b instanceof Uint8Array ? b : new Uint8Array(b instanceof ArrayBuffer ? b : b.buffer);
  return Array.from(bytes, (x) => x.toString(16).padStart(2, '0')).join('');
}

export async function sha1HeadTail(file: Blob): Promise<string> {
  if (file.size <= CHUNK_SIZE) {
    return bufHex(await crypto.subtle.digest('SHA-1', await file.arrayBuffer()));
  }
  const head = await file.slice(0, CHUNK_SIZE).arrayBuffer();
  const tail = await file.slice(Math.max(0, file.size - CHUNK_SIZE)).arrayBuffer();
  const buf = new Uint8Array(head.byteLength + tail.byteLength + 8);
  buf.set(new Uint8Array(head), 0);
  buf.set(new Uint8Array(tail), head.byteLength);
  new DataView(buf.buffer).setBigUint64(head.byteLength + tail.byteLength, BigInt(file.size));
  return bufHex(await crypto.subtle.digest('SHA-1', buf));
}
