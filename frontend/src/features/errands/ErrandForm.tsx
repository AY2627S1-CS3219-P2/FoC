// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: The errand fields shared by posting and editing an errand: a supplier
//   picker that shows availability, one item description with a counter,
//   delivery locations from order-service, an optional note, the reward with
//   a balance hint, and the client-side checks and request bodies behind them.
//   Focus returns to the submit button after a submit that fails, and inside
//   a dialog Escape with unsaved changes goes to the cancel button rather
//   than closing it. Edits can be kept when the errand changes under them.
// Author review: James Teo — reviewed and verified.

import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { SearchIcon } from "../../components/icons";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import type { Supplier } from "../suppliers/types";
import { creditsText } from "./format";
import type { OrdersApi } from "./ordersApi";
import type { CampusLocation, CreateErrandRequest, Errand, ErrandChanges } from "./orderTypes";

/**
 * order-service's limits, from its spec's CreateErrandRequest. The checks
 * here are UX only, so a student hears about a slip before posting;
 * order-service checks everything again and its answer is the one that
 * counts.
 */
export const MAX_ITEM_LENGTH = 280;
export const MAX_NOTE_LENGTH = 140;

/** The supplier an errand collects from: its id is sent, its name shown. */
export interface ChosenSupplier {
  id: string;
  name: string;
}

/** What the form holds. */
export interface ErrandFormValues {
  supplier: ChosenSupplier | null;
  itemDescription: string;
  deliveryLocation: string;
  deliveryNote: string;
  /** As typed, so a half-typed number can sit in the box. */
  reward: string;
}

export type ErrandFormField =
  | "supplier"
  | "itemDescription"
  | "deliveryLocation"
  | "deliveryNote"
  | "reward";

/** One message per field that needs fixing; an empty object means none. */
export type ErrandFormProblems = Partial<Record<ErrandFormField, string>>;

/** The order the fields appear in, so the first problem can take focus. */
const FIELD_ORDER: ErrandFormField[] = [
  "supplier",
  "itemDescription",
  "deliveryLocation",
  "deliveryNote",
  "reward",
];

/** Characters as order-service counts them: code points, so an emoji is one. */
export function textLength(text: string): number {
  return Array.from(text).length;
}

/** A blank form for a new errand, with the reward the old form started at. */
export function emptyErrandForm(): ErrandFormValues {
  return {
    supplier: null,
    itemDescription: "",
    deliveryLocation: "",
    deliveryNote: "",
    reward: "10",
  };
}

/** The form filled from an errand, for editing it. */
export function formValuesFrom(errand: Errand): ErrandFormValues {
  return {
    supplier: { id: errand.supplier.id, name: errand.supplier.name },
    itemDescription: errand.itemDescription,
    deliveryLocation: errand.deliveryLocation,
    deliveryNote: errand.deliveryNote ?? "",
    reward: String(errand.reward),
  };
}

/** The reward as a whole number, or null when the text is not one. */
export function parseReward(text: string): number | null {
  const trimmed = text.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const reward = Number(trimmed);
  return Number.isSafeInteger(reward) ? reward : null;
}

/**
 * The locations grouped by area, in the order order-service lists them, for
 * the form's grouped dropdown. A phone shows the groups in its own picker.
 */
export function groupByArea(locations: readonly CampusLocation[]): { area: string; names: string[] }[] {
  const groups: { area: string; names: string[] }[] = [];
  for (const { name, area } of locations) {
    const group = groups.find((g) => g.area === area);
    if (group) group.names.push(name);
    else groups.push({ area, names: [name] });
  }
  return groups;
}

/**
 * The Deliver to names the form accepts, and those it shows outside the area
 * groups. An errand saved at a place the list no longer has keeps it: it is
 * accepted while unchanged, since an unchanged location is never sent, and
 * stays pickable after another place is chosen. While the list loads, the
 * box's own value is shown so it never opens blank.
 */
export function locationChoices(
  locations: readonly CampusLocation[],
  saved: string,
  current: string,
): { accepted: string[]; extra: string[] } {
  const listed = locations.map((location) => location.name);
  const savedUnlisted = saved && !listed.includes(saved) ? saved : "";
  const extra = [savedUnlisted, current].filter(
    (name, i, all) => name !== "" && !listed.includes(name) && all.indexOf(name) === i,
  );
  return { accepted: savedUnlisted ? [...listed, savedUnlisted] : listed, extra };
}

/** The Deliver to options: any `extra` names first, then each area's places under its heading. */
export function LocationOptions({
  locations,
  extra,
}: {
  locations: readonly CampusLocation[];
  extra: readonly string[];
}) {
  return (
    <>
      {extra.map((name) => (
        <option key={`extra:${name}`} value={name}>
          {name}
        </option>
      ))}
      {groupByArea(locations).map((group) => (
        <optgroup key={group.area} label={group.area}>
          {group.names.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </optgroup>
      ))}
    </>
  );
}

/**
 * The fields that need fixing before a submit is worth sending. Only the
 * shape is checked (required, lengths, a whole reward, a listed location);
 * whether the supplier takes errands now, and whether the reward is
 * affordable, is order-service's to answer.
 */
export function validateErrandForm(
  values: ErrandFormValues,
  locations: readonly string[],
): ErrandFormProblems {
  const problems: ErrandFormProblems = {};
  if (!values.supplier) problems.supplier = "Choose a supplier to collect from.";

  const item = values.itemDescription.trim();
  if (!item) problems.itemDescription = "Say what to get.";
  else if (textLength(item) > MAX_ITEM_LENGTH) {
    problems.itemDescription = `Keep it to ${MAX_ITEM_LENGTH} characters.`;
  }

  if (!values.deliveryLocation) problems.deliveryLocation = "Choose where to deliver it.";
  else if (!locations.includes(values.deliveryLocation)) {
    problems.deliveryLocation = "Choose a location from the list.";
  }

  if (textLength(values.deliveryNote.trim()) > MAX_NOTE_LENGTH) {
    problems.deliveryNote = `Keep the note to ${MAX_NOTE_LENGTH} characters.`;
  }

  const reward = parseReward(values.reward);
  if (reward === null || reward < 1) {
    problems.reward = "The reward must be a whole number of credits, at least 1.";
  }
  return problems;
}

/**
 * The body of `POST /api/orders`: the item and note trimmed, and the note
 * left out when blank. Meant for values validateErrandForm passed.
 */
export function toCreateRequest(values: ErrandFormValues): CreateErrandRequest {
  const note = values.deliveryNote.trim();
  return {
    supplierId: values.supplier?.id ?? "",
    itemDescription: values.itemDescription.trim(),
    deliveryLocation: values.deliveryLocation,
    deliveryNote: note || undefined,
    reward: parseReward(values.reward) ?? 0,
  };
}

/**
 * What an amend sends besides `version`: only the fields that differ from
 * `errand`, the copy the edit started from. A note emptied out is sent as
 * "", which clears it. An empty result means nothing changed.
 */
export function errandChanges(errand: Errand, values: ErrandFormValues): ErrandChanges {
  const changes: ErrandChanges = {};
  if (values.supplier && values.supplier.id !== errand.supplier.id) {
    changes.supplierId = values.supplier.id;
  }
  const item = values.itemDescription.trim();
  if (item !== errand.itemDescription) changes.itemDescription = item;
  if (values.deliveryLocation !== errand.deliveryLocation) {
    changes.deliveryLocation = values.deliveryLocation;
  }
  const note = values.deliveryNote.trim();
  if (note !== (errand.deliveryNote ?? "")) changes.deliveryNote = note;
  const reward = parseReward(values.reward);
  if (reward !== null && reward !== errand.reward) changes.reward = reward;
  return changes;
}

/** Whether anything in the boxes differs from what the form opened with. */
export function formChanged(initial: ErrandFormValues, values: ErrandFormValues): boolean {
  return (
    initial.supplier?.id !== values.supplier?.id ||
    initial.itemDescription !== values.itemDescription ||
    initial.deliveryLocation !== values.deliveryLocation ||
    initial.deliveryNote !== values.deliveryNote ||
    initial.reward !== values.reward
  );
}

/**
 * The form after the errand it edits changed underneath it (a 409 on save,
 * then the latest errand fetched): each field the student changed from
 * `before`, the errand they started from, keeps what they typed, and every
 * other field takes the latest errand's value.
 */
export function keepEdits(
  before: ErrandFormValues,
  typed: ErrandFormValues,
  latest: ErrandFormValues,
): ErrandFormValues {
  const keep = <K extends Exclude<keyof ErrandFormValues, "supplier">>(key: K) =>
    typed[key] !== before[key] ? typed[key] : latest[key];
  return {
    supplier: typed.supplier?.id !== before.supplier?.id ? typed.supplier : latest.supplier,
    itemDescription: keep("itemDescription"),
    deliveryLocation: keep("deliveryLocation"),
    deliveryNote: keep("deliveryNote"),
    reward: keep("reward"),
  };
}

/**
 * The balance hint under the reward: what would be left available, or how
 * far short the requester is. `alreadyHeld` is what this errand holds now,
 * which the new reward replaces (0 when posting). Null when either number is
 * unknown. A hint only: order-service and credit-service decide.
 */
export function balanceHint(
  available: number | null,
  reward: number | null,
  alreadyHeld: number,
): string | null {
  if (available === null || reward === null) return null;
  const left = available - (reward - alreadyHeld);
  return left >= 0
    ? `Leaves you ${creditsText(left)} available.`
    : `You're ${creditsText(-left)} short of this reward.`;
}

interface ErrandFormProps {
  /** Injected by the screen, which App gives the clients. */
  suppliersApi: SuppliersApi;
  /** For the delivery locations. */
  ordersApi: OrdersApi;
  initial: ErrandFormValues;
  /**
   * What the boxes open with when it is not `initial`, such as edits kept
   * after the errand changed under them (keepEdits). Unsaved changes are
   * still measured against `initial`, the errand as saved.
   */
  startWith?: ErrandFormValues;
  /** Told what the boxes hold whenever it changes, so the screen can keep it. */
  onValuesChange?: (values: ErrandFormValues) => void;
  /** Credits this errand holds now: its reward when editing, 0 when posting. */
  alreadyHeld: number;
  /** The requester's available credits, or null to leave out the hint. */
  available: number | null;
  /** A quiet note above the fields. */
  notice?: string;
  /** True while the submit is in flight: every field and button is disabled. */
  submitting: boolean;
  /** The submit button's text for the reward in the box (null when not a number). */
  submitLabel: (reward: number | null) => string;
  busyLabel: string;
  cancelLabel: string;
  /** Called with values that passed validateErrandForm. */
  onSubmit: (values: ErrandFormValues) => void;
  onCancel: () => void;
  /** The last submit's failure and any follow-up, shown above the buttons. */
  feedback?: ReactNode;
}

export function ErrandForm({
  suppliersApi,
  ordersApi,
  initial,
  startWith,
  onValuesChange,
  alreadyHeld,
  available,
  notice,
  submitting,
  submitLabel,
  busyLabel,
  cancelLabel,
  onSubmit,
  onCancel,
  feedback,
}: ErrandFormProps) {
  const id = useId();
  const fieldId = (field: string) => `${id}-${field}`;

  const [values, setValues] = useState<ErrandFormValues>(startWith ?? initial);
  useEffect(() => {
    onValuesChange?.(values);
  }, [values, onValuesChange]);
  const [showProblems, setShowProblems] = useState(false);

  const [search, setSearch] = useState("");
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [suppliersState, setSuppliersState] = useState<"loading" | "ready" | "failed">("loading");
  const [suppliersAttempt, setSuppliersAttempt] = useState(0);

  // The fieldset disables every control while a submit is in flight, and a
  // disabled button loses focus to the page. When the submit fails and the
  // form is back, focus returns to the submit button beside the message.
  const submitRef = useRef<HTMLButtonElement>(null);
  const wasSubmitting = useRef(submitting);
  useEffect(() => {
    const lost = document.activeElement === null || document.activeElement === document.body;
    if (wasSubmitting.current && !submitting && lost) submitRef.current?.focus();
    wasSubmitting.current = submitting;
  }, [submitting]);

  // Inside a dialog, Escape would close it and drop what was typed. While
  // anything has changed, Escape is marked handled, which keeps the dialog
  // open, and focus goes to the cancel button, so Enter confirms dropping the
  // changes. Listened for on the document, so it applies wherever focus is,
  // even on the page while a submit has the buttons disabled.
  const formRef = useRef<HTMLFormElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const changed = formChanged(initial, values);
  useEffect(() => {
    if (!changed || !formRef.current?.closest('[role="dialog"]')) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      e.preventDefault();
      cancelRef.current?.focus();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [changed]);

  const [locations, setLocations] = useState<CampusLocation[]>([]);
  const [locationsState, setLocationsState] = useState<"loading" | "ready" | "failed">("loading");
  const [locationsAttempt, setLocationsAttempt] = useState(0);

  // Debounced so typing in the search box does not send a request per key.
  useEffect(() => {
    let cancelled = false;
    setSuppliersState("loading");
    const timer = setTimeout(() => {
      suppliersApi.listSuppliers({ search }).then(
        (result) => {
          if (cancelled) return;
          setSuppliers(result);
          setSuppliersState("ready");
        },
        () => {
          if (cancelled) return;
          setSuppliers([]);
          setSuppliersState("failed");
        },
      );
    }, 300);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [suppliersApi, search, suppliersAttempt]);

  useEffect(() => {
    let cancelled = false;
    setLocationsState("loading");
    ordersApi.listLocations().then(
      (result) => {
        if (cancelled) return;
        setLocations(result);
        setLocationsState("ready");
      },
      () => {
        if (!cancelled) setLocationsState("failed");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [ordersApi, locationsAttempt]);

  const choices = locationChoices(locations, initial.deliveryLocation, values.deliveryLocation);
  const problems = validateErrandForm(values, choices.accepted);
  const shown = (field: ErrandFormField) => (showProblems ? problems[field] : undefined);
  const reward = parseReward(values.reward);
  const hint = balanceHint(available, reward, alreadyHeld);
  const itemLength = textLength(values.itemDescription.trim());
  const noteLength = textLength(values.deliveryNote.trim());

  const set = <K extends keyof ErrandFormValues>(key: K, value: ErrandFormValues[K]) =>
    setValues((prev) => ({ ...prev, [key]: value }));

  function stepReward(by: number) {
    const current = parseReward(values.reward) ?? 0;
    set("reward", String(Math.max(1, current + by)));
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    const first = FIELD_ORDER.find((field) => problems[field]);
    if (first) {
      setShowProblems(true);
      document.getElementById(fieldId(first))?.focus();
      return;
    }
    onSubmit(values);
  }

  /** aria-describedby for a field: its hint or counter, and its problem when shown. */
  const describedBy = (field: ErrandFormField, extra?: string) =>
    [extra, shown(field) ? fieldId(`${field}-problem`) : undefined].filter(Boolean).join(" ") ||
    undefined;

  const problemText = (field: ErrandFormField) =>
    shown(field) ? (
      <p className="field-error" id={fieldId(`${field}-problem`)}>
        {shown(field)}
      </p>
    ) : null;

  return (
    <form onSubmit={submit} noValidate ref={formRef}>
      {notice && <p className="field-hint form-notice">{notice}</p>}

      <fieldset className="errand-form" disabled={submitting} aria-busy={submitting}>
        <div className="errand-form-col">
          <label className="field-label" htmlFor={fieldId("supplier")}>
            Collect from
          </label>
          <div className="search-field">
            <span className="search-field-icon">
              <SearchIcon />
            </span>
            <input
              id={fieldId("supplier")}
              type="search"
              placeholder="Search suppliers and campus locations"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              aria-invalid={shown("supplier") ? true : undefined}
              aria-describedby={describedBy("supplier", fieldId("chosen"))}
            />
          </div>
          <p className="field-hint" id={fieldId("chosen")}>
            {values.supplier ? `Chosen: ${values.supplier.name}` : "No supplier chosen yet."}
          </p>
          {problemText("supplier")}

          {suppliersState === "failed" ? (
            <p className="status error" role="alert">
              Couldn&rsquo;t load the suppliers.{" "}
              <button
                type="button"
                className="link-btn"
                onClick={() => setSuppliersAttempt((n) => n + 1)}
              >
                Retry
              </button>
            </p>
          ) : (
            <div className="picker" role="group" aria-label="Suppliers" aria-busy={suppliersState === "loading"}>
              {suppliersState === "loading" && suppliers.length === 0 && (
                <p className="picker-empty">Loading suppliers…</p>
              )}
              {suppliersState === "ready" && suppliers.length === 0 && (
                <p className="picker-empty">No suppliers match that search.</p>
              )}
              {suppliers.map((supplier) => {
                const chosen = values.supplier?.id === supplier.id;
                return (
                  <button
                    type="button"
                    key={supplier.id}
                    className={`picker-row ${chosen ? "selected" : ""}`}
                    aria-pressed={chosen}
                    // An unavailable supplier is shown but can't be picked;
                    // order-service refuses it anyway.
                    disabled={!supplier.is_available && !chosen}
                    onClick={() => set("supplier", { id: supplier.id, name: supplier.name })}
                  >
                    <span className="tag-building">{supplier.building}</span>
                    <span className="picker-row-text">
                      <span className="picker-row-name">{supplier.name}</span>
                      <span className="picker-row-meta">
                        {supplier.type}
                        {supplier.opening_time && supplier.closing_time
                          ? ` · ${supplier.opening_time}–${supplier.closing_time}`
                          : ""}
                      </span>
                    </span>
                    {chosen ? (
                      <span className="picker-row-chosen">Chosen</span>
                    ) : (
                      !supplier.is_available && (
                        <span className="badge badge-unavailable">Unavailable</span>
                      )
                    )}
                  </button>
                );
              })}
            </div>
          )}

          <div className="field field-spaced">
            <label htmlFor={fieldId("itemDescription")}>What to get</label>
            <textarea
              id={fieldId("itemDescription")}
              value={values.itemDescription}
              placeholder="1 × Iced latte, less ice"
              // order-service takes the item on one line, so a pasted list
              // becomes one line too.
              onChange={(e) => set("itemDescription", e.target.value.replace(/\r?\n/g, " "))}
              aria-invalid={shown("itemDescription") ? true : undefined}
              aria-describedby={describedBy("itemDescription", fieldId("item-count"))}
            />
            <CharCount id={fieldId("item-count")} length={itemLength} max={MAX_ITEM_LENGTH} />
            {problemText("itemDescription")}
          </div>
        </div>

        <div className="errand-form-col">
          <div className="field">
            <label htmlFor={fieldId("deliveryLocation")}>Deliver to</label>
            <select
              id={fieldId("deliveryLocation")}
              value={values.deliveryLocation}
              onChange={(e) => set("deliveryLocation", e.target.value)}
              disabled={locationsState !== "ready"}
              aria-invalid={shown("deliveryLocation") ? true : undefined}
              aria-describedby={describedBy("deliveryLocation")}
            >
              <option value="" disabled>
                {locationsState === "loading" ? "Loading locations…" : "Choose a campus location"}
              </option>
              <LocationOptions locations={locations} extra={choices.extra} />
            </select>
            {locationsState === "failed" && (
              <p className="field-error" role="alert">
                Couldn&rsquo;t load the delivery locations.{" "}
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => setLocationsAttempt((n) => n + 1)}
                >
                  Retry
                </button>
              </p>
            )}
            {problemText("deliveryLocation")}
          </div>

          <div className="field">
            <label htmlFor={fieldId("deliveryNote")}>
              How to find you <span className="field-optional">optional</span>
            </label>
            <textarea
              id={fieldId("deliveryNote")}
              value={values.deliveryNote}
              placeholder="Third table from the window, grey backpack"
              onChange={(e) => set("deliveryNote", e.target.value)}
              aria-invalid={shown("deliveryNote") ? true : undefined}
              aria-describedby={describedBy("deliveryNote", fieldId("note-count"))}
            />
            <CharCount id={fieldId("note-count")} length={noteLength} max={MAX_NOTE_LENGTH} />
            {problemText("deliveryNote")}
          </div>

          <label className="field-label" htmlFor={fieldId("reward")}>
            Reward
          </label>
          <div className="stepper">
            <button
              type="button"
              onClick={() => stepReward(-1)}
              disabled={(reward ?? 0) <= 1}
              aria-label="Decrease reward"
            >
              −
            </button>
            <div>
              <input
                id={fieldId("reward")}
                className="stepper-input"
                type="number"
                inputMode="numeric"
                min={1}
                step={1}
                value={values.reward}
                onChange={(e) => set("reward", e.target.value)}
                aria-invalid={shown("reward") ? true : undefined}
                aria-describedby={describedBy("reward", hint ? fieldId("reward-hint") : undefined)}
              />
              <div className="stepper-unit">credits</div>
            </div>
            <button type="button" onClick={() => stepReward(1)} aria-label="Increase reward">
              +
            </button>
          </div>
          {hint && (
            <p className="balance-label field-hint" id={fieldId("reward-hint")}>
              {hint}
            </p>
          )}
          {problemText("reward")}

          <div className="card form-summary">
            <dl className="summary-list">
              <div className="summary-row">
                <dt>Collect from</dt>
                <dd>{values.supplier ? values.supplier.name : "Not chosen"}</dd>
              </div>
              <div className="summary-row">
                <dt>Deliver to</dt>
                <dd>{values.deliveryLocation || "Not chosen"}</dd>
              </div>
              <div className="summary-row">
                <dt>Reward</dt>
                <dd>{reward === null ? "—" : creditsText(reward)}</dd>
              </div>
            </dl>

            {feedback}

            <div className="btn-row">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={onCancel}
                ref={cancelRef}
              >
                {cancelLabel}
              </button>
              <button
                type="submit"
                className="btn btn-primary"
                disabled={locationsState !== "ready"}
                ref={submitRef}
              >
                {submitting ? busyLabel : submitLabel(reward)}
              </button>
            </div>
          </div>
        </div>
      </fieldset>
    </form>
  );
}

/** "12 / 280", which says in words when the text is over. */
function CharCount({ id, length, max }: { id: string; length: number; max: number }) {
  const over = length - max;
  return (
    <p className={`char-count ${over > 0 ? "over" : ""}`} id={id}>
      {length} / {max}
      {over > 0 ? ` · ${over} too many` : ""}
    </p>
  );
}
