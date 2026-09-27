// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Visible marker for screens backed by fixtures rather than a service.
// Author review: Nigeltzy - Simple boilerplate for mock badge UI.

interface MockBadgeProps {
  /** The service this screen is waiting on, e.g. "credit-service". */
  service: string;
}

/**
 * Visible label for a screen whose data comes from a fixture module rather
 * than a real service, so fixture figures are not read as live data.
 */
export function MockBadge({ service }: MockBadgeProps) {
  return (
    <span className="mock-flag" title={`${service} is not implemented yet`}>
      Mock data · {service}
    </span>
  );
}
