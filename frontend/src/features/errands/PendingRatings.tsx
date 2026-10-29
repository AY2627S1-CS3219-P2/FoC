// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Asking a student to rate their completed errands (D-093). Until
//   they do, order-service refuses their next post or accept, and this says so.
// Author review: PENDING — <reviewer to complete>

import { useState } from "react";
import type { ToastMessage } from "../../components/Toast";
import { usePolling } from "../../lib/polling";
import type { UsersApi } from "../profile/usersApi";
import { orderErrorMessage } from "./format";
import type { OrdersApi } from "./ordersApi";
import type { PendingRating } from "./orderTypes";
import { StarInput } from "./ratings";
import { usePeople } from "./usePeople";

interface PendingRatingsProps {
  ordersApi: OrdersApi;
  usersApi: UsersApi | null;
  onNotify: (message: ToastMessage) => void;
}

/** Shows nothing until there is an errand to rate. Checked every 30 s. */
export function PendingRatings({ ordersApi, usersApi, onNotify }: PendingRatingsProps) {
  const [pending, setPending] = useState<PendingRating[]>([]);
  const refresh = usePolling(
    async (isActive) => {
      const items = await ordersApi.listPendingRatings();
      if (isActive()) setPending(items);
    },
    30_000,
    [ordersApi],
  );
  const people = usePeople(
    pending.map((p) => p.rateeId),
    usersApi,
    ordersApi,
  );

  if (pending.length === 0) return null;
  return (
    <section className="pending-ratings" aria-labelledby="pending-ratings-title">
      <h2 id="pending-ratings-title">Rate your completed errand{pending.length > 1 ? "s" : ""}</h2>
      <p className="list-note">
        You can&rsquo;t post or accept another errand until you rate{" "}
        {pending.length > 1 ? "these" : "this one"}. A rating can&rsquo;t be changed later.
      </p>
      {pending.map((p) => (
        <RateOne
          key={p.errandId}
          pending={p}
          name={people.get(p.rateeId)?.username}
          ordersApi={ordersApi}
          onDone={() => {
            setPending((prev) => prev.filter((x) => x.errandId !== p.errandId));
            onNotify({ text: "Thanks — your rating is saved.", kind: "success" });
            refresh();
          }}
        />
      ))}
    </section>
  );
}

interface RateOneProps {
  pending: PendingRating;
  /** The other side's username; undefined while loading, null if unknown. */
  name: string | null | undefined;
  ordersApi: OrdersApi;
  onDone: () => void;
}

function RateOne({ pending, name, ordersApi, onDone }: RateOneProps) {
  const [stars, setStars] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const who = name ?? (pending.raterRole === "requester" ? "your courier" : "the requester");
  const role = pending.raterRole === "requester" ? "courier" : "requester";

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await ordersApi.rate(pending.errandId, stars);
      onDone();
    } catch (err) {
      setError(orderErrorMessage(err));
      setBusy(false);
    }
  }

  return (
    <div className="pending-rating">
      <p>
        How was <strong>{who}</strong> as your {role} for &ldquo;{pending.itemDescription}&rdquo;?
      </p>
      <div className="pending-rating-row">
        <StarInput
          value={stars}
          onChange={setStars}
          label={`Rate ${who} from 1 to 5 stars`}
          disabled={busy}
        />
        <button
          type="button"
          className="btn btn-primary"
          disabled={stars === 0 || busy}
          onClick={() => void submit()}
        >
          {busy ? "Saving…" : "Submit rating"}
        </button>
      </div>
      {error && (
        <p className="status error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
