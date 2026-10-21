// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported the prototype's modal overlay plumbing to a React component.
// Author review: Nigeltzy - Simple button modal boilerplate generated.

// AI-generated (edited by James Teo): useRef, useState and RefObject, for
// keyboard focus.
import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";

interface ModalProps {
  onClose: () => void;
  children: ReactNode;
  // AI-generated (edited by James Teo).
  /** The id of the heading that names the dialog, for screen readers. */
  labelledBy?: string;
  // AI-generated (edited by James Teo).
  /**
   * Takes focus on close when whatever opened the dialog has left the page,
   * such as the Accept button of an errand that is no longer open. Give it
   * tabIndex={-1} so it can be focused.
   */
  fallbackFocus?: RefObject<HTMLElement | null>;
}

// AI-generated (edited by James Teo): what Tab may land on inside the dialog,
// before the controls a disabled fieldset turns off are left out.
const FOCUSABLE =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Overlay dialog. Closes on backdrop click, on the × button, and on Escape,
 * unless something inside already handled that Escape (an edit with unsaved
 * changes does).
 */
export function Modal({ onClose, children, labelledBy, fallbackFocus }: ModalProps) {
  // AI-generated (edited by James Teo): the dialog, and whatever had focus
  // before it opened, read while rendering so a field inside that takes focus
  // with autoFocus is not mistaken for it.
  const dialogRef = useRef<HTMLDivElement>(null);
  const [opener] = useState(() =>
    document.activeElement instanceof HTMLElement ? document.activeElement : null,
  );

  // AI-generated (edited by James Teo): Escape that something inside already
  // handled is left alone, and Tab is kept inside the dialog.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented) onClose();
      else if (e.key === "Tab") keepFocusInside(dialogRef.current, e);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  // AI-generated (edited by James Teo): keyboard focus moves into the dialog
  // when it opens, unless a field inside already took it, and goes back to
  // whatever opened it when it closes, so a keyboard user is not left on the
  // page behind the overlay. When that is gone, or was the page itself (a
  // button disabled while its call ran loses focus), fallbackFocus takes it.
  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.contains(document.activeElement)) dialog.focus();
    return () => {
      // Deferred, and only once the dialog has really left the page: React's
      // development re-run of this effect keeps the dialog where it is.
      setTimeout(() => {
        if (dialog?.isConnected) return;
        const back = opener && opener !== document.body && opener.isConnected ? opener : null;
        back?.focus();
        if (!back || document.activeElement !== back) fallbackFocus?.current?.focus();
      }, 0);
    };
  }, [opener, fallbackFocus]);

  return (
    <div
      className="modal-overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {/* AI-generated (edited by James Teo): named by its heading, and
          focusable itself. */}
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        tabIndex={-1}
        ref={dialogRef}
      >
        <button className="modal-close" onClick={onClose} aria-label="Close">
          ×
        </button>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  );
}

// AI-generated (edited by James Teo): Tab and Shift+Tab wrap around inside the
// dialog rather than leaving it. Controls a disabled fieldset turns off are
// skipped, since focusing one does nothing. Focus that fell out of the dialog,
// as when a button it was on is disabled while its call runs, comes back in.
function keepFocusInside(dialog: HTMLElement | null, e: KeyboardEvent) {
  if (!dialog) return;
  const items = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    (item) => !item.matches(":disabled"),
  );
  const current = document.activeElement;
  const first = items[0];
  const last = items[items.length - 1];
  if (!first || !last) {
    e.preventDefault();
    dialog.focus();
  } else if (!current || !dialog.contains(current)) {
    e.preventDefault();
    (e.shiftKey ? last : first).focus();
  } else if (e.shiftKey && (current === first || current === dialog)) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && current === last) {
    e.preventDefault();
    first.focus();
  }
}
