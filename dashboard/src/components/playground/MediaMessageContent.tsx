import React, { useState } from 'react';
import { Image as ImageIcon, Music, Video as VideoIcon, FileText, ExternalLink, Download, X } from 'lucide-react';
import { ChatAttachment, MediaRef } from '../../api/client';

interface MediaMessageContentProps {
  attachments?: ChatAttachment[];
  mediaParts?: MediaRef[];
  role?: string;
}

function getSrc(item: ChatAttachment | MediaRef): string {
  if (item.url) return item.url;
  const data = (item as ChatAttachment).data;
  if (data) {
    if (data.startsWith('data:')) return data;
    return `data:${item.mime_type};base64,${data}`;
  }
  return '';
}

function formatBytes(bytes?: number): string {
  if (!bytes) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export const MediaMessageContent: React.FC<MediaMessageContentProps> = ({
  attachments = [],
  mediaParts = [],
  role = 'user',
}) => {
  const [activeModalSrc, setActiveModalSrc] = useState<string | null>(null);

  const allItems: Array<{
    type: string;
    mime: string;
    src: string;
    filename?: string;
    size?: number;
    caption?: string;
  }> = [];

  for (const att of attachments) {
    allItems.push({
      type: att.type,
      mime: att.mime_type,
      src: getSrc(att),
      filename: att.filename,
      size: att.size,
    });
  }

  for (const part of mediaParts) {
    let t = 'image';
    if (part.mime_type?.startsWith('audio/')) t = 'audio';
    else if (part.mime_type?.startsWith('video/')) t = 'video';
    else if (part.mime_type?.includes('pdf')) t = 'document';

    allItems.push({
      type: t,
      mime: part.mime_type,
      src: getSrc(part),
      filename: part.filename || part.media_id,
      size: part.size,
      caption: part.caption,
    });
  }

  if (allItems.length === 0) return null;

  return (
    <div className={`space-y-2 mb-2 ${role === 'user' ? 'text-right' : 'text-left'}`}>
      <div className={`flex flex-wrap gap-2 ${role === 'user' ? 'justify-end' : 'justify-start'}`}>
        {allItems.map((item, idx) => {
          const isImg = item.type === 'image' || item.mime.startsWith('image/');
          const isAud = item.type === 'audio' || item.mime.startsWith('audio/');
          const isVid = item.type === 'video' || item.mime.startsWith('video/');

          if (isImg && item.src) {
            return (
              <div key={idx} className="relative group rounded-xl overflow-hidden border border-neutral-700/60 max-w-[260px] bg-neutral-950">
                <img
                  src={item.src}
                  alt={item.filename || 'media attachment'}
                  onClick={() => setActiveModalSrc(item.src)}
                  className="w-full h-auto max-h-52 object-cover cursor-pointer hover:scale-[1.02] transition-transform duration-200"
                  loading="lazy"
                />
                {item.caption && (
                  <div className="p-1.5 bg-neutral-900/90 text-[10px] text-neutral-300 border-t border-neutral-800">
                    {item.caption}
                  </div>
                )}
                {item.filename && !item.caption && (
                  <div className="absolute bottom-0 inset-x-0 bg-gradient-to-t from-black/80 via-black/40 to-transparent p-1.5 opacity-0 group-hover:opacity-100 transition-opacity">
                    <p className="text-[10px] text-white truncate">{item.filename}</p>
                  </div>
                )}
              </div>
            );
          }

          if (isAud && item.src) {
            return (
              <div
                key={idx}
                className="w-full max-w-sm bg-neutral-900/90 border border-neutral-700/70 rounded-xl p-2.5 shadow-sm text-left"
              >
                <div className="flex items-center gap-2 mb-1.5">
                  <div className="p-1.5 rounded-lg bg-emerald-500/10 text-emerald-400">
                    <Music className="w-3.5 h-3.5" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-[11px] font-medium text-neutral-200 truncate">
                      {item.filename || 'Audio clip'}
                    </p>
                    <p className="text-[9px] text-neutral-500 font-mono">
                      {item.mime} {item.size ? `· ${formatBytes(item.size)}` : ''}
                    </p>
                  </div>
                </div>
                <audio controls src={item.src} className="w-full h-8" preload="metadata" />
              </div>
            );
          }

          if (isVid && item.src) {
            return (
              <div
                key={idx}
                className="w-full max-w-md bg-neutral-950 border border-neutral-700/70 rounded-xl overflow-hidden shadow-sm text-left"
              >
                <video controls src={item.src} className="w-full max-h-64 object-contain bg-black" preload="metadata" />
                {item.filename && (
                  <div className="p-2 bg-neutral-900/90 flex items-center justify-between text-[11px] text-neutral-300 border-t border-neutral-800">
                    <span className="truncate">{item.filename}</span>
                    <span className="text-[10px] text-neutral-500 shrink-0 ml-2">{formatBytes(item.size)}</span>
                  </div>
                )}
              </div>
            );
          }

          // Document / other files
          return (
            <div
              key={idx}
              className="flex items-center gap-2.5 bg-neutral-900/90 border border-neutral-700/70 rounded-xl p-2.5 max-w-xs shadow-sm text-left"
            >
              <div className="p-2 rounded-lg bg-blue-500/10 text-blue-400 shrink-0">
                <FileText className="w-4 h-4" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-[11px] font-medium text-neutral-200 truncate">
                  {item.filename || 'Document'}
                </p>
                <p className="text-[10px] text-neutral-400 truncate">
                  {item.mime} {item.size ? `· ${formatBytes(item.size)}` : ''}
                </p>
              </div>
              {item.src && (
                <a
                  href={item.src}
                  target="_blank"
                  rel="noopener noreferrer"
                  download={item.filename || 'attachment'}
                  className="p-1.5 text-neutral-400 hover:text-white rounded-lg hover:bg-neutral-800 transition-colors"
                  title="Download / View document"
                >
                  <ExternalLink className="w-3.5 h-3.5" />
                </a>
              )}
            </div>
          );
        })}
      </div>

      {/* Lightbox Modal */}
      {activeModalSrc && (
        <div
          className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm flex items-center justify-center p-4"
          onClick={() => setActiveModalSrc(null)}
        >
          <div className="relative max-w-4xl max-h-[90vh] bg-neutral-900 border border-neutral-800 rounded-2xl overflow-hidden shadow-2xl">
            <button
              type="button"
              onClick={() => setActiveModalSrc(null)}
              className="absolute top-3 right-3 p-1.5 rounded-full bg-black/60 text-white hover:bg-black/90 transition-colors z-10"
              title="Close"
            >
              <X className="w-4 h-4" />
            </button>
            <img src={activeModalSrc} alt="Preview" className="max-w-full max-h-[85vh] object-contain" />
          </div>
        </div>
      )}
    </div>
  );
};
