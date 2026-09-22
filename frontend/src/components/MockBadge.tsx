// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Visible marker for screens backed by fixtures rather than a service.
// Author review: Nigetlzy - Simple boilerplate for mock badge UI.

interface MockBadgeProps {
  /** The service this screen is waiting on, e.g. "credit-service". */
  service: string;
}

/**
 * Renders on every screen whose data comes from src/lib/mock.ts fixtures. A
 * graded demo must not show invented figures as if they were live, so this is
 * deliberately visible rather than a code comment.
 */
export function MockBadge({ service }: MockBadgeProps) {
  return (
    <span className="mock-flag" title={`${service} is not implemented yet`}>
      Mock data · {service}
    </span>
  );
}
