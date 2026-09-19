// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Image with placeholder fallback, replacing the prototype's inline
//   onerror attribute.
// Author review: PENDING — <reviewer to complete>

import { useState } from "react";

interface SupplierImageProps {
  src: string;
  alt: string;
  className: string;
}

/**
 * Seed rows can carry a broken or empty image_url, so a missing image is a
 * normal state rather than an error. Renders the 🏬 placeholder in that case.
 */
export function SupplierImage({ src, alt, className }: SupplierImageProps) {
  const [failed, setFailed] = useState(false);

  if (!src || failed) {
    return <div className={`${className} ${className}-placeholder`}>🏬</div>;
  }
  return (
    <img
      className={className}
      src={src}
      alt={alt}
      loading="lazy"
      onError={() => setFailed(true)}
    />
  );
}
