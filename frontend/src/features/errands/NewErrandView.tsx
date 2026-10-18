// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the Request an errand screen from the owner's mockup. Supplier
//   list comes from the real supplier-service; posting goes to the mock
//   order-service.
// Author review: Nigeltzy - AI built it according to the prototype our team designed and the requirements provided.
// Per code and viewed UI, it seems valid and works as intended.

// AI-generated (edited by James Teo): rebuilt as Post an errand on the shared
// ErrandForm, posting to order-service with one duplicate-submission key per
// submission.
import { useState } from "react";
import type { ToastMessage } from "../../components/Toast";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { emptyErrandForm, ErrandForm, toCreateRequest, type ErrandFormValues } from "./ErrandForm";
import { createErrorMessage, creditsText, isShortOfCredits } from "./format";
import { newRequestId, type OrdersApi } from "./ordersApi";

interface NewErrandViewProps {
  /** Injected by App, which owns the token store the transport reads. */
  suppliersApi: SuppliersApi;
  // AI-generated (edited by James Teo).
  /** order-service's client, built by App over the same transport. */
  ordersApi: OrdersApi;
  // AI-generated (edited by James Teo): null while the balance is unknown,
  // which leaves the hint out rather than guessing.
  /** Spendable credits, for the "leaves you N available" hint. */
  available: number | null;
  onNotify: (message: ToastMessage) => void;
  // AI-generated (edited by James Teo): posting, leaving and the Credits link
  // are told apart.
  /** After a post succeeds: App refreshes the balance and shows My errands. */
  onPosted: () => void;
  /** Leaves without posting. */
  onCancel: () => void;
  /** Opens Credits, from the not-enough-credits message. */
  onOpenCredits: () => void;
}

// AI-generated (edited by James Teo): why the last post failed.
interface PostFailure {
  message: string;
  /** 422: the reward is more than the available credits. */
  shortOfCredits: boolean;
}

export function NewErrandView({
  suppliersApi,
  ordersApi,
  available,
  onNotify,
  onPosted,
  onCancel,
  onOpenCredits,
}: NewErrandViewProps) {
  // AI-generated (edited by James Teo): the post and its duplicate-submission
  // key (F7.2). The key is minted when the form opens and again only after a
  // post succeeds; every retry of a failed or unanswered post reuses it, so
  // the retry cannot post a second errand.
  const [posting, setPosting] = useState(false);
  const [requestId, setRequestId] = useState(newRequestId);
  const [failure, setFailure] = useState<PostFailure | null>(null);

  async function post(values: ErrandFormValues) {
    setPosting(true);
    setFailure(null);
    let reward: number;
    try {
      const detail = await ordersApi.createErrand(toCreateRequest(values), requestId);
      // A 200 for a key already used answers with the errand the first
      // attempt created, so its reward is the one held.
      reward = detail.errand.reward;
    } catch (err) {
      setFailure({ message: createErrorMessage(err), shortOfCredits: isShortOfCredits(err) });
      setPosting(false);
      return;
    }
    setRequestId(newRequestId());
    setPosting(false);
    onNotify({ text: `Errand posted · ${creditsText(reward)} held`, kind: "success" });
    onPosted();
  }

  // AI-generated (edited by James Teo): the page around the shared form. The
  // expiry time is not quoted: order-service sets it, and the errand's
  // detail shows the real deadline once it is posted.
  return (
    <section>
      <div className="page-head">
        <h1>Post an errand</h1>
        <p className="page-sub">
          Credits are held the moment you post. They come back to you if you cancel, or if
          nobody accepts the errand before it expires.
        </p>
      </div>

      <ErrandForm
        suppliersApi={suppliersApi}
        ordersApi={ordersApi}
        initial={emptyErrandForm()}
        alreadyHeld={0}
        available={available}
        submitting={posting}
        submitLabel={(reward) =>
          reward === null ? "Post errand" : `Post errand · hold ${creditsText(reward)}`
        }
        busyLabel="Posting…"
        cancelLabel="Cancel"
        onSubmit={(values) => void post(values)}
        onCancel={onCancel}
        feedback={
          failure && (
            <p className="status error" role="alert">
              {failure.message}
              {failure.shortOfCredits && (
                <>
                  {" "}
                  <button type="button" className="link-btn" onClick={onOpenCredits}>
                    See your credits
                  </button>
                </>
              )}
            </p>
          )
        }
      />
    </section>
  );
}
