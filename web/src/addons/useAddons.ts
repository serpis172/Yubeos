import { useQuery } from "@tanstack/react-query";
import { getAddons } from "../lib/api";

export const ADDONS_KEY = ["addons"] as const;

/** Installed add-ons, shared by the Add-ons page and the sidebar. Refreshed every 30 s. */
export function useAddons() {
  return useQuery({
    queryKey: ADDONS_KEY,
    queryFn: ({ signal }) => getAddons(signal),
    refetchInterval: 30_000,
    retry: false,
  });
}
