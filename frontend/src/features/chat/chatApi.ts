import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
import { queryString, NetworkError } from "../../lib/http";
import type {
  Conversation,
  ChatMessage,
  MessagePage,
  UnreadSummaryResponse,
  WSTicketResponse,
} from "./chatTypes";

const PREFIX = "/api/chat";

export class ChatApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ChatApiError";
    this.status = status;
  }
}

export interface ChatApi {
  fetchWSTicket: () => Promise<string>;
  fetchConversationsByErrand: (errandId: string) => Promise<Conversation[]>;
  fetchConversation: (conversationId: string) => Promise<Conversation>;
  fetchMessages: (
    conversationId: string,
    cursor?: string,
    since?: string,
    limit?: number,
  ) => Promise<MessagePage>;
  sendTextMessage: (
    conversationId: string,
    text: string,
    clientMessageId: string,
  ) => Promise<ChatMessage>;
  sendImageMessage: (
    conversationId: string,
    file: File,
    clientMessageId: string,
  ) => Promise<ChatMessage>;
  markConversationRead: (conversationId: string) => Promise<void>;
  fetchUnreadSummary: () => Promise<UnreadSummaryResponse>;
}

export function createChatApi(
  send: AuthorizedSend,
  getAccessToken: () => string | null,
): ChatApi {
  const baseUrl = config.gatewayBaseUrl;

  async function call<T>(
    path: string,
    method: "GET" | "POST" = "GET",
    body?: unknown,
  ): Promise<T> {
    const res = await send({
      baseUrl,
      path: `${PREFIX}${path}`,
      method,
      body,
    });

    if (!res.ok) {
      const msg =
        typeof res.body === "object" && res.body !== null && "error" in res.body
          ? String((res.body as { error: unknown }).error)
          : `Request failed with status ${res.status}`;
      throw new ChatApiError(msg, res.status);
    }

    return res.body as T;
  }

  return {
    async fetchWSTicket(): Promise<string> {
      const res = await call<WSTicketResponse>("/ws-ticket", "POST");
      return res.ticket;
    },

    async fetchConversationsByErrand(errandId: string): Promise<Conversation[]> {
      const query = queryString({ errandId });
      return call<Conversation[]>(`/conversations${query}`, "GET");
    },

    async fetchConversation(conversationId: string): Promise<Conversation> {
      return call<Conversation>(`/conversations/${conversationId}`, "GET");
    },

    async fetchMessages(
      conversationId: string,
      cursor?: string,
      since?: string,
      limit = 50,
    ): Promise<MessagePage> {
      const query = queryString({
        cursor,
        since,
        limit: limit > 0 ? String(limit) : undefined,
      });
      return call<MessagePage>(`/conversations/${conversationId}/messages${query}`, "GET");
    },

    async sendTextMessage(
      conversationId: string,
      text: string,
      clientMessageId: string,
    ): Promise<ChatMessage> {
      return call<ChatMessage>(`/conversations/${conversationId}/messages`, "POST", {
        client_message_id: clientMessageId,
        text,
      });
    },

    async sendImageMessage(
      conversationId: string,
      file: File,
      clientMessageId: string,
    ): Promise<ChatMessage> {
      const token = getAccessToken();
      const formData = new FormData();
      formData.append("client_message_id", clientMessageId);
      formData.append("image", file);

      const headers: Record<string, string> = {};
      if (token) {
        headers["Authorization"] = `Bearer ${token}`;
      }

      let response: Response;
      try {
        response = await fetch(`${baseUrl}${PREFIX}/conversations/${conversationId}/messages`, {
          method: "POST",
          headers,
          body: formData,
        });
      } catch {
        throw new NetworkError(baseUrl);
      }

      const parsed: unknown = await response.json().catch(() => null);
      if (!response.ok) {
        const msg =
          typeof parsed === "object" && parsed !== null && "error" in parsed
            ? String((parsed as { error: unknown }).error)
            : `Image upload failed with status ${response.status}`;
        throw new ChatApiError(msg, response.status);
      }

      return parsed as ChatMessage;
    },

    async markConversationRead(conversationId: string): Promise<void> {
      await call(`/conversations/${conversationId}/read`, "POST");
    },

    async fetchUnreadSummary(): Promise<UnreadSummaryResponse> {
      return call<UnreadSummaryResponse>("/unread-summary", "GET");
    },
  };
}
