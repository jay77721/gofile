// 工具函数:格式化、文件类型、分片哈希

export function fmtSize(b) {
  if (b < 1024) return b + ' B'
  if (b < 1048576) return (b / 1024).toFixed(1) + ' KB'
  if (b < 1073741824) return (b / 1048576).toFixed(1) + ' MB'
  return (b / 1073741824).toFixed(2) + ' GB'
}

export function fmtDate(s) {
  try {
    return new Date(s).toLocaleString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  } catch { return s }
}

const IMAGE = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico']
const VIDEO = ['mp4', 'webm', 'mov']
const AUDIO = ['mp3', 'wav', 'ogg', 'flac']
const CODE = ['txt', 'md', 'log', 'json', 'xml', 'csv', 'go', 'js', 'ts', 'jsx', 'tsx', 'py', 'css', 'html', 'sh', 'bash', 'bat', 'env', 'conf', 'ini', 'toml', 'sql', 'java', 'rb', 'php', 'rs', 'swift', 'kt', 'yaml', 'yml']

export function extOf(fn) { return fn.split('.').pop().toLowerCase() }

export function canPreview(fn) {
  const ext = extOf(fn)
  return IMAGE.includes(ext) || VIDEO.includes(ext) || AUDIO.includes(ext) || ext === 'pdf' || CODE.includes(ext)
}

// 文件类型分类(图标/标签)
export function fileKind(fn) {
  const ext = extOf(fn)
  if (IMAGE.includes(ext)) return 'image'
  if (VIDEO.includes(ext)) return 'video'
  if (AUDIO.includes(ext)) return 'audio'
  if (ext === 'pdf') return 'pdf'
  if (CODE.includes(ext)) return 'code'
  return 'file'
}

export function kindIcon(kind) {
  return { image: '🖼', video: '🎥', audio: '🎵', pdf: '📄', code: '📝', file: '📄' }[kind] || '📄'
}

export function kindColor(kind) {
  return {
    image: '#7c3aed', video: '#dc2626', audio: '#d97706',
    pdf: '#dc2626', code: '#2563eb', file: '#64748b',
  }[kind] || '#64748b'
}

// 分片哈希(与后端 FastUpload 一致的 head+tail+size 方案)
export const CHUNK_SIZE = 5 * 1024 * 1024
export const CHUNK_MIN = 10 * 1024 * 1024
export const MAX_FILE = 100 * 1024 * 1024

export async function sha1HeadTail(file) {
  if (file.size <= CHUNK_SIZE) return bufHex(await crypto.subtle.digest('SHA-1', await file.arrayBuffer()))
  const head = await file.slice(0, CHUNK_SIZE).arrayBuffer()
  const tail = await file.slice(Math.max(0, file.size - CHUNK_SIZE)).arrayBuffer()
  const buf = new Uint8Array(head.byteLength + tail.byteLength + 8)
  buf.set(new Uint8Array(head), 0)
  buf.set(new Uint8Array(tail), head.byteLength)
  new DataView(buf.buffer).setBigUint64(head.byteLength + tail.byteLength, BigInt(file.size))
  return bufHex(await crypto.subtle.digest('SHA-1', buf))
}

function bufHex(b) { return Array.from(new Uint8Array(b), (x) => x.toString(16).padStart(2, '0')).join('') }
