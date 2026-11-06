import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { AppShell } from "./AppShell";
import * as chatContext from "../features/chat/ChatSocketContext";
import type { Session } from "../features/auth/types";

const mockSession: Session = {
  userId: "user-1",
  username: "alice",
  role: "STUDENT",
  initials: "AL",
};

describe("AppShell unread notification badges", () => {
  it("renders unread badge on Requesting toggle button when requesterHasUnread is true", () => {
    vi.spyOn(chatContext, "useChatSocket").mockReturnValue({
      hasUnread: true,
      unreadErrandIds: new Set(["err-1"]),
      requesterHasUnread: true,
      courierHasUnread: false,
      requesterUnreadErrandIds: new Set(["err-1"]),
      courierUnreadErrandIds: new Set(),
      markErrandRead: vi.fn(),
      subscribeToEvents: vi.fn(),
      chatApi: {} as any,
    });

    const html = renderToStaticMarkup(
      <AppShell
        session={mockSession}
        view="my-errands"
        onNavigate={vi.fn()}
        mode="delivering"
        onModeChange={vi.fn()}
        available={100}
        held={0}
        onLogOut={vi.fn()}
      >
        <div>Content</div>
      </AppShell>,
    );

    // In delivering mode, toggle Requesting shows unread dot
    expect(html).toContain('<span>Requesting</span><span class="unread-dot');
    // In delivering mode, toggle Delivering does not show unread dot
    expect(html).not.toContain('<span>Delivering</span><span class="unread-dot');
    // Sidebar link for my-errands does NOT have unread badge because courierHasUnread is false
    expect(html).not.toMatch(/<span>My errands<\/span><span class="unread-dot/);
  });

  it("renders unread badge on Delivering toggle button and sidebar when in delivering mode with courierHasUnread", () => {
    vi.spyOn(chatContext, "useChatSocket").mockReturnValue({
      hasUnread: true,
      unreadErrandIds: new Set(["err-2"]),
      requesterHasUnread: false,
      courierHasUnread: true,
      requesterUnreadErrandIds: new Set(),
      courierUnreadErrandIds: new Set(["err-2"]),
      markErrandRead: vi.fn(),
      subscribeToEvents: vi.fn(),
      chatApi: {} as any,
    });

    const html = renderToStaticMarkup(
      <AppShell
        session={mockSession}
        view="my-errands"
        onNavigate={vi.fn()}
        mode="delivering"
        onModeChange={vi.fn()}
        available={100}
        held={0}
        onLogOut={vi.fn()}
      >
        <div>Content</div>
      </AppShell>,
    );

    expect(html).not.toContain('<span>Requesting</span><span class="unread-dot');
    expect(html).toContain('<span>Delivering</span><span class="unread-dot');
    // Sidebar link for my-errands SHOULD have unread badge in delivering mode
    expect(html).toMatch(/<span>My errands<\/span><span class="unread-dot/);
  });

  it("renders unread badge on sidebar my-errands when in requesting mode with requesterHasUnread", () => {
    vi.spyOn(chatContext, "useChatSocket").mockReturnValue({
      hasUnread: true,
      unreadErrandIds: new Set(["err-1"]),
      requesterHasUnread: true,
      courierHasUnread: false,
      requesterUnreadErrandIds: new Set(["err-1"]),
      courierUnreadErrandIds: new Set(),
      markErrandRead: vi.fn(),
      subscribeToEvents: vi.fn(),
      chatApi: {} as any,
    });

    const html = renderToStaticMarkup(
      <AppShell
        session={mockSession}
        view="my-errands"
        onNavigate={vi.fn()}
        mode="requesting"
        onModeChange={vi.fn()}
        available={100}
        held={0}
        onLogOut={vi.fn()}
      >
        <div>Content</div>
      </AppShell>,
    );

    expect(html).toContain('<span>Requesting</span><span class="unread-dot');
    expect(html).toMatch(/<span>My errands<\/span><span class="unread-dot/);
  });
});
