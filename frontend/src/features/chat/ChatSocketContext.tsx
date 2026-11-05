import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { config } from "../../lib/config";
import type { ChatApi } from "./chatApi";
import type { RealtimeEvent } from "./chatTypes";

interface ChatSocketContextValue {
  hasUnread: boolean;
  unreadErrandIds: Set<string>;
  markErrandRead: (errandId: string) => void;
  subscribeToEvents: (listener: (event: RealtimeEvent) => void) => () => void;
  chatApi: ChatApi;
}

const defaultContextValue: ChatSocketContextValue = {
  hasUnread: false,
  unreadErrandIds: new Set(),
  markErrandRead: () => {},
  subscribeToEvents: () => () => {},
  chatApi: {} as ChatApi,
};

const ChatSocketContext = createContext<ChatSocketContextValue | null>(null);

export interface ChatSocketProviderProps {
  chatApi: ChatApi;
  isAuthenticated: boolean;
  accessToken: string | null;
  children: ReactNode;
}

export function ChatSocketProvider({
  chatApi,
  isAuthenticated,
  accessToken,
  children,
}: ChatSocketProviderProps) {
  const [hasUnread, setHasUnread] = useState(false);
  const [unreadErrandIds, setUnreadErrandIds] = useState<Set<string>>(new Set());

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const listenersRef = useRef<Set<(event: RealtimeEvent) => void>>(new Set());

  // Synchronize unread state via REST
  const refreshUnreadSummary = useCallback(async () => {
    if (!isAuthenticated) return;
    try {
      const summary = await chatApi.fetchUnreadSummary();
      setHasUnread(summary.has_unread);
      setUnreadErrandIds(new Set(summary.unread_errand_ids));
    } catch {
      // Ignore background unread sync errors
    }
  }, [chatApi, isAuthenticated]);

  const markErrandRead = useCallback((errandId: string) => {
    setUnreadErrandIds((prev) => {
      if (!prev.has(errandId)) return prev;
      const next = new Set(prev);
      next.delete(errandId);
      if (next.size === 0) {
        setHasUnread(false);
      }
      return next;
    });
  }, []);

  const subscribeToEvents = useCallback(
    (listener: (event: RealtimeEvent) => void) => {
      listenersRef.current.add(listener);
      return () => {
        listenersRef.current.delete(listener);
      };
    },
    [],
  );

  // In-band token refresh when accessToken updates
  useEffect(() => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN && accessToken) {
      wsRef.current.send(
        JSON.stringify({
          type: "auth_refresh",
          token: accessToken,
        }),
      );
    }
  }, [accessToken]);

  // Connect WebSocket
  useEffect(() => {
    if (!isAuthenticated) {
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
      setHasUnread(false);
      setUnreadErrandIds(new Set());
      return;
    }

    // Initial unread fetch
    void refreshUnreadSummary();

    let isCancelled = false;

    async function connect() {
      try {
        const ticket = await chatApi.fetchWSTicket();
        if (isCancelled) return;

        const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
        const host = config.gatewayBaseUrl
          ? new URL(config.gatewayBaseUrl).host
          : window.location.host;
        const wsUrl = `${protocol}//${host}/api/chat/ws?ticket=${ticket}`;

        const ws = new WebSocket(wsUrl);
        wsRef.current = ws;

        ws.onopen = () => {
          // Sync state on open
          void refreshUnreadSummary();
        };

        ws.onmessage = (event) => {
          try {
            const raw = JSON.parse(event.data);
            const realtimeEvent = raw as RealtimeEvent;

            // Notify listeners
            listenersRef.current.forEach((listener) => {
              try {
                listener(realtimeEvent);
              } catch (e) {
                console.error("error in chat event listener:", e);
              }
            });

            // Update unread state on new message
            if (realtimeEvent.type === "message.created") {
              setHasUnread(true);
              void refreshUnreadSummary();
            }
          } catch {
            // Non-JSON frame
          }
        };

        ws.onclose = () => {
          wsRef.current = null;
          if (!isCancelled && isAuthenticated) {
            // Reconnect after 3s
            reconnectTimeoutRef.current = window.setTimeout(connect, 3000);
          }
        };

        ws.onerror = () => {
          ws.close();
        };
      } catch {
        if (!isCancelled && isAuthenticated) {
          reconnectTimeoutRef.current = window.setTimeout(connect, 5000);
        }
      }
    }

    void connect();

    return () => {
      isCancelled = true;
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [chatApi, isAuthenticated, refreshUnreadSummary]);

  return (
    <ChatSocketContext.Provider
      value={{
        hasUnread,
        unreadErrandIds,
        markErrandRead,
        subscribeToEvents,
        chatApi,
      }}
    >
      {children}
    </ChatSocketContext.Provider>
  );
}

export function useChatSocket(): ChatSocketContextValue {
  const ctx = useContext(ChatSocketContext);
  return ctx ?? defaultContextValue;
}
