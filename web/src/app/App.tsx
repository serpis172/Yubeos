import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { Placeholder } from "../pages/Placeholder";
import { Shell } from "../shell/Shell";
import { allNavItems } from "../shell/nav";

const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: 5_000, refetchOnWindowFocus: false } } });

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Shell />}>
            {allNavItems.map((i) => <Route key={i.id} path={i.to} element={<Placeholder />} />)}
            <Route path="*" element={<Placeholder />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
