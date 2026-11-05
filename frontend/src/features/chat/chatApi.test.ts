import { describe, expect, it, vi } from "vitest";
import { createChatApi } from "./chatApi";
import type { HttpResponse } from "../../lib/http";

describe("chatApi", () => {
  it("fetchWSTicket calls POST /api/chat/ws-ticket and returns ticket", async () => {
    const mockSend = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { ticket: "ticket-123" },
    } as HttpResponse);

    const api = createChatApi(mockSend, () => "test-token");
    const ticket = await api.fetchWSTicket();

    expect(ticket).toBe("ticket-123");
    expect(mockSend).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "/api/chat/ws-ticket",
        method: "POST",
      }),
    );
  });

  it("fetchConversationsByErrand queries conversations by errandId", async () => {
    const mockSend = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: [{ id: "conv-1", errand_id: "err-1" }],
    } as HttpResponse);

    const api = createChatApi(mockSend, () => "test-token");
    const convs = await api.fetchConversationsByErrand("err-1");

    expect(convs).toHaveLength(1);
    expect(mockSend).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "/api/chat/conversations?errandId=err-1",
        method: "GET",
      }),
    );
  });

  it("sendTextMessage posts message payload", async () => {
    const mockSend = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { id: "msg-1", text_content: "hello" },
    } as HttpResponse);

    const api = createChatApi(mockSend, () => "test-token");
    const msg = await api.sendTextMessage("conv-1", "hello", "client-1");

    expect(msg.id).toBe("msg-1");
    expect(mockSend).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "/api/chat/conversations/conv-1/messages",
        method: "POST",
        body: {
          client_message_id: "client-1",
          text: "hello",
        },
      }),
    );
  });

  it("fetchUnreadSummary returns unread status and errands", async () => {
    const mockSend = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      body: { has_unread: true, unread_errand_ids: ["err-1", "err-2"] },
    } as HttpResponse);

    const api = createChatApi(mockSend, () => "test-token");
    const summary = await api.fetchUnreadSummary();

    expect(summary.has_unread).toBe(true);
    expect(summary.unread_errand_ids).toEqual(["err-1", "err-2"]);
  });
});
