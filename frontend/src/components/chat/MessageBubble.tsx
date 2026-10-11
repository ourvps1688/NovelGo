import { memo } from "react";
import Markdown from "@/components/Markdown";

interface Props {
  role: "user" | "assistant";
  content: string;
}

export default memo(function MessageBubble({ role, content }: Props) {
  const isUser = role === "user";

  return (
    <div className={`flex ${isUser ? "justify-end" : "justify-start"}`}>
      <div
        className={`max-w-[78%] rounded-2xl px-4 py-3 break-words text-sm leading-relaxed ${
          isUser
            ? "bg-bubble-user text-bubble-user-foreground rounded-br-md shadow-xs"
            : "bg-card border border-border/40 text-foreground rounded-bl-md shadow-xs"
        }`}
      >
        <Markdown
          content={content}
          className={isUser ? "markdown-user" : undefined}
        />
      </div>
    </div>
  );
});
