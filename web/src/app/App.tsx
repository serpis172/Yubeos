import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { RequireAuth } from "../auth/RequireAuth";
import { SetupPage } from "../auth/SetupPage";
import { DashboardPage } from "../pages/DashboardPage";
import { AppearancePage } from "../pages/AppearancePage";
import { PrefsProvider } from "../prefs/PrefsProvider";
import { Placeholder } from "../pages/Placeholder";
import { SecurityPage } from "../pages/SecurityPage";
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
              <Route element={<PrefsProvider><Shell /></PrefsProvider>}>
                <Route path="/" element={<DashboardPage />} />
                <Route path="/settings/appearance" element={<AppearancePage />} />
                <Route path="/settings/security" element={<SecurityPage />} />
                {allNavItems.filter((i) => !["appearance", "security", "dashboard"].includes(i.id)).map((i) => <Route key={i.id} path={i.to} element={<Placeholder />} />)}
                <Route path="*" element={<Placeholder />} />
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
