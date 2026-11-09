// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: One account's page inside the dashboard (D-117, D-150): its actions and
//   its review on their own, with a link back to where the admin came from, for
//   when they arrive from a list that is not the Accounts list, such as the
//   Flagged wallets tab.
// Author review: PENDING — <reviewer to complete>

import type { ToastMessage } from "../../components/Toast";
import type { CreditAdminApi } from "../credits/creditAdminApi";
import type { OrdersApi } from "../errands/ordersApi";
import type { UsersApi } from "../profile/usersApi";
import { AccountReview } from "./AccountReview";
import { AdminView } from "./AdminView";

interface AccountFocusViewProps {
  usersApi: UsersApi;
  ordersApi: OrdersApi | null;
  creditAdminApi: CreditAdminApi | null;
  /** The signed-in admin, so no action is offered on their own account. */
  currentUserId: string;
  uid: string;
  /** What to call the account's owner in a sentence; unknown when it was not on the list. */
  name?: string;
  /** Where Back goes, in words: "Flagged wallets". Absent when the account sits in a pane and needs no way back. */
  backLabel?: string;
  onBack?: () => void;
  onNotify: (message: ToastMessage) => void;
}

/**
 * The account's actions and review without the Accounts list around them. It is
 * the same pair the Accounts tab opens under a row (AdminView, then
 * AccountReview), so what an admin can do here is what they can do there.
 */
export function AccountFocusView({
  usersApi,
  ordersApi,
  creditAdminApi,
  currentUserId,
  uid,
  name,
  backLabel,
  onBack,
  onNotify,
}: AccountFocusViewProps) {
  return (
    <div>
      {backLabel && onBack && (
        <p style={{ margin: "0 0 12px" }}>
          <button type="button" className="link-btn" onClick={onBack}>
            &larr; Back to {backLabel.toLowerCase()}
          </button>
        </p>
      )}
      <div className="account-detail" style={{ marginTop: 0 }}>
        <AdminView
          key={uid}
          embedded
          usersApi={usersApi}
          currentUserId={currentUserId}
          initialAccountId={uid}
          onNotify={onNotify}
        />
        <AccountReview
          uid={uid}
          ordersApi={ordersApi}
          usersApi={usersApi}
          creditAdminApi={creditAdminApi}
          name={name}
        />
      </div>
    </div>
  );
}
