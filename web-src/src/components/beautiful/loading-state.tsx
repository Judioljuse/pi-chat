// Beautiful UI (https://www.beautifului.dev) — MIT License, Copyright (c) 2026 Shane Levine.
// Copied via the site's Copy button; restyled to pichat tokens. See THIRD_PARTY_NOTICES.md.

import { useEffect, useState } from "react";

/* ─────────────────────────────────────────────────────────
 * LOADING STATE — pixel-grid loader for long-running work
 *
 * Variants:
 *   Drive  — square cells, chevron wavefront driving right;
 *            the 650ms cycle is shorter than the sweep, so
 *            two fronts are always in flight
 *   Dots   — same wavefront, circular cells
 *   Orbit  — a comet lapping the grid perimeter
 *
 * (pichat: the Surfer variant, which streamed a remote video, was removed.)
 * Paired with a shimmering label and a live elapsed timer
 * in mono tabular figures. Reduced motion freezes the grid
 * to its dim state; the timer still ticks.
 * ───────────────────────────────────────────────────────── */

const chevron = Array.from({ length: 9 }, (_, i) => {
  const r = Math.floor(i / 3), c = i % 3;
  return (c + Math.abs(r - 1)) * 90;
});

const ORBIT_ORDER = [0, 1, 2, 5, 8, 7, 6, 3];
const orbit = Array.from({ length: 9 }, (_, i) => {
  const k = ORBIT_ORDER.indexOf(i);
  return k === -1 ? null : k * 110;
});

const PATTERNS: Record<string, { delays: (number | null)[]; dur: number; round: boolean }> = {
  Drive: { delays: chevron, dur: 650, round: false },
  Dots: { delays: chevron, dur: 650, round: true },
  Orbit: { delays: orbit, dur: 950, round: false },
};

export function LoaderGrid({
  variant = "Drive",
  cell = 4,
  className,
}: {
  variant?: "Drive" | "Dots" | "Orbit";
  /** Cell size in px (pichat). */
  cell?: number;
  className?: string;
}) {
  const { delays, dur, round } = PATTERNS[variant] ?? PATTERNS.Drive;
  return (
    <span
      aria-hidden
      className={`grid shrink-0 gap-[1.5px] ${className ?? ""}`}
      style={{ gridTemplateColumns: `repeat(3, ${cell}px)` }}
    >
      {delays.map((delay, index) => (
        <span
          key={index}
          className={`bg-foreground ${round ? "rounded-full" : "rounded-[1px]"}`}
          style={{
            width: cell,
            height: cell,
            opacity: delay === null ? 0.07 : 0.15,
            animation: delay === null ? "none" : `pixel-on ${dur}ms ease-in-out ${delay}ms infinite`,
          }}
        />
      ))}
    </span>
  );
}

function useElapsed(startedAt?: number) {
  const [now, setNow] = useState(() => Date.now());
  const [origin] = useState(() => startedAt ?? Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), startedAt ? 1000 : 100);
    return () => clearInterval(t);
  }, [startedAt]);
  const total = Math.max(0, (now - (startedAt ?? origin)) / 1000);
  if (startedAt) {
    if (total < 60) return `${Math.floor(total)}s`;
    return `${Math.floor(total / 60)}m ${Math.floor(total % 60)}s`;
  }
  if (total < 60) return `${total.toFixed(1)}s`;
  return `${Math.floor(total / 60)}m ${(total % 60).toFixed(1)}s`;
}

export default function LoadingState({
  label = "正在处理…",
  variant = "Drive",
  startedAt,
  showGrid = true,
  className,
}: {
  label?: string;
  variant?: "Drive" | "Dots" | "Orbit";
  /** pichat: hide the inline grid when the caller places it elsewhere. */
  showGrid?: boolean;
  /** Unix ms; when set the timer counts whole seconds from it. */
  startedAt?: number;
  className?: string;
}) {
  const elapsed = useElapsed(startedAt);

  return (
    <div role="status" className={`flex w-fit items-center gap-2.5 ${className ?? ""}`}>
      {showGrid && <LoaderGrid variant={variant} />}
      <span
        className="bg-clip-text text-[14px] font-medium text-transparent"
        style={{
          backgroundImage:
            "linear-gradient(90deg, var(--muted-foreground) 30%, var(--foreground) 50%, var(--muted-foreground) 70%)",
          backgroundSize: "250% 100%",
          animation: "shimmer-text 1.8s linear infinite",
        }}
      >
        {label}
      </span>
      <span className="inline-block min-w-[5ch] font-mono text-[12px] text-muted-foreground tabular-nums">{elapsed}</span>
    </div>
  );
}
