import { allNavItems, navGroups } from "./nav";

describe("navigation model", () => {
  it("contains every section from the design sketches", () => {
    const labels = allNavItems.map((i) => i.label);
    for (const l of ["Dashboard", "File Explorer", "Disk Manager", "Terminal", "Add-ons", "Repo-Papers", "Appearance"]) {
      expect(labels).toContain(l);
    }
  });

  it("has unique ids and unique routes", () => {
    expect(new Set(allNavItems.map((i) => i.id)).size).toBe(allNavItems.length);
    expect(new Set(allNavItems.map((i) => i.to)).size).toBe(allNavItems.length);
  });

  it("keeps the sketch grouping: core · ecosystem · settings", () => {
    expect(navGroups.map((g) => g.id)).toEqual(["core", "ecosystem", "settings"]);
  });
});
