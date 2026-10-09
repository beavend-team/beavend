import React from 'react';
import { X, Image as ImageIcon, Music, Video as VideoIcon, FileText } from 'lucide-react';
import { ChatAttachment } from '../../api/client';

interface MediaAttachmentPreviewProps {
  attachments: ChatAttachment[];
  onRemove: (index: number) => void;
  disabled?: boolean;
}

function formatBytes(bytes?: number): string {
  if (!bytes) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function getAttachmentSrc(att: ChatAttachment): string {
  if (att.url) return att.url;
  if (att.data) {
    if (att.data.startsWith('data:')) return att.data;
    return `data:${att.mime_type};base64,${att.data}`;
  }
  return '';
}

export const MediaAttachmentPreview: React.FC<MediaAttachmentPreviewProps> = ({
  attachments,
  onRemove,
  disabled,
}) => {
  if (!attachments || attachments.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-2 px-3 pt-2 pb-1 bg-neutral-950/70 border-t border-neutral-800/80">
      {attachments.map((att, idx) => {
        const src = getAttachmentSrc(att);
        const isImage = att.type === 'image' && Boolean(src);

        return (
          <div
            key={idx}
            className="group relative flex items-center gap-2 bg-neutral-900 border border-neutral-700/80 rounded-lg p-1.5 pr-2.5 max-w-[220px] shadow-sm animate-in fade-in zoom-in-95 duration-150"
          >
            {/* Thumbnail or Icon */}
            {isImage ? (
              <img
                src={src}
                alt={att.filename || 'attachment'}
                className="w-8 h-8 rounded object-cover bg-neutral-950 border border-neutral-800 shrink-0"
              />
            ) : (
              <div className="w-8 h-8 rounded bg-neutral-800 flex items-center justify-center shrink-0 text-neutral-400">
                {att.type === 'audio' && <Music className="w-4 h-4 text-emerald-400" />}
                {att.type === 'video' && <VideoIcon className="w-4 h-4 text-purple-400" />}
                {att.type === 'document' && <FileText className="w-4 h-4 text-blue-400" />}
                {att.type === 'image' && <ImageIcon className="w-4 h-4 text-amber-400" />}
              </div>
            )}

            {/* Info */}
            <div className="flex-1 min-w-0">
              <p className="text-[11px] font-medium text-neutral-200 truncate">
                {att.filename || `${att.type} file`}
              </p>
              <p className="text-[10px] text-neutral-500 truncate">
                {formatBytes(att.size) || att.mime_type}
              </p>
            </div>

            {/* Remove button */}
            {!disabled && (
              <button
                type="button"
                onClick={() => onRemove(idx)}
                className="p-1 rounded-md text-neutral-400 hover:text-red-400 hover:bg-neutral-800 transition-colors"
                title="Remove attachment"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
};
