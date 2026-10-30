// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Usernames from user-service and ratings from order-service for the
//   requesters named on Find errands cards (D-093), fetched once per user.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useRef, useState } from "react";
import type { UsersApi } from "../profile/usersApi";
import type { OrdersApi } from "./ordersApi";
import type { UserRating } from "./orderTypes";

/**
 * What a card shows about one person. `undefined` while loading; `null` when
 * the service could not say.
 */
export interface Person {
  username: string | null | undefined;
  rating: UserRating | null | undefined;
}

/**
 * Each uid's username and rating, fetched the first time the uid appears and
 * kept for the life of the screen, so the five-second refresh asks nothing
 * again. Composes two services, each through its own client.
 */
export function usePeople(
  uids: readonly string[],
  usersApi: UsersApi | null,
  ordersApi: Pick<OrdersApi, "getUserRating">,
): ReadonlyMap<string, Person> {
  const [people, setPeople] = useState<ReadonlyMap<string, Person>>(new Map());
  const asked = useRef(new Set<string>());
  // Whether the screen is still up. Not per run of the effect below: a uid is
  // asked about once, so an answer must land even after the list has changed.
  const mounted = useRef(true);
  const key = [...new Set(uids)].sort().join(",");

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    const update = (uid: string, change: Partial<Person>) => {
      if (!mounted.current) return;
      setPeople((prev) => {
        const next = new Map(prev);
        next.set(uid, { ...(prev.get(uid) ?? { username: undefined, rating: undefined }), ...change });
        return next;
      });
    };
    for (const uid of key ? key.split(",") : []) {
      if (asked.current.has(uid)) continue;
      asked.current.add(uid);
      if (usersApi) {
        usersApi.getUser(uid).then(
          (p) => update(uid, { username: p.username || null }),
          () => update(uid, { username: null }),
        );
      } else {
        update(uid, { username: null });
      }
      ordersApi.getUserRating(uid).then(
        (rating) => update(uid, { rating }),
        () => update(uid, { rating: null }),
      );
    }
  }, [key, usersApi, ordersApi]);

  return people;
}
