import { useEffect } from "react";
import { useReplayStore } from "../store/useReplayStore";

// REPLAY_PLAYBACK_SPEED compresses wall-clock playback relative to the
// replay window's own timeline — at 1x, scrubbing through the default
// 30-minute window would take 30 real minutes, which makes for a
// pointless "play" button. 30x turns a 30-minute window into about a
// minute of playback.
const REPLAY_PLAYBACK_SPEED = 30;
const REPLAY_TICK_INTERVAL_MS = 250;

function formatClock(ms: number): string {
  return new Date(ms).toLocaleTimeString();
}

// ReplayScrubber renders the bottom control bar for replay mode: a
// play/pause toggle, a time slider over the loaded window, and an exit
// control. It renders nothing in live mode (isActive false). Distinct
// amber styling (vs. the live map's blue/gray aircraft and status
// badges) is what makes replay visually unmistakable from live, per
// docs/prd/phase-2-realtime-systems.md P2-FR5.
export function ReplayScrubber() {
  const isActive = useReplayStore((s) => s.isActive);
  const isLoading = useReplayStore((s) => s.isLoading);
  const isPlaying = useReplayStore((s) => s.isPlaying);
  const error = useReplayStore((s) => s.error);
  const windowStartMs = useReplayStore((s) => s.windowStartMs);
  const windowEndMs = useReplayStore((s) => s.windowEndMs);
  const scrubMs = useReplayStore((s) => s.scrubMs);
  const setScrub = useReplayStore((s) => s.setScrub);
  const togglePlay = useReplayStore((s) => s.togglePlay);
  const exit = useReplayStore((s) => s.exit);

  useEffect(() => {
    if (!isPlaying || scrubMs === null) return;
    const timer = setInterval(() => {
      setScrub(scrubMs + REPLAY_TICK_INTERVAL_MS * REPLAY_PLAYBACK_SPEED);
    }, REPLAY_TICK_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [isPlaying, scrubMs, setScrub]);

  if (!isActive) return null;

  return (
    <div className="absolute bottom-3 left-3 right-3 z-10 rounded bg-amber-950/90 px-4 py-3 text-amber-100 shadow">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-semibold uppercase tracking-wide">
          Replay mode
        </span>
        <button type="button" onClick={exit} className="text-xs underline">
          exit replay
        </button>
      </div>
      {isLoading && <div className="text-xs">loading replay window…</div>}
      {error && <div className="text-xs text-red-300">{error}</div>}
      {windowStartMs !== null && windowEndMs !== null && scrubMs !== null && (
        <div className="mt-2 flex items-center gap-3">
          <button
            type="button"
            onClick={togglePlay}
            className="rounded bg-amber-800 px-2 py-1 text-xs"
          >
            {isPlaying ? "pause" : "play"}
          </button>
          <input
            type="range"
            min={windowStartMs}
            max={windowEndMs}
            value={scrubMs}
            onChange={(e) => setScrub(Number(e.target.value))}
            className="flex-1"
          />
          <span className="w-20 text-right text-xs">
            {formatClock(scrubMs)}
          </span>
        </div>
      )}
    </div>
  );
}
