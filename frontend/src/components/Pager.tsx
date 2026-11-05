// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Previous / Next paging for the admin dashboard's lists (D-104): "Page
//   3 of 7", with the buttons off at either end and while a page loads.
// Author review: PENDING — <reviewer to complete>

interface PagerProps {
  /** 1-based. */
  page: number;
  pageCount: number;
  /** Rows matched in all, shown beside the page number. */
  total: number;
  busy?: boolean;
  onPage: (page: number) => void;
}

/** Paging controls; renders nothing when everything fits on one page. */
export function Pager({ page, pageCount, total, busy = false, onPage }: PagerProps) {
  if (pageCount <= 1) {
    return total > 0 ? <p className="pager-info">{total} in all</p> : null;
  }
  return (
    <nav className="pager" aria-label="Pages">
      <button className="btn btn-secondary" disabled={busy || page <= 1} onClick={() => onPage(page - 1)}>
        Previous
      </button>
      <span className="pager-info" aria-live="polite">
        Page {page} of {pageCount} · {total} in all
      </span>
      <button className="btn btn-secondary" disabled={busy || page >= pageCount} onClick={() => onPage(page + 1)}>
        Next
      </button>
    </nav>
  );
}
