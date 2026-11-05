interface UnreadBadgeProps {
  className?: string;
}

export function UnreadBadge({ className = "" }: UnreadBadgeProps) {
  return (
    <span
      className={`unread-dot ${className}`}
      aria-label="New unread message"
      title="New unread message"
    />
  );
}
