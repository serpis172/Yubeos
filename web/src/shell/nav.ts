/** Navigation model derived from the hand-drawn sidebar sketch (docs/UI-DESIGN.md §1). */
export interface NavItem {
  id: string;
  label: string;
  to: string;
  /** Roadmap phase in which the page becomes real; shown on placeholders. */
  phase: number;
  icon: IconName;
}

export type IconName =
  | "dashboard" | "folder" | "disk" | "terminal" | "puzzle" | "book" | "palette" | "shield" | "info";

export interface NavGroup {
  id: string;
  items: NavItem[];
}

export const navGroups: NavGroup[] = [
  {
    id: "core",
    items: [
      { id: "dashboard", label: "Dashboard", to: "/", phase: 2, icon: "dashboard" },
      { id: "files", label: "File Explorer", to: "/files", phase: 4, icon: "folder" },
      { id: "disks", label: "Disk Manager", to: "/disks", phase: 5, icon: "disk" },
      { id: "terminal", label: "Terminal", to: "/terminal", phase: 3, icon: "terminal" },
    ],
  },
  {
    id: "ecosystem",
    items: [
      { id: "addons", label: "Add-ons", to: "/addons", phase: 7, icon: "puzzle" },
      { id: "repo-papers", label: "Repo-Papers", to: "/repo-papers", phase: 8, icon: "book" },
    ],
  },
  // Installed add-ons are injected here at runtime from /api/v1/addons (Phase 7).
  {
    id: "settings",
    items: [
      { id: "appearance", label: "Appearance", to: "/settings/appearance", phase: 1, icon: "palette" },
      { id: "security", label: "Security", to: "/settings/security", phase: 1, icon: "shield" },
      { id: "system", label: "System", to: "/settings/system", phase: 1, icon: "info" },
    ],
  },
];

export const allNavItems: NavItem[] = navGroups.flatMap((g) => g.items);
