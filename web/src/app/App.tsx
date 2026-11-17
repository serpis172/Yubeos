import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { RequireAuth } from "../auth/RequireAuth";
import { SetupPage } from "../auth/SetupPage";
import { Placeholder } from "../pages/Placeholder";
import { Shell } from "../shell/Shell";
import { allNavItems } from "../shell/nav";
import { ErrorBoundary } from "./ErrorBoundary";

const newClient = () => new QueryClient({ defaultOptions: { queries: { staleTime: 5_000, refetchOnWindowFocus: false } } });

export function App() {
  const [queryClient] = useState(newClient); // one cache per app instance (isolated in tests, discarded on sign-out)
  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <Routes>
            <Route path="/setup" element={<SetupPage />} />
            <Route element={<RequireAuth />}>
              <Route element={<Shell />}>
                {allNavItems.map((i) => <Route key={i.id} path={i.to} element={<Placeholder />} />)}
                <Route path="*" element={<Placeholder />} />
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
