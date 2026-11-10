// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The admin dashboard (D-101 to D-123): one screen with four tabs, the
//   active errands, the past errands, the accounts and the audit log, replacing
//   the separate All errands and Admin screens.
// Author review: PENDING — <reviewer to complete>

import { useRef, useState, type KeyboardEvent } from "react";
import { MockBadge } from "../../components/MockBadge";
import { NeedsService } from "../../components/NeedsService";
import type { ToastMessage } from "../../components/Toast";
import { AllErrandsView } from "../errands/AllErrandsView";
import type { CreditAdminApi } from "../credits/creditAdminApi";
import type { OrdersApi } from "../errands/ordersApi";
import type { UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { AccountsPanel } from "./AccountsPanel";
import { AuditLogView } from "./AuditLogView";
import { FlaggedWalletsView } from "./FlaggedWalletsView";
import { PastErrandsView } from "./PastErrandsView";

export type DashboardTab = "active" | "past" | "accounts" | "flagged" | "audit";

export const DASHBOARD_TABS: { id: DashboardTab; label: string }[] = [
  { id: "active", label: "Active errands" },
  { id: "past", label: "Past errands" },
  { id: "accounts", label: "Accounts" },
  { id: "flagged", label: "Flagged wallets" },
  { id: "audit", label: "Audit log" },
];

/**
 * The tab to move to for an arrow, Home or End key, wrapping at the ends; null
 * for any other key. The tab bar follows the WAI-ARIA tabs pattern.
 */
export function tabAfterKey(current: DashboardTab, key: string): DashboardTab | null {
  const index = DASHBOARD_TABS.findIndex((t) => t.id === current);
  const last = DASHBOARD_TABS.length - 1;
  switch (key) {
    case "ArrowRight":
      return DASHBOARD_TABS[index === last ? 0 : index + 1]!.id;
    case "ArrowLeft":
      return DASHBOARD_TABS[index === 0 ? last : index - 1]!.id;
    case "Home":
      return DASHBOARD_TABS[0]!.id;
    case "End":
      return DASHBOARD_TABS[last]!.id;
    default:
      return null;
  }
}

interface DashboardViewProps {
  /** Null in a fixture build, which has no user-service to call. */
  usersApi: UsersApi | null;
  /** Null in a fixture build, which has no order-service to call. */
  ordersApi: OrdersApi | null;
  suppliersApi: SuppliersApi;
  // AI-generated (edited by Claude Code, 2026-11-09): the credit admin routes (D-150).
  /** Null in a fixture build, which has no credit-service to call. */
  creditAdminApi: CreditAdminApi | null;
  /** The signed-in admin. */
  userId: string;
  onNotify: (message: ToastMessage) => void;
  /** An account handed over from an errand's detail; opens the Accounts tab on it. */
  initialAccountId: string | null;
}

/**
 * App renders this for ADMIN sessions only, which is an affordance: each
 * service answers the admin reads and actions for ADMIN tokens alone.
 */
export function DashboardView({ usersApi, ordersApi, suppliersApi, creditAdminApi, userId, onNotify, initialAccountId }: DashboardViewProps) {
  const [tab, setTab] = useState<DashboardTab>(initialAccountId ? "accounts" : "active");
  const [account, setAccount] = useState<string | null>(initialAccountId);
  const tabRefs = useRef<Partial<Record<DashboardTab, HTMLButtonElement | null>>>({});

  /** Opens an account from an errand's detail or an audit row. */
  function openAccount(uid: string) {
    setAccount(uid);
    setTab("accounts");
  }

  function onKeyDown(e: KeyboardEvent) {
    const next = tabAfterKey(tab, e.key);
    if (!next) return;
    e.preventDefault();
    setTab(next);
    tabRefs.current[next]?.focus();
  }

  return (
    <section>
      <div className="page-head">
        <h1>Dashboard</h1>
        <p className="page-sub">Settle stalled errands, review accounts, and see what admins have done.</p>
      </div>

      <div className="dash-tabs" role="tablist" aria-label="Dashboard sections" onKeyDown={onKeyDown}>
        {DASHBOARD_TABS.map((t) => (
          <button
            key={t.id}
            ref={(el) => {
              tabRefs.current[t.id] = el;
            }}
            id={`dash-tab-${t.id}`}
            role="tab"
            type="button"
            aria-selected={tab === t.id}
            aria-controls={`dash-panel-${t.id}`}
            tabIndex={tab === t.id ? 0 : -1}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div role="tabpanel" id={`dash-panel-${tab}`} aria-labelledby={`dash-tab-${tab}`}>
        {tab === "active" &&
          (ordersApi ? (
            <AllErrandsView
              embedded
              ordersApi={ordersApi}
              suppliersApi={suppliersApi}
              usersApi={usersApi}
              userId={userId}
              onNotify={onNotify}
              onLookUpAccount={openAccount}
            />
          ) : (
            <NeedsService title="Active errands" service="order-service" />
          ))}

        {tab === "past" &&
          (ordersApi ? (
            <PastErrandsView
              ordersApi={ordersApi}
              suppliersApi={suppliersApi}
              usersApi={usersApi}
              userId={userId}
              onNotify={onNotify}
              onLookUpAccount={openAccount}
            />
          ) : (
            <NeedsService title="Past errands" service="order-service" />
          ))}

        {tab === "accounts" &&
          (usersApi ? (
            <AccountsPanel
              key={account ?? ""}
              usersApi={usersApi}
              ordersApi={ordersApi}
              creditAdminApi={creditAdminApi}
              currentUserId={userId}
              initialAccountId={account}
              onNotify={onNotify}
            />
          ) : (
            <>
              <MockBadge service="user-service" />
              <p className="status">The account tools need user-service, so a fixture build cannot use them.</p>
            </>
          ))}

        {tab === "flagged" &&
          (creditAdminApi ? (
            <FlaggedWalletsView
              creditAdminApi={creditAdminApi}
              usersApi={usersApi}
              ordersApi={ordersApi}
              currentUserId={userId}
              onNotify={onNotify}
            />
          ) : (
            <NeedsService title="Flagged wallets" service="credit-service" />
          ))}

        {tab === "audit" && (
          <AuditLogView
            usersApi={usersApi}
            ordersApi={ordersApi}
            suppliersApi={suppliersApi}
            creditAdminApi={creditAdminApi}
            onOpenAccount={openAccount}
          />
        )}
      </div>
    </section>
  );
}
