import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface AgentAssistantMarkdownProps {
  text: string;
  streaming?: boolean;
}

/** Keep assistant output readable without turning model text into HTML. */
export function AgentAssistantMarkdown({ text, streaming = false }: AgentAssistantMarkdownProps) {
  if (!text) {
    return null;
  }

  return (
    <div className="agent-markdown" data-streaming={streaming || undefined}>
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
    </div>
  );
}
