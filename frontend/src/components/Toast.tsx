// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported the prototype's toast notification to a React component.
// Author review: Nigeltzy - Simple boilerplate toast.

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

  // AI-generated (edited by James Teo): a success never stays up beside a newer
  // error. Screens show their errors where they happen, not here, so this
  // watches the page: the first error that appears or changes after the
  // success takes it down. An error already showing does not.
  useEffect(() => {
    if (message?.kind !== "success") return;
    const before = errorsOnPage();
    const observer = new MutationObserver(() => {
      if (hasNewerError(before, errorsOnPage())) onDismiss();
    });
    observer.observe(document.body, { childList: true, subtree: true, characterData: true });
    return () => observer.disconnect();
  }, [message, onDismiss]);

  if (!message) return null;
  return (
    <div className={`toast ${message.kind}`} role="status">
      {message.text}
    </div>
  );
}

// AI-generated (edited by James Teo): what a success toast gives way to.

/** An error on a screen: an alert, a failed request's line or a field's problem. */
const ERROR_SELECTOR = '[role="alert"], .status.error, .field-error';

/** The text of every error on the page, the toast's own excepted. */
function errorsOnPage(): Map<Element, string> {
  const errors = new Map<Element, string>();
  for (const el of document.querySelectorAll(ERROR_SELECTOR)) {
    if (!el.closest(".toast")) errors.set(el, el.textContent?.trim() ?? "");
  }
  return errors;
}

/**
 * True when `now` has an error that `before` did not: a new one, or one whose
 * text changed. One already showing in `before`, or one with no text, does
 * not count.
 */
export function hasNewerError<K>(before: Map<K, string>, now: Map<K, string>): boolean {
  for (const [key, text] of now) {
    if (text && before.get(key) !== text) return true;
  }
  return false;
}
