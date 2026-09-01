import type { ReactNode } from "react";

import { GraphAgentPanel, GraphWorkbenchPage } from "./GraphWorkbenchPage";
import type { CanonicalProductDetail, GraphProjection } from "../../lib/types";

export function GraphWorkbenchSurface({
  product,
  initialGraph,
  agentError,
  onRetryAgent,
  onOpenConversation,
}: {
  product: CanonicalProductDetail;
  initialGraph: GraphProjection;
  agentError: unknown;
  onRetryAgent: () => void;
  onOpenConversation: () => void;
}) {
  const agentContent: ReactNode = (
    <GraphAgentPanel
      error={agentError}
      onRetry={onRetryAgent}
      onOpenConversation={onOpenConversation}
    />
  );
  return (
    <GraphWorkbenchPage
      product={product}
      initialGraph={initialGraph}
      agentContent={agentContent}
    />
  );
}
