import { useQuery } from "@tanstack/react-query";
import { getMe, setCsrfToken } from "../lib/api";

export const ME_KEY = ["me"] as const;

/** The signed-in user. A 401 error means "show the login page"; the CSRF token is captured here. */
export function useMe() {
  return useQuery({
    queryKey: ME_KEY,
    queryFn: async ({ signal }) => {
      const me = await getMe(signal);
      setCsrfToken(me.csrf);
      return me;
    },
    retry: false,
    staleTime: 30_000,
  });
}
