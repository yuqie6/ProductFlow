import { lazy, Suspense, useEffect, useMemo, useRef } from "react";
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { BrowserRouter, Navigate, Route, Routes, useLocation } from "react-router-dom";

import { AppToaster } from "./components/ui/toast";
import { TooltipProvider } from "./components/ui/tooltip";
import { api } from "./lib/api";
import { canAccessOpsSettings } from "./lib/opsAccess";
import { accountIdentity, applyAccountSwitchBoundary, ownMerchantId, sameAccountIdentity } from "./lib/accountBoundary";
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
const OpsPage = lazy(() => import("./pages/OpsPage").then((module) => ({ default: module.OpsPage })));
const OpsMerchantPage = lazy(() => import("./pages/OpsMerchantPage").then((module) => ({ default: module.OpsMerchantPage })));
const OpsProductPage = lazy(() => import("./pages/OpsProductPage").then((module) => ({ default: module.OpsProductPage })));
const AccountPage = lazy(() => import("./pages/AccountPage").then((module) => ({ default: module.AccountPage })));
const PasswordRecoveryPage = lazy(() => import("./pages/PasswordRecoveryPage").then((module) => ({ default: module.PasswordRecoveryPage })));
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
  const { pathname } = useLocation();
  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: api.getSessionState,
    retry: false,
  });

  const authenticated = Boolean(sessionQuery.data?.authenticated);
  const showMerchantDock = authenticated && Boolean(ownMerchantId(sessionQuery.data)) && pathname !== "/ops" && !pathname.startsWith("/ops/");
  const canOpenSettings = canAccessOpsSettings(sessionQuery.data);
  const identity = useMemo(() => accountIdentity(sessionQuery.data), [sessionQuery.data]);
  const previousIdentityRef = useRef<typeof identity>(null);

  useEffect(() => {
    const previous = previousIdentityRef.current;
    if (previous !== null && !sameAccountIdentity(previous, identity)) {
      applyAccountSwitchBoundary(queryClient, {
        onInvalidateSubscriptions: disposeAllConversationRuntimes,
      });
    }
    previousIdentityRef.current = identity;
  }, [identity, queryClient]);

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
          <Route path="/password-recovery" element={<PasswordRecoveryPage />} />
          <Route path="/ops" element={canOpenSettings ? <OpsPage /> : <Navigate to={authenticated ? "/account" : "/login"} replace />} />
          <Route path="/ops/merchants/:merchantId" element={canOpenSettings ? <OpsMerchantPage /> : <Navigate to={authenticated ? "/account" : "/login"} replace />} />
          <Route path="/ops/merchants/:merchantId/products/:productId" element={canOpenSettings ? <OpsProductPage /> : <Navigate to={authenticated ? "/account" : "/login"} replace />} />
          <Route path="/account" element={authenticated ? <AccountPage key={identity?.userId} /> : <Navigate to="/login" replace />} />
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
            element={
              canOpenSettings ? <SettingsPage /> : <Navigate to={authenticated ? "/home" : "/login"} replace />
            }
          />
          <Route
            path="/products/:productId"
            element={authenticated ? <ProductWorkbenchPage /> : <Navigate to="/login" replace />}
          />
          <Route path="*" element={<Navigate to={authenticated ? "/home" : "/login"} replace />} />
        </Routes>
      </Suspense>
      {showMerchantDock ? (
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
