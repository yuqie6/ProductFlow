import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface AgentAssistantMarkdownProps {
  text: string;
  streaming?: boolean;
}

/** 流式中用纯文本避免每 token 全量 GFM；结束后再解析 Markdown。 */
export function AgentAssistantMarkdown({ text, streaming = false }: AgentAssistantMarkdownProps) {
  if (!text) {
    return null;
  }

  if (streaming) {
    return (
      <div className="agent-markdown whitespace-pre-wrap break-words" data-streaming="true">
        {text}
      </div>
    );
  }

  return (
    <div className="agent-markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
    </div>
  );
}
