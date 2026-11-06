import { useEffect, useRef, useState, type FormEvent, type ChangeEvent } from "react";
import { config } from "../../lib/config";
import { useChatSocket } from "./ChatSocketContext";
import type { ChatMessage, Conversation } from "./chatTypes";

interface ChatBoxProps {
  errandId: string;
  currentUserId: string;
  isRequester?: boolean;
  onClose?: () => void;
}

export function ChatBox({
  errandId,
  currentUserId,
  isRequester: _isRequester,
  onClose: _onClose,
}: ChatBoxProps) {
  const { chatApi, subscribeToEvents, markErrandRead, setActiveConversationId, refreshUnreadSummary } = useChatSocket();

  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [selectedConvId, setSelectedConvId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMessages, setLoadingMessages] = useState(false);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [previewImage, setPreviewImage] = useState<string | null>(null);
  const [timeRemainingText, setTimeRemainingText] = useState<string | null>(null);
  const [isExpired, setIsExpired] = useState(false);
  const [dropdownOpen, setDropdownOpen] = useState(false);

  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const messagesEndRef = useRef<HTMLDivElement | null>(null);
  const dropdownRef = useRef<HTMLDivElement | null>(null);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  };

  // Close dropdown on outside click
  useEffect(() => {
    function handleOutsideClick(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    }
    if (dropdownOpen) {
      document.addEventListener("mousedown", handleOutsideClick);
      return () => document.removeEventListener("mousedown", handleOutsideClick);
    }
  }, [dropdownOpen]);

  // Load conversations for the errand
  useEffect(() => {
    let active = true;

    async function loadChat() {
      setLoading(true);
      try {
        const convs = await chatApi.fetchConversationsByErrand(errandId);
        if (!active) return;

        setConversations(convs);

        if (convs.length > 0) {
          // Default to the first active conversation, or the first one overall
          const activeConv = convs.find((c) => c.status === "ACTIVE") ?? convs[0];
          setSelectedConvId((prev) => {
            // Keep previous selection if it still exists in convs
            if (prev && convs.some((c) => c.id === prev)) {
              return prev;
            }
            return activeConv.id;
          });
        } else {
          setSelectedConvId(null);
        }
      } catch (e) {
        console.error("Failed to load conversation:", e);
      } finally {
        if (active) setLoading(false);
      }
    }

    void loadChat();

    return () => {
      active = false;
    };
  }, [errandId, chatApi]);

  // Current conversation selected
  const conversation = conversations.find((c) => c.id === selectedConvId) ?? null;

  // Track active conversation to prevent incoming messages from marking unread while viewing
  useEffect(() => {
    setActiveConversationId(selectedConvId);
    return () => {
      setActiveConversationId(null);
      void refreshUnreadSummary();
    };
  }, [selectedConvId, setActiveConversationId, refreshUnreadSummary]);

  // Load messages whenever selected conversation changes
  useEffect(() => {
    if (!selectedConvId) {
      setMessages([]);
      return;
    }

    let active = true;

    async function loadMessages() {
      setLoadingMessages(true);
      try {
        const page = await chatApi.fetchMessages(selectedConvId!);
        if (!active) return;

        // Chronological ordering
        const sorted = [...page.messages].sort(
          (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
        );
        setMessages(sorted);

        // Mark as read only if current user is participant
        if (conversation && (conversation.requester_id === currentUserId || conversation.courier_id === currentUserId)) {
          void chatApi.markConversationRead(conversation.id);
          markErrandRead(errandId);
        }
        setTimeout(scrollToBottom, 50);
      } catch (e) {
        console.error("Failed to load messages for conversation:", e);
      } finally {
        if (active) setLoadingMessages(false);
      }
    }

    void loadMessages();

    return () => {
      active = false;
    };
  }, [selectedConvId, chatApi, markErrandRead, errandId, currentUserId, conversation]);

  // Real-time WebSocket events listener
  useEffect(() => {
    const unsubscribe = subscribeToEvents((event) => {
      if (event.type === "message.created") {
        const msg = event.data;
        // Append to message list if it belongs to currently viewed conversation
        if (selectedConvId && msg.conversation_id === selectedConvId) {
          setMessages((prev) => {
            if (prev.some((m) => m.id === msg.id || (m.client_message_id && m.client_message_id === msg.client_message_id))) {
              return prev;
            }
            return [...prev, msg];
          });
          if (conversation && (conversation.requester_id === currentUserId || conversation.courier_id === currentUserId)) {
            void chatApi.markConversationRead(conversation.id);
            markErrandRead(errandId);
          }
          setTimeout(scrollToBottom, 50);
        }
      } else if (event.type === "conversation.read_only") {
        const convId = event.data.conversation_id;
        setConversations((prev) =>
          prev.map((c) => (c.id === convId ? { ...c, status: "FROZEN" } : c)),
        );
        if (selectedConvId === convId) {
          setIsExpired(true);
        }
      }
    });

    return unsubscribe;
  }, [selectedConvId, conversation, currentUserId, errandId, subscribeToEvents, chatApi, markErrandRead]);

  // Scroll to bottom when messages update
  useEffect(() => {
    scrollToBottom();
  }, [messages.length]);

  // 24-hour countdown calculation
  useEffect(() => {
    if (!conversation || !conversation.errand_completed_at) {
      setTimeRemainingText(null);
      return;
    }

    const completedAt = new Date(conversation.errand_completed_at).getTime();
    const cutoff = completedAt + 24 * 60 * 60 * 1000;

    const updateCountdown = () => {
      const now = Date.now();
      const diff = cutoff - now;

      if (diff <= 0 || conversation.status === "FROZEN") {
        setIsExpired(true);
        setTimeRemainingText(null);
        return;
      }

      const hours = Math.floor(diff / (1000 * 60 * 60));
      const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));
      const endTimeStr = new Date(cutoff).toLocaleTimeString([], {
        hour: "numeric",
        minute: "2-digit",
      });

      if (hours > 0) {
        setTimeRemainingText(`Errand completed. Chat closes in ${hours}h ${minutes}m (at ${endTimeStr})`);
      } else {
        setTimeRemainingText(`Errand completed. Chat closes in ${minutes}m (at ${endTimeStr})`);
      }
    };

    updateCountdown();
    const timer = setInterval(updateCountdown, 30000);
    return () => clearInterval(timer);
  }, [conversation]);

  const isParticipant =
    conversation != null &&
    (currentUserId === conversation.requester_id || currentUserId === conversation.courier_id);

  const isWithdrawnCourier =
    conversation != null &&
    conversation.status === "FROZEN" &&
    conversation.errand_completed_at == null;

  const isReadOnly =
    !isParticipant ||
    isExpired ||
    conversation?.status === "FROZEN" ||
    (conversation?.errand_completed_at != null &&
      Date.now() >= new Date(conversation.errand_completed_at).getTime() + 24 * 60 * 60 * 1000);

  // Unicode rune character count
  const runeCount = Array.from(text).length;
  const isTooLong = runeCount > 250;

  const handleSendText = async (e: FormEvent) => {
    e.preventDefault();
    if (!conversation || isReadOnly || isTooLong || text.trim() === "" || sending) return;

    const clientMsgId = crypto.randomUUID();
    const content = text;
    setText("");

    const optimisticMsg: ChatMessage = {
      id: clientMsgId,
      conversation_id: conversation.id,
      sender_id: currentUserId,
      message_type: "TEXT",
      text_content: content,
      client_message_id: clientMsgId,
      created_at: new Date().toISOString(),
      status: "pending",
    };

    setMessages((prev) => [...prev, optimisticMsg]);
    setTimeout(scrollToBottom, 50);

    try {
      setSending(true);
      markErrandRead(errandId);
      const saved = await chatApi.sendTextMessage(conversation.id, content, clientMsgId);
      setMessages((prev) =>
        prev.map((m) => (m.client_message_id === clientMsgId ? { ...saved, status: "sent" } : m)),
      );
    } catch {
      setMessages((prev) =>
        prev.map((m) =>
          m.client_message_id === clientMsgId ? { ...m, status: "failed" } : m,
        ),
      );
    } finally {
      setSending(false);
    }
  };

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    setFileError(null);
    const file = e.target.files?.[0];
    if (!file) return;

    // Validate type
    const validTypes = ["image/jpeg", "image/jpg", "image/png"];
    if (!validTypes.includes(file.type.toLowerCase())) {
      setFileError("Only JPG and PNG images are allowed.");
      return;
    }

    // Validate size (10MB)
    if (file.size > 10 * 1024 * 1024) {
      setFileError("Image file exceeds 10MB limit.");
      return;
    }

    setSelectedFile(file);
  };

  const handleSendImage = async () => {
    if (!conversation || !selectedFile || isReadOnly || sending) return;

    const clientMsgId = crypto.randomUUID();
    const fileToSend = selectedFile;
    setSelectedFile(null);
    if (fileInputRef.current) fileInputRef.current.value = "";

    const localUrl = URL.createObjectURL(fileToSend);

    const optimisticMsg: ChatMessage = {
      id: clientMsgId,
      conversation_id: conversation.id,
      sender_id: currentUserId,
      message_type: "IMAGE",
      media_url: localUrl,
      client_message_id: clientMsgId,
      created_at: new Date().toISOString(),
      status: "pending",
    };

    setMessages((prev) => [...prev, optimisticMsg]);
    setTimeout(scrollToBottom, 50);

    try {
      setSending(true);
      markErrandRead(errandId);
      const saved = await chatApi.sendImageMessage(conversation.id, fileToSend, clientMsgId);
      setMessages((prev) =>
        prev.map((m) => (m.client_message_id === clientMsgId ? { ...saved, status: "sent" } : m)),
      );
    } catch {
      setMessages((prev) =>
        prev.map((m) =>
          m.client_message_id === clientMsgId ? { ...m, status: "failed" } : m,
        ),
      );
    } finally {
      setSending(false);
    }
  };

  const handleRetry = async (failedMsg: ChatMessage) => {
    if (!conversation || isReadOnly) return;

    setMessages((prev) =>
      prev.map((m) => (m.id === failedMsg.id ? { ...m, status: "pending" } : m)),
    );

    try {
      const clientMsgId = failedMsg.client_message_id || failedMsg.id;
      const saved = await chatApi.sendTextMessage(
        conversation.id,
        failedMsg.text_content || "",
        clientMsgId,
      );
      setMessages((prev) =>
        prev.map((m) => (m.id === failedMsg.id ? { ...saved, status: "sent" } : m)),
      );
    } catch {
      setMessages((prev) =>
        prev.map((m) => (m.id === failedMsg.id ? { ...m, status: "failed" } : m)),
      );
    }
  };

  if (loading) {
    return (
      <div className="chat-container">
        <div className="chat-loading">Connecting to chat…</div>
      </div>
    );
  }

  if (conversations.length === 0 || !conversation) {
    return (
      <div className="chat-container">
        <div className="chat-empty">No courier has accepted this errand yet. Chat will appear once accepted.</div>
      </div>
    );
  }

  // Label helper for courier conversations
  const getConversationLabel = (conv: Conversation, index: number) => {
    const isConvActive = conv.status === "ACTIVE";
    const courierNum = conversations.length - index;
    if (isConvActive) {
      return `Courier ${courierNum} (Current)`;
    }
    return `Courier ${courierNum} (Withdrawn)`;
  };

  return (
    <div className="chat-container" role="region" aria-label="Real-time Errand Chat">
      {/* Multi-Courier Header Dropdown */}
      {conversations.length > 1 && (
        <div className="chat-header-bar" ref={dropdownRef}>
          <div className="chat-header-info">
            <span className="chat-header-title">Chat Session</span>
          </div>
          <div className="chat-dropdown-wrapper">
            <button
              type="button"
              className="chat-dropdown-btn"
              onClick={() => setDropdownOpen((prev) => !prev)}
              aria-haspopup="listbox"
              aria-expanded={dropdownOpen}
            >
              <span>{getConversationLabel(conversation, conversations.indexOf(conversation))}</span>
              <span className="chat-dropdown-caret">▾</span>
            </button>

            {dropdownOpen && (
              <div className="chat-dropdown-menu" role="listbox">
                {conversations.map((conv, idx) => {
                  const isSelected = conv.id === selectedConvId;
                  const isConvActive = conv.status === "ACTIVE";
                  return (
                    <button
                      key={conv.id}
                      type="button"
                      role="option"
                      aria-selected={isSelected}
                      className={`chat-dropdown-item ${isSelected ? "selected" : ""}`}
                      onClick={() => {
                        setSelectedConvId(conv.id);
                        setDropdownOpen(false);
                      }}
                    >
                      <span className="chat-item-label">{getConversationLabel(conv, idx)}</span>
                      <span className={`chat-item-badge ${isConvActive ? "active" : "withdrawn"}`}>
                        {isConvActive ? "Active" : "Withdrawn"}
                      </span>
                      {isSelected && <span className="chat-item-check">✓</span>}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      )}

      {/* 24h Countdown Banner */}
      {timeRemainingText && !isReadOnly && (
        <div className="chat-banner countdown" role="status">
          ⏱️ {timeRemainingText}
        </div>
      )}

      {/* Read-Only Notice */}
      {isReadOnly && (
        <div className="chat-banner readonly" role="alert">
          {isWithdrawnCourier
            ? "🔒 This courier has withdrawn. This conversation is archived and read-only."
            : !isParticipant
            ? "🔒 Admin audit view (read-only)."
            : "🔒 This conversation is read-only."}
        </div>
      )}

      {/* Message List */}
      <div className="chat-messages" role="log" aria-live="polite">
        {loadingMessages ? (
          <div className="chat-messages-empty">Loading messages…</div>
        ) : messages.length === 0 ? (
          <div className="chat-messages-empty">No messages yet. Say hello! 👋</div>
        ) : (
          messages.map((msg) => {
            let isRightAligned: boolean;
            let bubbleClass: string;
            let senderLabel: string | null = null;

            if (isParticipant) {
              const isMine = msg.sender_id === currentUserId;
              isRightAligned = isMine;
              bubbleClass = isMine ? "bubble-mine" : "bubble-theirs";
            } else {
              const isRequesterMsg = conversation != null && msg.sender_id === conversation.requester_id;
              if (isRequesterMsg) {
                isRightAligned = false;
                bubbleClass = "bubble-admin-requester";
                senderLabel = "Requester";
              } else {
                isRightAligned = true;
                bubbleClass = "bubble-admin-courier";
                senderLabel = "Courier";
              }
            }

            const timeStr = new Date(msg.created_at).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            });

            return (
              <div
                key={msg.id}
                className={`chat-bubble-row ${isRightAligned ? "mine" : "theirs"}`}
              >
                <div className={`chat-bubble-container ${isRightAligned ? "align-right" : "align-left"}`}>
                  {senderLabel && <span className="chat-sender-label">{senderLabel}</span>}
                  <div className={`chat-bubble ${bubbleClass}`}>
                    {msg.message_type === "IMAGE" && msg.media_url && (
                      <div className="chat-image-wrapper">
                        <img
                          src={
                            msg.media_url.startsWith("blob:")
                              ? msg.media_url
                              : `${config.gatewayBaseUrl}${msg.media_url}`
                          }
                          alt="Shared image"
                          className="chat-image-thumbnail"
                          onClick={() =>
                            setPreviewImage(
                              msg.media_url!.startsWith("blob:")
                                ? msg.media_url!
                                : `${config.gatewayBaseUrl}${msg.media_url}`,
                            )
                          }
                        />
                      </div>
                    )}

                    {msg.text_content && (
                      <p className="chat-text">{msg.text_content}</p>
                    )}

                    <div className="chat-meta">
                      <span className="chat-time">{timeStr}</span>
                      {msg.status === "pending" && (
                        <span className="chat-status sending">Sending…</span>
                      )}
                      {msg.status === "failed" && (
                        <span className="chat-status failed">
                          Not sent.{" "}
                          <button
                            type="button"
                            className="retry-btn"
                            onClick={() => handleRetry(msg)}
                          >
                            Retry
                          </button>
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            );
          })
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Image Preview Modal */}
      {previewImage && (
        <div className="image-modal-backdrop" onClick={() => setPreviewImage(null)}>
          <div className="image-modal-content">
            <img src={previewImage} alt="Enlarged view" />
            <button
              type="button"
              className="image-modal-close"
              onClick={() => setPreviewImage(null)}
            >
              ✕
            </button>
          </div>
        </div>
      )}

      {/* Input Form */}
      {!isReadOnly ? (
        <div className="chat-input-bar">
          {fileError && <div className="chat-file-error">{fileError}</div>}

          {selectedFile && (
            <div className="chat-file-preview">
              <span>📎 {selectedFile.name} ({(selectedFile.size / 1024).toFixed(1)} KB)</span>
              <button
                type="button"
                className="btn-send-image"
                onClick={handleSendImage}
                disabled={sending}
              >
                Send Image
              </button>
              <button
                type="button"
                className="btn-cancel-file"
                onClick={() => setSelectedFile(null)}
              >
                ✕
              </button>
            </div>
          )}

          <form onSubmit={handleSendText} className="chat-form">
            <input
              type="file"
              ref={fileInputRef}
              accept="image/jpeg,image/png,image/jpg"
              style={{ display: "none" }}
              onChange={handleFileChange}
              disabled={sending}
            />

            <button
              type="button"
              className="chat-btn-attach"
              title="Attach image (JPEG/PNG up to 10MB)"
              aria-label="Attach image"
              onClick={() => fileInputRef.current?.click()}
              disabled={sending}
            >
              📷
            </button>

            <input
              type="text"
              className="chat-input"
              placeholder="Type a message (up to 250 characters)…"
              value={text}
              onChange={(e) => setText(e.target.value)}
              disabled={sending}
              maxLength={300} // slight buffer over 250 for typing
            />

            <span className={`chat-char-counter ${isTooLong ? "overflow" : ""}`}>
              {runeCount}/250
            </span>

            <button
              type="submit"
              className="btn btn-primary chat-btn-send"
              disabled={sending || text.trim() === "" || isTooLong}
            >
              Send
            </button>
          </form>
        </div>
      ) : (
        <div className="chat-disabled-bar">
          <span>
            {isWithdrawnCourier
              ? "This conversation is archived because the courier has withdrawn."
              : !isParticipant
              ? "Admin audit view: message sending is disabled."
              : "Messaging is disabled for this conversation."}
          </span>
        </div>
      )}
    </div>
  );
}
