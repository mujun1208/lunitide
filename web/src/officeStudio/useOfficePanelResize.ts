import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { usePanelResize } from '../ui/usePanelResize';

export const OFFICE_PANEL_WIDTH_KEY = 'lunitide:office-studio:inspector-width';
const MIN = 350;
const maxWidth = (width: number) => Math.max(MIN, width >= 1200 ? width - 208 - 320 - 8 : width - 100);

export function useOfficePanelResize(taskId: string) {
  const workspaceRef = useRef<HTMLDivElement>(null);
  const [available, setAvailable] = useState(window.innerWidth);
  const limit = maxWidth(available);
  const [width, start, resizeTo] = usePanelResize({
    storageKey: OFFICE_PANEL_WIDTH_KEY,
    initial: Math.round(available * 0.35),
    min: MIN,
    max: () => limit,
    reverse: true,
  });
  useEffect(() => {
    const workspace = workspaceRef.current;
    if (!workspace) return;
    const measure = () => { if (workspace.clientWidth > 0) setAvailable(workspace.clientWidth); };
    measure();
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', measure);
      return () => window.removeEventListener('resize', measure);
    }
    const observer = new ResizeObserver(measure);
    observer.observe(workspace);
    return () => observer.disconnect();
  }, [taskId]);
  useEffect(() => {
    if (width > limit) resizeTo(limit);
  }, [width, limit, resizeTo]);
  const reset = () => resizeTo(available * 0.35);
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = event.shiftKey ? 80 : 20;
    switch (event.key) {
      case 'ArrowLeft': resizeTo(width + step); break;
      case 'ArrowRight': resizeTo(width - step); break;
      case 'Home': resizeTo(MIN); break;
      case 'End': resizeTo(limit); break;
      default: return;
    }
    event.preventDefault();
  };
  return { workspaceRef, width, start, reset, onKeyDown, min: MIN, max: limit };
}
