import { lazy, Suspense, useEffect, useMemo, useRef } from "react";
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";

import { AppToaster } from "./components/ui/toast";
import { TooltipProvider } from "./components/ui/tooltip";
import { api } from "./lib/api";
import { activeMerchantId, applyMerchantSwitchBoundary } from "./lib/merchantBoundary";
import { PreferencesProvider, useI18n } from "./lib/preferences";
import { disposeAllConversationRuntimes } from "./pages/workbench/agent/conversation/runtime";

const MediaLibraryPage = lazy(() =>
  import("./pages/MediaLibraryPage").then((module) => ({ default: module.MediaLibraryPage })),
);
const HomePage = lazy(() =>
  import("./pages/HomePage").then((module) => ({ default: module.HomePage })),
);
const HelpPage = lazy(() =>
  import("./pages/HelpPage").then((module) => ({ default: module.HelpPage })),
);
const LoginPage = lazy(() =>
  import("./pages/LoginPage").then((module) => ({ default: module.LoginPage })),
);
const loadImageChatPage = () =>
  import("./pages/ImageChatPage").then((module) => ({ default: module.ImageChatPage }));
const ImageChatPage = lazy(loadImageChatPage);
const AgentProductCreatePage = lazy(() =>
  import("./pages/AgentProductCreatePage").then((module) => ({ default: module.AgentProductCreatePage })),
);
const ProductWorkbenchPage = lazy(() =>
  import("./pages/workbench/ProductWorkbenchPage").then((module) => ({ default: module.ProductWorkbenchPage })),
);
const GlobalAgentDock = lazy(() =>
  import("./components/GlobalAgentDock").then((module) => ({ default: module.GlobalAgentDock })),
);
const loadProductListPage = () =>
  import("./pages/ProductListPage").then((module) => ({ default: module.ProductListPage }));
const ProductListPage = lazy(loadProductListPage);
const SettingsPage = lazy(() =>
  import("./pages/SettingsPage").then((module) => ({ default: module.SettingsPage })),
);

function LoadingScreen() {
  const { t } = useI18n();

  return (
    <div className="flex min-h-screen items-center justify-center bg-surface-base text-text-muted">
      <Loader2 size={24} className="animate-spin" />
      <span className="sr-only">{t("app.loading")}</span>
    </div>
  );
}

function AppRoutes() {
  const queryClient = useQueryClient();
  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: api.getSessionState,
    retry: false,
  });

  const authenticated = Boolean(sessionQuery.data?.authenticated);
  const merchantId = activeMerchantId(sessionQuery.data);
  const previousMerchantRef = useRef<string | null>(null);

  useEffect(() => {
    const previous = previousMerchantRef.current;
    if (previous !== null && previous !== merchantId) {
      applyMerchantSwitchBoundary(queryClient, {
        onInvalidateSubscriptions: disposeAllConversationRuntimes,
      });
    }
    previousMerchantRef.current = merchantId;
  }, [merchantId, queryClient]);

  useEffect(() => {
    if (!authenticated) {
      return;
    }
    void loadProductListPage();
    void loadImageChatPage();
  }, [authenticated]);

  if (sessionQuery.isLoading) {
    return <LoadingScreen />;
  }

  return (
    <>
      <Suspense fallback={<LoadingScreen />}>
        <Routes>
          <Route path="/login" element={<LoginPage authenticated={authenticated} />} />
          <Route
            path="/home"
            element={authenticated ? <HomePage /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/products"
            element={authenticated ? <ProductListPage /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/products/new/agent"
            element={authenticated ? <Navigate to="/products/new" replace /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/products/new"
            element={authenticated ? <AgentProductCreatePage /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/image-chat"
            element={authenticated ? <ImageChatPage /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/media-library"
            element={authenticated ? <MediaLibraryPage /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/help"
            element={authenticated ? <HelpPage /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/settings"
            element={authenticated ? <SettingsPage /> : <Navigate to="/login" replace />}
          />
          <Route
            path="/products/:productId"
            element={authenticated ? <ProductWorkbenchPage /> : <Navigate to="/login" replace />}
          />
          <Route path="*" element={<Navigate to={authenticated ? "/home" : "/login"} replace />} />
        </Routes>
      </Suspense>
      {authenticated ? (
        <Suspense fallback={null}>
          <GlobalAgentDock />
        </Suspense>
      ) : null}
    </>
  );
}

export function App() {
  const queryClient = useMemo(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            refetchOnWindowFocus: false,
          },
        },
      }),
    [],
  );

  return (
    <QueryClientProvider client={queryClient}>
      <PreferencesProvider>
        <TooltipProvider>
          <BrowserRouter>
            <div className="min-h-screen bg-surface-base font-sans text-text-primary selection:bg-accent/20">
              <AppRoutes />
              <AppToaster />
            </div>
          </BrowserRouter>
        </TooltipProvider>
      </PreferencesProvider>
    </QueryClientProvider>
  );
}
