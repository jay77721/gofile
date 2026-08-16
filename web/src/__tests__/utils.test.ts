import { describe, it, expect } from 'vitest';
import {
  fmtSize,
  fmtDate,
  extOf,
  canPreview,
  fileKind,
  kindIcon,
  kindColor,
  bufHex,
  CHUNK_SIZE,
  CHUNK_MIN,
  MAX_FILE,
} from '../utils';

describe('fmtSize', () => {
  it('should format bytes correctly', () => {
    expect(fmtSize(0)).toBe('0 B');
    expect(fmtSize(500)).toBe('500 B');
    expect(fmtSize(1024)).toBe('1.0 KB');
    expect(fmtSize(1536)).toBe('1.5 KB');
    expect(fmtSize(1048576)).toBe('1.0 MB');
    expect(fmtSize(1048576 * 2.5)).toBe('2.5 MB');
    expect(fmtSize(1073741824)).toBe('1.00 GB');
    expect(fmtSize(1073741824 * 3.75)).toBe('3.75 GB');
  });

  it('should handle string or null/undefined gracefully', () => {
    expect(fmtSize('1024')).toBe('1.0 KB');
    expect(fmtSize(undefined)).toBe('0 B');
    expect(fmtSize(null)).toBe('0 B');
  });
});

describe('fmtDate', () => {
  it('should format date string', () => {
    const formatted = fmtDate('2026-08-16T12:00:00Z');
    expect(typeof formatted).toBe('string');
    expect(formatted.length).toBeGreaterThan(0);
  });

  it('should handle empty or invalid date gracefully', () => {
    expect(fmtDate('')).toBe('');
    expect(fmtDate(null)).toBe('');
    expect(fmtDate(undefined)).toBe('');
    expect(fmtDate('invalid-date')).toBe('invalid-date');
  });
});

describe('extOf & canPreview', () => {
  it('should extract extension in lowercase', () => {
    expect(extOf('image.PNG')).toBe('png');
    expect(extOf('archive.tar.gz')).toBe('gz');
    expect(extOf('noext')).toBe('');
    expect(extOf('')).toBe('');
  });

  it('should detect previewable files', () => {
    expect(canPreview('photo.jpg')).toBe(true);
    expect(canPreview('video.mp4')).toBe(true);
    expect(canPreview('song.mp3')).toBe(true);
    expect(canPreview('doc.pdf')).toBe(true);
    expect(canPreview('script.ts')).toBe(true);
    expect(canPreview('binary.exe')).toBe(false);
  });
});

describe('fileKind, kindIcon, kindColor', () => {
  it('should classify file kinds correctly', () => {
    expect(fileKind('pic.jpg')).toBe('image');
    expect(fileKind('movie.mp4')).toBe('video');
    expect(fileKind('track.flac')).toBe('audio');
    expect(fileKind('manual.pdf')).toBe('pdf');
    expect(fileKind('main.go')).toBe('code');
    expect(fileKind('archive.zip')).toBe('file');
  });

  it('should return appropriate icons and colors', () => {
    expect(kindIcon('image')).toBe('🖼');
    expect(kindIcon('video')).toBe('🎥');
    expect(kindIcon('unknown')).toBe('📄');

    expect(kindColor('image')).toBe('#7c3aed');
    expect(kindColor('video')).toBe('#dc2626');
    expect(kindColor('code')).toBe('#2563eb');
    expect(kindColor('file')).toBe('#64748b');
  });
});

describe('constants & bufHex', () => {
  it('should export correct constants', () => {
    expect(CHUNK_SIZE).toBe(5 * 1024 * 1024);
    expect(CHUNK_MIN).toBe(10 * 1024 * 1024);
    expect(MAX_FILE).toBe(100 * 1024 * 1024);
  });

  it('should convert buffer to hex string', () => {
    const bytes = new Uint8Array([0x00, 0x0f, 0x10, 0xff]);
    expect(bufHex(bytes)).toBe('000f10ff');
  });
});
