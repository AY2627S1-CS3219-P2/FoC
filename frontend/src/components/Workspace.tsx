// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: The workspace layout (D-154): a list with the open item's detail beside
//   it on a wide window, and the same detail in a dialog on a narrow one. Arrow
//   keys move between the list's rows.
// Author review: PENDING — <reviewer to complete>

import { useRef, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { nextListIndex } from "../lib/listKeys";
import { useMediaQuery } from "../lib/useMediaQuery";
import { Modal } from "./Modal";

/** The width from which a list and its detail fit side by side. */
export const WORKSPACE_WIDE = "(min-width: 1100px)";

/** The rows an arrow key can land on: an errand's button and any row that opts in. */
const ROW = ".errand-card-open, [data-ws-item]";

interface WorkspaceProps {
  /** The list, with its filters. */
  list: ReactNode;
  /** The open item's detail, or null when none is open. */
  detail: ReactNode | null;
  onCloseDetail: () => void;
  /** Said in the empty pane, e.g. "Pick an errand to see it here". */
  emptyText: string;
  /** The id of the detail's heading, so the dialog on a narrow window is named by it. */
  detailTitleId?: string;
  /** Where focus goes in the dialog when its first control cannot take it. */
  fallbackFocus?: RefObject<HTMLElement | null>;
  /**
   * What a narrow window does with the detail: open it in a dialog (the default),
   * or leave it to the list, for a detail that holds a dialog of its own and so
   * cannot sit inside one.
   */
  narrow?: "dialog" | "list";
}

/**
 * Wide: two columns, the list on the left and the detail on the right, with no
 * page change when a row is picked. Narrow: the list alone, and a picked row's
 * detail opens in a dialog as it did before.
 */
export function Workspace({ list, detail, onCloseDetail, emptyText, detailTitleId, fallbackFocus, narrow = "dialog" }: WorkspaceProps) {
  const wide = useMediaQuery(WORKSPACE_WIDE);
  const listRef = useRef<HTMLDivElement>(null);

  /** Escape closes the open detail, as it closes the dialog on a narrow window. */
  function onEscape(e: KeyboardEvent) {
    if (e.key === "Escape" && detail && !e.defaultPrevented) onCloseDetail();
  }

  /** Arrow keys, Home and End move focus between rows; Enter opens the focused one. */
  function onKeyDown(e: KeyboardEvent) {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const rows = [...(listRef.current?.querySelectorAll<HTMLElement>(ROW) ?? [])];
    const target = nextListIndex(rows.indexOf(document.activeElement as HTMLElement), e.key, rows.length);
    if (target === null) return;
    e.preventDefault();
    rows[target]!.focus();
  }

  if (!wide) {
    return (
      <>
        {list}
        {narrow === "dialog" && detail && (
          <Modal onClose={onCloseDetail} labelledBy={detailTitleId} fallbackFocus={fallbackFocus}>
            {detail}
          </Modal>
        )}
      </>
    );
  }

  return (
    <div className="ws" onKeyDown={onEscape}>
      <div className="ws-list" ref={listRef} onKeyDown={onKeyDown}>
        {list}
      </div>
      <aside className="ws-detail" aria-label="Details">
        {detail ? (
          <>
            <button type="button" className="ws-close" aria-label="Close details" onClick={onCloseDetail}>
              &times;
            </button>
            {detail}
          </>
        ) : (
          <p className="ws-empty">{emptyText}</p>
        )}
      </aside>
    </div>
  );
}
