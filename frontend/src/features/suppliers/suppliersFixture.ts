// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-09-27
// Scope: Fixture stand-in for supplier-service in a fixture build
//   (VITE_USE_FIXTURES=true), seeded from data/csv/supplier-seed-data.csv the
//   way supplier-service's seed.go loads it.
// Author review: nigeltzy

import { mockDelay } from "../../lib/mock";
import { SupplierApiError, type SuppliersApi } from "./suppliersApi";
import type { Supplier, SupplierFilter, SupplierInput } from "./types";

/**
 * Stands in for supplier-service when the app runs on fixtures, so Suppliers
 * and New errand work without a gateway. It stores and returns; validation
 * and duplicate checks stay with supplier-service.
 *
 * The rows are data/csv/supplier-seed-data.csv as supplier-service's seed.go
 * loads it: hours as HH:MM, every supplier available, no description. The list
 * order and filters follow its list query.
 */

type SeedRow = Omit<
  Supplier,
  "id" | "description" | "is_available" | "created_at" | "updated_at"
>;

const SEEDED_AT = "2026-09-21T00:00:00Z";

/** supplier-service's ErrNotFound message. */
const NOT_FOUND = "supplier not found";

function seed(n: number, row: SeedRow): Supplier {
  return {
    ...row,
    id: `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`,
    description: "",
    is_available: true,
    created_at: SEEDED_AT,
    updated_at: SEEDED_AT,
  };
}

const SEED: readonly Supplier[] = [
  seed(1, {
    name: "Anna's x Soup Union",
    type: "Food",
    building: "Central Library",
    floor: "1",
    location_description: "Next to NUS Co-op",
    latitude: 1.296444,
    longitude: 103.773032,
    opening_time: "09:00",
    closing_time: "18:00",
    image_url: "https://raw.githubusercontent.com/CS3219-AY2627S1/FoC-Template/main/data/images/ANNA.jpeg",
  }),
  seed(2, {
    name: "NUS Co-op",
    type: "Shopping",
    building: "Central Library",
    floor: "1",
    location_description: "Inside the library on the right side",
    latitude: 1.2967866,
    longitude: 103.7732677,
    opening_time: "09:00",
    closing_time: "16:00",
    image_url: "https://lh3.googleusercontent.com/grass-cs/ACvplmMm2Wd6iUHwM3HCh9UtZklJe3U2pTKH9vLiMoCqxNxogVxfZFjS63xwHQDcuXfSqstx8jvv8bAgCYzGjm12xV9z1zUPN2R7yHn5INhxHt2VykvuCDsJBqmNe4xX05XhthJn1JfVpnOLkHmw=s1360-w1360-h1020-rw",
  }),
  seed(3, {
    name: "Printer @ Com 2",
    type: "Printing",
    building: "Com 2",
    floor: "1",
    location_description: "Next to LT19",
    latitude: 1.2938347,
    longitude: 103.7744572,
    opening_time: "00:00",
    closing_time: "23:59",
    image_url: "https://raw.githubusercontent.com/CS3219-AY2627S1/FoC-Template/main/data/images/PRINTER_COM2.jpeg",
  }),
  seed(4, {
    name: "Cool Spot",
    type: "Food",
    building: "Com2",
    floor: "1",
    location_description: "Opp LT16",
    latitude: 1.2940156,
    longitude: 103.7738478,
    opening_time: "09:00",
    closing_time: "21:30",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWnbKftSoeMWP3T7GaFshaZ0NLOuRsE33l_MwuKA_0jjg9mDfPMREaQGXpFsMZVbW7hTadp4WgrKMDZ0q-DD7TPonC89NaAFOzrAlGVpRyrD-gOywx3dL2ahDFfTGwlLC1A8uIMVtzBjw_6E=s1360-w1360-h1020-rw",
  }),
  seed(5, {
    name: "InstaChef",
    type: "Food",
    building: "Terrace",
    floor: "1",
    location_description: "Next to foyer",
    latitude: 1.2938898,
    longitude: 103.7736305,
    opening_time: "00:00",
    closing_time: "23:59",
    image_url: "https://raw.githubusercontent.com/CS3219-AY2627S1/FoC-Template/main/data/images/INSTACHEF.jpeg",
  }),
  seed(6, {
    name: "Cafe+ Robot Cafe",
    type: "Food/Coffee",
    building: "Central Library",
    floor: "1",
    location_description: "Opp to central library entrance",
    latitude: 1.296444,
    longitude: 103.773032,
    opening_time: "00:00",
    closing_time: "23:59",
    image_url: "https://raw.githubusercontent.com/CS3219-AY2627S1/FoC-Template/main/data/images/ROBOT_CAFE.jpeg",
  }),
  seed(7, {
    name: "A Hot Hideout",
    type: "Food",
    building: "Prince George's Park",
    floor: "2",
    location_description: "Near PGP entrance",
    latitude: 1.2908445,
    longitude: 103.7770891,
    opening_time: "11:00",
    closing_time: "21:30",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWkrNItZxIKyImhH6dKRBAMHPyhjFCMKQWX-NO_nCoDNmsBSn2MhrKYOUcTZu0VdSOmcgq0iKAikd3eVwW_yhkNICQdrVUbhe1TC2oMLWymLInaRe3kryqoilpT_zlxW3rCtGsD0=s1360-w1360-h1020-rw",
  }),
  seed(8, {
    name: "Arise and Shine",
    type: "Food",
    building: "Engineering Block E4",
    floor: "4",
    location_description: "Near LT6",
    latitude: 1.2991517,
    longitude: 103.769064,
    opening_time: "08:00",
    closing_time: "18:00",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWlMQJFtn1-R6faY8YON6TXELpF_1ix9IRl2kpFQdaZQ8gLVz-BZoJP_ldTNuLwneALFlP4CAUw3QNZDDexgMC1dZYsPBHlt9CNjk0kQzJG8zTFNh4r4oZYoUxSkShsAtDgkAv2E=s1360-w1360-h1020-rw",
  }),
  seed(9, {
    name: "Bakehaus / Aurea",
    type: "Food",
    building: "The Ridge",
    floor: "1",
    location_description: "Near COM2",
    latitude: 1.2946778,
    longitude: 103.7707872,
    opening_time: "08:00",
    closing_time: "21:00",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWnSPhOWOjsovwu98TZpWjiNt6Rb79YXhZtWhw-TMRPT3XP8uItykHUQbU3ppAJZTys49yqlRbKzfMlIBByyzBSXyiXnfkb-BdWUSTYSyJEYVDMJhMtdUZbUET4xYNjWew1l4VgPYfWg2N0=w408-h356-k-no",
  }),
  seed(10, {
    name: "Central Square @ YIH",
    type: "Food",
    building: "Yusof Ishak House",
    floor: "1",
    location_description: "Closest to Opp UHC bus stop",
    latitude: 1.2984401,
    longitude: 103.7726256,
    opening_time: "08:00",
    closing_time: "20:00",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWnUshwdsGvjqWDr5IcLlUYijZ3CALJYoQnzqszWPQfqBxbPHCqAbFmnTlb1Hwk0JiJZxPWfCoiGFJymHKpspsExW0nKjlkLC54nk5F_bGvnYikXoyvfV67B6BdVy8P7SUdh-X9jTmGlMpGY=w408-h306-k-no",
  }),
  seed(11, {
    name: "Pasta Express",
    type: "Food",
    building: "Frontier",
    floor: "1",
    location_description: "Aircon section",
    latitude: 1.2947819,
    longitude: 103.7704435,
    opening_time: "09:30",
    closing_time: "19:30",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWmUD0So0a9tnpaQvZ14OeUzkiSDLX4MJiqAs1hm6t5CzjByx9gUT0GktsahO8CHZSOfNpb7SA3zKSl9yU0r5Ca_BFE8XEmt4JvWYadw6iTg6nkcFkXN6G-TctMa2yQtCguG9h5e=w408-h272-k-no",
  }),
  seed(12, {
    name: "TOMORO COFFEE",
    type: "Food/Coffee",
    building: "Hon Sui Sen Memorial Library",
    floor: "2",
    location_description: "Inside HSSML",
    latitude: 1.2931259,
    longitude: 103.7719943,
    opening_time: "08:15",
    closing_time: "18:00",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWkQQottowVVI2NVVhDZfDAEmIbA8ZqyIs-jx1C4deCKActV3lwvOPDmRTZ902JFteB3CM4gv0W8gQsmM5fowQ_oVdqrnM5ziKqwxG4yvFJfz36_q5jJ2vMbXPbX2YKjap55Ab2c=w408-h306-k-no",
  }),
  seed(13, {
    name: "Octobox",
    type: "Shopping",
    building: "Prince George’s Park",
    floor: "2",
    location_description: "Near PGP entrance",
    latitude: 1.2904347,
    longitude: 103.7787588,
    opening_time: "00:00",
    closing_time: "23:59",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWlA4Toa4tSE4KK1c-JORamlknrxZXraXrD2dAdfcMgmHPk1HyqWvVM9NOVTFpR3QM3qXlcOsQdDtrcdl9RzpFomNJAkazPtjmEJVEntPex_0ltOVaIatbucplmKax2s49281GPpnk7Btzk=w408-h306-k-no",
  }),
  seed(14, {
    name: "Smooy",
    type: "Food",
    building: "COM3",
    floor: "1",
    location_description: "The Terrace @ COM3",
    latitude: 1.2948308,
    longitude: 103.7716305,
    opening_time: "11:00",
    closing_time: "21:00",
    image_url: "https://fkemumodynkaeojrrkbj.supabase.co/storage/v1/object/public/images/brands/e2d9c08f-e2f0-403c-8e78-dbe6167960e2/website/smooy/header-smooy-tarrina.png",
  }),
  seed(15, {
    name: "Goh Bros E-Print Pte Ltd",
    type: "Printing",
    building: "Yusof Ishak House",
    floor: "5",
    location_description: "Take the long staircase up YIH",
    latitude: 1.2984905,
    longitude: 103.7720544,
    opening_time: "09:00",
    closing_time: "18:00",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWm5NuiWtWZyjafM7uN_6Gu9iQZXaIrwmzxQMdizQUH1R2aQWL9SwOQDS4lyD_TtB_k5sBV4whaAtseI1fq74WgMRSUUnsTBtDmlL51bBtvLSF8FhHnGqgSUm9JM-GF-SgvFsZ1v=w408-h306-k-no",
  }),
  seed(16, {
    name: "Cheers Unmanned Convenience Store",
    type: "Shopping",
    building: "Engineering Block E3",
    floor: "4",
    location_description: "Take right from Arise n Shine",
    latitude: 1.2994341,
    longitude: 103.7526298,
    opening_time: "00:00",
    closing_time: "23:59",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWnH5mpLERSb_Yf7gsqMglXEDHGdVu8zE1tV2GdKNMT1KcUF7u6imG9Vjy-tepRUSFVsaEQeQgQFUKsxU_acX_kFwJ9OUri1rhzVDPhg31i1z33p3d7vaDJStF595Ou2ATOwJw5N=w408-h544-k-no",
  }),
  seed(17, {
    name: "Nami",
    type: "Food",
    building: "innovation4.0",
    floor: "1",
    location_description: "Opp TCOMS",
    latitude: 1.2942982,
    longitude: 103.7708813,
    opening_time: "08:00",
    closing_time: "17:30",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWntNY4TO3Lb87QdhY0myxuoeYmI8-N9N3ExA2IM1pazJTfHH0-vbcM3jLv5lwG_8D2Mlxf4yuC8M-c8Pr3EvS9Gc4ZWEyJbTBR_I_ZrlqRBTMoKwzIv4N2uvmR_3WcVbGwg7aPZS1VY3fdZ=s1360-w1360-h1020-rw",
  }),
  seed(18, {
    name: "Supersnacks",
    type: "Food",
    building: "Prince George’s Park",
    floor: "1",
    location_description: "At level 1 in Prince George's Park Residences, Block 10",
    latitude: 1.2913847,
    longitude: 103.7776367,
    opening_time: "11:00",
    closing_time: "02:00",
    image_url: "https://uci.nus.edu.sg/wp-content/uploads/2024/02/Supersnacks-Edited-1024x684-1-898x600.jpg",
  }),
  seed(19, {
    name: "Good Day Cafe",
    type: "Food/Coffee",
    building: "Medicine+Science Library",
    floor: "1",
    location_description: "Inside MedScience library",
    latitude: 1.2967989,
    longitude: 103.7794336,
    opening_time: "07:30",
    closing_time: "18:30",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWnmMPF_PW_yskgKVzZQzhxvMYOjqUvIkGTKAnt9M083VExnE23zxA_hF1fsFOhSYoanfZpxGhkI-wJ218hawyZXaZRLgKRxe0IsiCGUbbp6w5yqyVuJxzNU4NzUaa_Um3hYgFiI=w408-h306-k-no",
  }),
  seed(20, {
    name: "The Coffee Roaster",
    type: "Food/Coffee",
    building: "Blk AS8",
    floor: "1",
    location_description: "Behind central library bus stop",
    latitude: 1.296252229,
    longitude: 103.7720926,
    opening_time: "08:00",
    closing_time: "17:30",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWk65JqsLWtUmpRJwU3shE351_3yPnxVqBnd6i6NMcgZZsQEHesr1AWQC8JbtXPjNlNZRR2L-3I1vl3ik4niQQecuYeUeFidEP5FKOosrJCXmgdtRPl30XtQZB0N5RNNwpSQJnv5=w408-h271-k-no",
  }),
  seed(21, {
    name: "he by He Brews",
    type: "Food/Coffee",
    building: "Engineering Block EA",
    floor: "1",
    location_description: "Near LT7 & Engineering Auditorium",
    latitude: 1.300566804,
    longitude: 103.7707577,
    opening_time: "08:00",
    closing_time: "17:00",
    image_url: "https://lh3.googleusercontent.com/gps-cs-s/AHRPTWneOXivy1ZqDtOcywaoKrBqW59o4w179360AN9ad0a7jOp_wwaHluRjAhORj-9UQKkmFyutYwLAFAQUkJBvEPLnDmw-SwZ657drKFKDb4kdtxQYPNzbBnyNI33bk02xB1UT82vvxNw7Q_qt=w408-h306-k-no",
  }),
];

/** A fresh fixture per call, so each App instance starts from the seed. */
export function createSuppliersFixture(): SuppliersApi {
  let suppliers = SEED.map((s) => ({ ...s }));
  let created = 0;

  function find(id: string): Supplier {
    const found = suppliers.find((s) => s.id === id);
    if (!found) throw new SupplierApiError(NOT_FOUND, 404);
    return found;
  }

  return {
    async listSuppliers({ category, search }: SupplierFilter = {}) {
      // `category` matches the type case-insensitively (ILIKE with no
      // wildcards); `search` is a case-insensitive substring of the name.
      const needle = search?.toLowerCase();
      const matches = suppliers
        .filter((s) => !category || s.type.toLowerCase() === category.toLowerCase())
        .filter((s) => !needle || s.name.toLowerCase().includes(needle))
        .sort((a, b) => a.name.localeCompare(b.name));
      return mockDelay(matches.map((s) => ({ ...s })));
    },

    async getSupplier(id: string) {
      return mockDelay({ ...find(id) });
    },

    async createSupplier(input: SupplierInput) {
      created += 1;
      const now = new Date().toISOString();
      const supplier: Supplier = {
        ...input,
        id: `00000000-0000-4000-9000-${String(created).padStart(12, "0")}`,
        created_at: now,
        updated_at: now,
      };
      suppliers = [...suppliers, supplier];
      return mockDelay({ ...supplier });
    },

    async updateSupplier(id: string, input: SupplierInput) {
      const existing = find(id);
      const updated: Supplier = {
        ...input,
        id,
        created_at: existing.created_at,
        updated_at: new Date().toISOString(),
      };
      suppliers = suppliers.map((s) => (s.id === id ? updated : s));
      return mockDelay({ ...updated });
    },

    async deleteSupplier(id: string) {
      find(id);
      suppliers = suppliers.filter((s) => s.id !== id);
      await mockDelay(undefined);
    },
  };
}
