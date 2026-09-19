// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported the prototype's toast notification to a React component.
// Author review: PENDING — <reviewer to complete>

import { useEffect } from "react";

export type ToastKind = "success" | "error";

export interface ToastMessage {
  text: string;
  kind: ToastKind;
}

interface ToastProps {
  message: ToastMessage | null;
  onDismiss: () => void;
}

export function Toast({ message, onDismiss }: ToastProps) {
  useEffect(() => {
    if (!message) return;
    const timer = setTimeout(onDismiss, 3000);
    return () => clearTimeout(timer);
  }, [message, onDismiss]);

  if (!message) return null;
  return (
    <div className={`toast ${message.kind}`} role="status">
      {message.text}
    </div>
  );
}
