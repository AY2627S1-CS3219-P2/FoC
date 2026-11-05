// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The filter row the dashboard's Active errands and Past errands tabs
//   share (D-105): state, supplier, a date range and a sort.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState } from "react";
import type { Supplier } from "../suppliers/types";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { STATE_LABEL } from "./format";
import type { AdminErrandSort, ErrandStatus } from "./orderTypes";

/** What the row filters by; the tab passes it to order-service's admin list. */
export interface ErrandFilterValues {
  state: ErrandStatus | "";
  supplierId: string;
  /** YYYY-MM-DD, or "" for no lower bound. */
  from: string;
  /** YYYY-MM-DD, or "" for no upper bound. */
  to: string;
  sort: AdminErrandSort;
}

interface ErrandFiltersProps {
  /** Keeps the ids of two rows on one page apart. */
  idPrefix: string;
  value: ErrandFilterValues;
  /** The states the tab can hold. */
  states: readonly ErrandStatus[];
  /** What the two dates are measured against, e.g. "Ended". */
  dateLabel: string;
  sorts: readonly { value: AdminErrandSort; label: string }[];
  suppliers: Supplier[];
  onChange: (next: ErrandFilterValues) => void;
}

/** A row of labelled filters; every change is sent up whole. */
export function ErrandFilters({ idPrefix, value, states, dateLabel, sorts, suppliers, onChange }: ErrandFiltersProps) {
  const set = <K extends keyof ErrandFilterValues>(key: K, v: ErrandFilterValues[K]) =>
    onChange({ ...value, [key]: v });
  return (
    <div className="filter-row">
      <div className="field">
        <label htmlFor={`${idPrefix}-state`}>State</label>
        <select id={`${idPrefix}-state`} value={value.state} onChange={(e) => set("state", e.target.value as ErrandStatus | "")}>
          <option value="">Any</option>
          {states.map((s) => (
            <option key={s} value={s}>
              {STATE_LABEL[s]}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor={`${idPrefix}-supplier`}>Supplier</label>
        <select id={`${idPrefix}-supplier`} value={value.supplierId} onChange={(e) => set("supplierId", e.target.value)}>
          <option value="">Any</option>
          {suppliers.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor={`${idPrefix}-from`}>{dateLabel} from</label>
        <input id={`${idPrefix}-from`} type="date" value={value.from} onChange={(e) => set("from", e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor={`${idPrefix}-to`}>{dateLabel} before</label>
        <input id={`${idPrefix}-to`} type="date" value={value.to} onChange={(e) => set("to", e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor={`${idPrefix}-sort`}>Sort</label>
        <select id={`${idPrefix}-sort`} value={value.sort} onChange={(e) => set("sort", e.target.value as AdminErrandSort)}>
          {sorts.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

/** The suppliers the filter offers. A failed load only leaves the list empty. */
export function useSupplierChoices(suppliersApi: SuppliersApi): Supplier[] {
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  useEffect(() => {
    let live = true;
    suppliersApi
      .listSuppliers()
      .then((s) => live && setSuppliers(s))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [suppliersApi]);
  return suppliers;
}
