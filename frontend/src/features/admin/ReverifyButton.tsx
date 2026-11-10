// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: The Re-verify button (D-150): asks credit-service to re-run the ledger
//   checks on one wallet and hands what happened back as a sentence. Shared by
//   the Flagged wallets tab and an account's review.
// Author review: PENDING — <reviewer to complete>

import { useState } from "react";
import type { CreditAdminApi } from "../credits/creditAdminApi";
import { reverifyMessage, type ReverifyMessage, type ReverifyResult } from "../credits/creditAdminTypes";

interface ReverifyButtonProps {
  creditAdminApi: CreditAdminApi;
  /** The wallet's owner. */
  userId: string;
  /** What to call them in the sentence about what happened. */
  name: string;
  /**
   * Called once the answer is in. `result` is null when the request failed, and
   * `message` then says so; otherwise it is credit-service's answer in words.
   * The parent shows the message, so it outlives a row that the answer removes.
   */
  onDone: (message: ReverifyMessage, result: ReverifyResult | null) => void;
  /** Lets a parent keep two buttons from running at once. */
  disabled?: boolean;
}

/**
 * Re-verify clears a flag only when every check passes; it never edits a
 * balance (D-150). Whether to offer it is the parent's call: credit-service
 * answers NOT_FLAGGED for a wallet that is fine.
 */
export function ReverifyButton({ creditAdminApi, userId, name, onDone, disabled = false }: ReverifyButtonProps) {
  const [busy, setBusy] = useState(false);

  async function run() {
    setBusy(true);
    try {
      const result = await creditAdminApi.reverify(userId);
      onDone(reverifyMessage(result, name), result);
    } catch (err) {
      onDone(
        { kind: "error", text: err instanceof Error ? err.message : "The Re-verify failed. Try again." },
        null,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <button type="button" className="btn btn-secondary" onClick={run} disabled={busy || disabled}>
      {busy ? "Re-verifying…" : "Re-verify"}
    </button>
  );
}
