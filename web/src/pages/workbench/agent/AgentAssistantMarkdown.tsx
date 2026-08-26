import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface AgentAssistantMarkdownProps {
  text: string;
  streaming?: boolean;
}

/** 保持助手输出可读，不把模型文本转成 HTML。 */
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
