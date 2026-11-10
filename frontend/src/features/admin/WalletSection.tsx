// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: The wallet block of an account's review (D-150): the account as
//   credit-service stores it, with its status, and for a flagged one why and a
//   Re-verify button.
// Author review: PENDING — <reviewer to complete>

import { useCallback, useEffect, useState } from "react";
import type { CreditAdminApi } from "../credits/creditAdminApi";
import { amountsText, reasonLabel, type AdminWallet, type ReverifyMessage } from "../credits/creditAdminTypes";
import { formatDateTime } from "../errands/time";
import { ReverifyButton } from "./ReverifyButton";

interface WalletSectionProps {
  creditAdminApi: CreditAdminApi;
  uid: string;
  /** What to call the account's owner in a sentence. */
  name: string;
}

type Loaded = { wallet: AdminWallet | null } | { error: string } | null;

/**
 * Loads on its own, like the rest of the review, so credit-service being down
 * leaves the other blocks readable. A flagged wallet's balances are shown but
 * labelled: credit-service does not trust them while it is flagged.
 */
export function WalletSection({ creditAdminApi, uid, name }: WalletSectionProps) {
  const [loaded, setLoaded] = useState<Loaded>(null);
  const [message, setMessage] = useState<ReverifyMessage | null>(null);

  const load = useCallback(() => {
    let live = true;
    creditAdminApi.getWallet(uid).then(
      (wallet) => live && setLoaded({ wallet }),
      (err) => live && setLoaded({ error: err instanceof Error ? err.message : "Couldn't load this." }),
    );
    return () => {
      live = false;
    };
  }, [creditAdminApi, uid]);

  useEffect(() => {
    setLoaded(null);
    setMessage(null);
    return load();
  }, [load]);

  return (
    <div className="review-section">
      <h4 className="field-label">Wallet</h4>
      {loaded === null && (
        <p className="status" role="status">
          Loading…
        </p>
      )}
      {loaded && "error" in loaded && (
        <p className="status error" role="alert">
          {loaded.error}
        </p>
      )}
      {loaded && "wallet" in loaded && loaded.wallet === null && <p className="audit-meta">No credit account yet.</p>}
      {loaded && "wallet" in loaded && loaded.wallet && (
        <>
          <dl className="detail-list">
            <Row label="Available" value={String(loaded.wallet.availableCredits)} />
            <Row label="Held on errands" value={String(loaded.wallet.reservedCredits)} />
            <Row label="Status" value={loaded.wallet.integrityStatus === "FLAGGED" ? "Flagged" : "Consistent"} />
          </dl>
          {loaded.wallet.integrityStatus === "FLAGGED" && (
            <>
              <p className="status error" role="alert">
                {flaggedText(loaded.wallet)}
              </p>
              <p className="audit-meta">
                These are the stored figures. They aren&rsquo;t trusted while the wallet is flagged.
              </p>
              <div className="btn-row" style={{ justifyContent: "flex-start" }}>
                <ReverifyButton
                  creditAdminApi={creditAdminApi}
                  userId={uid}
                  name={name}
                  onDone={(m) => {
                    setMessage(m);
                    load();
                  }}
                />
              </div>
            </>
          )}
          {message && (
            <p className={message.kind === "error" ? "status error" : "status"} role={message.kind === "error" ? "alert" : "status"}>
              {message.text}
            </p>
          )}
        </>
      )}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

/** Why the wallet is flagged, with when and the amounts that disagree. */
function flaggedText(w: AdminWallet): string {
  const parts = [reasonLabel(w.reasonCode)];
  const amounts = amountsText(w.expected, w.observed);
  if (amounts) parts.push(amounts.toLowerCase());
  const since = w.flaggedAt ? formatDateTime(w.flaggedAt) : "";
  return `Flagged${since ? ` ${since}` : ""}: ${parts.join(" — ")}.`;
}
