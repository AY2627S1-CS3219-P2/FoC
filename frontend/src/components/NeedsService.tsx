// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: The notice a fixture build shows in place of a screen whose service
//   it has no client for (order-service, credit-service).
// Author review: James Teo — reviewed and verified.

interface NeedsServiceProps {
  /** The screen's heading, as it reads with the service there. */
  title: string;
  /** The backend service the screen calls, e.g. "order-service". */
  service: string;
}

/**
 * Stands in for a screen in a fixture build (VITE_USE_FIXTURES=true), which
 * has a client for neither order-service nor credit-service: a real one
 * would send the fixture's fake token to the gateway and be signed out.
 * Unlike MockBadge, nothing is shown under it, because no fixture data
 * stands behind these screens.
 */
export function NeedsService({ title, service }: NeedsServiceProps) {
  return (
    <section>
      <div className="page-head">
        <h1>{title}</h1>
      </div>
      <p className="status">
        <strong>Needs {service}.</strong> A fixture build doesn&rsquo;t call{" "}
        {service}, so there is nothing to show here.
      </p>
    </section>
  );
}
