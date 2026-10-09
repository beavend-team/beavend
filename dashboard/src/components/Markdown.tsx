import React from 'react';

// Minimal, dependency-free markdown renderer. Emits React elements only
// (no dangerouslySetInnerHTML), so model output cannot inject HTML.

const INLINE_RE = /(`[^`]+`)|(\*\*[^*]+\*\*|__[^_]+__)|(\*[^*\s][^*]*\*|_[^_\s][^_]*_)|(\[[^\]]+\]\([^)\s]+\))/g;

function renderInline(text: string, keyPrefix: string): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  let last = 0;
  let i = 0;
  for (const m of text.matchAll(INLINE_RE)) {
    const idx = m.index ?? 0;
    if (idx > last) out.push(text.slice(last, idx));
    const tok = m[0];
    const key = `${keyPrefix}-${i++}`;
    if (m[1]) {
      out.push(
        <code key={key} className="px-1 py-0.5 rounded bg-neutral-950 border border-neutral-700 font-mono text-[11px] text-emerald-300">
          {tok.slice(1, -1)}
        </code>,
      );
    } else if (m[2]) {
      out.push(<strong key={key} className="font-semibold text-white">{renderInline(tok.slice(2, -2), key)}</strong>);
    } else if (m[3]) {
      out.push(<em key={key}>{renderInline(tok.slice(1, -1), key)}</em>);
    } else if (m[4]) {
      const lm = /^\[([^\]]+)\]\(([^)\s]+)\)$/.exec(tok);
      const href = lm?.[2] ?? '';
      if (lm && /^(https?:|mailto:)/i.test(href)) {
        out.push(
          <a key={key} href={href} target="_blank" rel="noopener noreferrer" className="text-emerald-400 underline">
            {lm[1]}
          </a>,
        );
      } else {
        out.push(tok);
      }
    }
    last = idx + tok.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

function isTableDelimRow(line: string): boolean {
  const trimmed = line.trim();
  if (!trimmed.includes('|')) return false;
  const inner = trimmed.replace(/^\|/, '').replace(/\|$/, '');
  const cells = inner.split('|');
  if (cells.length === 0) return false;
  return cells.every((c) => /^\s*:?-{1,}:?\s*$/.test(c));
}

function parseRowCells(line: string): string[] {
  let trimmed = line.trim();
  if (trimmed.startsWith('|')) trimmed = trimmed.slice(1);
  if (trimmed.endsWith('|')) trimmed = trimmed.slice(0, -1);
  return trimmed.split('|').map((c) => c.trim());
}

function parseAlignments(delimLine: string): ('left' | 'center' | 'right')[] {
  const cells = parseRowCells(delimLine);
  return cells.map((cell) => {
    const c = cell.trim();
    const start = c.startsWith(':');
    const end = c.endsWith(':');
    if (start && end) return 'center';
    if (end) return 'right';
    return 'left';
  });
}

export function Markdown({ content }: { content: string }) {
  const lines = content.replace(/\r\n/g, '\n').split('\n');
  const blocks: React.ReactNode[] = [];
  let i = 0;
  let k = 0;

  const isSpecial = (idx: number) => {
    const l = lines[idx];
    if (/^```/.test(l) || /^#{1,6}\s/.test(l) || /^\s*([-*+]|\d+\.)\s+/.test(l) || /^>\s?/.test(l) || /^\s*([-*_]){3,}\s*$/.test(l)) {
      return true;
    }
    // Check if line is the start of a markdown table (header followed by delimiter)
    if (l.includes('|') && idx + 1 < lines.length && isTableDelimRow(lines[idx + 1])) {
      return true;
    }
    return false;
  };

  while (i < lines.length) {
    const line = lines[i];

    if (/^```/.test(line)) {
      const buf: string[] = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) buf.push(lines[i++]);
      i++;
      blocks.push(
        <pre key={k++} className="p-2.5 my-1.5 bg-neutral-950 border border-neutral-800 rounded-lg text-[11px] font-mono text-neutral-200 overflow-x-auto whitespace-pre">
          {buf.join('\n')}
        </pre>,
      );
      continue;
    }

    // Markdown Table
    if (line.includes('|') && i + 1 < lines.length && isTableDelimRow(lines[i + 1])) {
      const headers = parseRowCells(lines[i]);
      const alignments = parseAlignments(lines[i + 1]);
      i += 2; // skip header and delimiter row
      const rows: string[][] = [];
      while (i < lines.length && lines[i].trim() !== '' && lines[i].includes('|')) {
        rows.push(parseRowCells(lines[i]));
        i++;
      }
      blocks.push(
        <div key={k++} className="my-2.5 overflow-x-auto rounded-lg border border-neutral-800 bg-neutral-950/70 shadow-sm">
          <table className="min-w-full divide-y divide-neutral-800 text-xs">
            <thead className="bg-neutral-900/90 text-neutral-200">
              <tr>
                {headers.map((h, colIdx) => {
                  const align = alignments[colIdx] || 'left';
                  return (
                    <th
                      key={colIdx}
                      className={`px-3.5 py-2.5 font-semibold text-[11px] tracking-wider text-neutral-300 border-b border-neutral-800 ${
                        align === 'center' ? 'text-center' : align === 'right' ? 'text-right' : 'text-left'
                      }`}
                    >
                      {renderInline(h, `th${k}-${colIdx}`)}
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800/60 font-normal">
              {rows.map((row, rowIdx) => (
                <tr key={rowIdx} className="hover:bg-neutral-900/50 transition-colors">
                  {headers.map((_, colIdx) => {
                    const align = alignments[colIdx] || 'left';
                    const cell = row[colIdx] ?? '';
                    return (
                      <td
                        key={colIdx}
                        className={`px-3.5 py-2 text-neutral-300 ${
                          align === 'center' ? 'text-center' : align === 'right' ? 'text-right' : 'text-left'
                        }`}
                      >
                        {renderInline(cell, `td${k}-${rowIdx}-${colIdx}`)}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      continue;
    }

    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      const size = ['text-base', 'text-sm', 'text-sm', 'text-xs', 'text-xs', 'text-xs'][h[1].length - 1];
      blocks.push(
        <div key={k++} className={`${size} font-bold text-white mt-2 mb-1`}>
          {renderInline(h[2], `h${k}`)}
        </div>,
      );
      i++;
      continue;
    }

    if (/^\s*([-*_]){3,}\s*$/.test(line)) {
      blocks.push(<hr key={k++} className="my-2 border-neutral-700" />);
      i++;
      continue;
    }

    if (/^>\s?/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) buf.push(lines[i++].replace(/^>\s?/, ''));
      blocks.push(
        <blockquote key={k++} className="border-l-2 border-neutral-600 pl-3 my-1.5 text-neutral-400">
          {renderInline(buf.join(' '), `q${k}`)}
        </blockquote>,
      );
      continue;
    }

    const li = /^\s*([-*+]|\d+\.)\s+/.exec(line);
    if (li) {
      const ordered = /\d/.test(li[1]);
      const items: string[] = [];
      while (i < lines.length && /^\s*([-*+]|\d+\.)\s+/.test(lines[i])) {
        items.push(lines[i++].replace(/^\s*([-*+]|\d+\.)\s+/, ''));
      }
      const Tag = ordered ? 'ol' : 'ul';
      blocks.push(
        <Tag key={k++} className={`${ordered ? 'list-decimal' : 'list-disc'} pl-5 my-1.5 space-y-0.5`}>
          {items.map((it, n) => (
            <li key={n}>{renderInline(it, `l${k}-${n}`)}</li>
          ))}
        </Tag>,
      );
      continue;
    }

    if (line.trim() === '') {
      i++;
      continue;
    }

    const buf: string[] = [];
    while (i < lines.length && lines[i].trim() !== '' && !isSpecial(i)) buf.push(lines[i++]);
    if (buf.length === 0) buf.push(lines[i++]);
    blocks.push(
      <p key={k++} className="my-1 whitespace-pre-wrap">
        {renderInline(buf.join('\n'), `p${k}`)}
      </p>,
    );
  }

  return <div className="space-y-0.5">{blocks}</div>;
}
