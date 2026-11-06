export type MessageType = "TEXT" | "IMAGE";

export type ConversationStatus = "ACTIVE" | "FROZEN";

export interface Conversation {
  id: string;
  errand_id: string;
  requester_id: string;
  courier_id: string;
  status: ConversationStatus;
  errand_completed_at?: string;
  requester_last_read_at: string;
  courier_last_read_at: string;
  last_event_occurred_at: string;
  created_at: string;
  updated_at: string;
  is_read_only?: boolean;
}

export interface ChatMessage {
  id: string;
  conversation_id: string;
  sender_id: string;
  message_type: MessageType;
  text_content?: string;
  media_id?: string;
  media_url?: string;
  client_message_id?: string;
  created_at: string;
  // Local optimistic state
  status?: "pending" | "sent" | "failed";
}

export interface MessagePage {
  messages: ChatMessage[];
  next_cursor?: string;
  has_more: boolean;
}

export interface UnreadSummaryResponse {
  has_unread: boolean;
  unread_errand_ids: string[];
  requester_has_unread?: boolean;
  courier_has_unread?: boolean;
  requester_unread_errand_ids?: string[];
  courier_unread_errand_ids?: string[];
}

export interface WSTicketResponse {
  ticket: string;
}

export type RealtimeEvent =
  | {
      type: "message.created";
      data: ChatMessage;
    }
  | {
      type: "conversation.read_only";
      data: {
        conversation_id: string;
        errand_id: string;
      };
    }
  | {
      type: "conversation.read";
      data: {
        conversation_id: string;
        reader_id: string;
        read_at: string;
      };
    };
