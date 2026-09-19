// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the Credits screen from the owner's mockup, against the mock
//   credit-service.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState } from "react";
import { MockBadge } from "../../components/MockBadge";
import * as creditsApi from "./creditsApi";
import type { Balance, LedgerEntry } from "./types";

export function CreditsView() {
  const [balance, setBalance] = useState<Balance | null>(null);
  const [ledger, setLedger] = useState<LedgerEntry[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    Promise.all([creditsApi.getBalance(), creditsApi.listLedger()]).then(
      ([b, l]) => {
        if (cancelled) return;
        setBalance(b);
        setLedger(l);
        setLoading(false);
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  const total = balance ? balance.available + balance.held : 0;
  const availablePct = total ? (balance!.available / total) * 100 : 0;

  return (
    <section>
      <div className="page-head">
        <h1>Credits</h1>
        <p className="page-sub">
          Credits only move between students on this platform. They can&rsquo;t
          be bought, cashed out, or spent with a shop.
        </p>
      </div>

      <MockBadge service="credit-service" />

      {loading && <p className="status">Loading your balance…</p>}

      {!loading && balance && (
        <>
          <div className="card">
            <div className="balance-card">
              <div>
                <div className="balance-figure">{balance.available}</div>
                <div className="balance-label">available</div>
              </div>
              <div style={{ textAlign: "right" }}>
                <div className="balance-figure held">{balance.held}</div>
                <div className="balance-label">held on open errands</div>
              </div>
            </div>

            <div className="balance-bar" style={{ marginTop: 14 }}>
              <div style={{ display: "flex", height: "100%" }}>
                <div
                  className="balance-bar-available"
                  style={{ width: `${availablePct}%` }}
                />
                <div
                  className="balance-bar-held"
                  style={{ width: `${100 - availablePct}%` }}
                />
              </div>
            </div>

            <p className="balance-label" style={{ marginTop: 10 }}>
              Held credits return to you if an errand is cancelled or expires.
            </p>
          </div>

          <h2 className="section-label">History</h2>

          <div className="ledger">
            {ledger.map((entry) => (
              <div className="ledger-row" key={entry.id}>
                <div>
                  <div className="ledger-label">{entry.label}</div>
                  <div className="ledger-time">
                    {formatWhen(entry.occurredAt)}
                    {entry.note ? ` · ${entry.note}` : ""}
                  </div>
                </div>
                <div>
                  <div
                    className={`ledger-amount ${
                      entry.amount > 0 ? "positive" : ""
                    }`}
                  >
                    {entry.amount > 0 ? `+${entry.amount}` : entry.amount}
                  </div>
                  <div className="ledger-running">
                    {entry.runningAvailable} available
                  </div>
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}

/** "8 Sept, 19:06", as in the mockup. */
function formatWhen(iso: string): string {
  const date = new Date(iso);
  const day = date.toLocaleDateString("en-SG", {
    day: "numeric",
    month: "short",
  });
  const time = date.toLocaleTimeString("en-SG", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
  return `${day}, ${time}`;
}
