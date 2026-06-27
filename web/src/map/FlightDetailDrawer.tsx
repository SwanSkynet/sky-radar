import { useEffect, useState } from "react";
import type { FlightState } from "../api/flights";
import { useFlightStore } from "../store/useFlightStore";
import {
  aircraftTypeLabel,
  decodeSquawkEmergency,
  headerLabel,
  positionQualityLabel,
  secondsAgo,
  verticalRateArrow,
} from "./flightFormat";

// Row renders a labelled value, greying out when the value is null/empty so
// the field set stays visible but the absence is obvious (per the decided
// drawer spec — "omit/grey rows whose value is null").
function Row({ label, value }: { label: string; value: React.ReactNode }) {
  const empty = value == null || value === "";
  return (
    <div className="flex justify-between gap-4 py-1 text-sm">
      <span className="text-text">{label}</span>
      <span
        className={
          empty ? "text-text/40 italic" : "text-text-h text-right font-mono"
        }
      >
        {empty ? "—" : value}
      </span>
    </div>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="border-t border-border pt-2">
      <div className="mb-1 text-xs tracking-wide text-text/60 uppercase">
        {title}
      </div>
      {children}
    </div>
  );
}

function fmtNumber(n: number | null, suffix = "", digits = 0): string | null {
  if (n == null) return null;
  return `${n.toFixed(digits)}${suffix}`;
}

// FlightDetailDrawer shows the currently selected aircraft. It tracks the
// selection by icao24 (via the store), reading the *current* FlightState for
// that aircraft each render, so it keeps updating live as new WS messages
// upsert that aircraft without the user having to reselect.
export function FlightDetailDrawer() {
  const selectedIcao24 = useFlightStore((s) => s.selectedIcao24);
  const flight = useFlightStore((s) =>
    s.selectedIcao24 ? s.flights[s.selectedIcao24] : undefined,
  );
  const clearSelection = useFlightStore((s) => s.clearSelection);

  // Re-render once a second so the "last seen N s ago" row keeps ticking even
  // when no new message has arrived for this aircraft.
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!selectedIcao24) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [selectedIcao24]);

  // Close on Esc whenever a selection is active.
  useEffect(() => {
    if (!selectedIcao24) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") clearSelection();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedIcao24, clearSelection]);

  if (!selectedIcao24) return null;

  // Selection set but the aircraft isn't (yet) in view — show a minimal shell
  // rather than nothing, so the close affordance is still reachable.
  if (!flight) {
    return (
      <DrawerShell onClose={clearSelection} title={selectedIcao24.toUpperCase()}>
        <div className="py-4 text-sm text-text/60">
          aircraft no longer in view
        </div>
      </DrawerShell>
    );
  }

  return (
    <DrawerShell
      onClose={clearSelection}
      title={headerLabel(flight)}
      badges={<HeaderBadges flight={flight} />}
    >
      <div className="flex flex-col gap-3">
        <Section title="Identity">
          <Row label="ICAO24" value={flight.icao24.toUpperCase()} />
          <Row label="Registration" value={flight.registration} />
          <Row label="Type" value={aircraftTypeLabel(flight)} />
        </Section>

        <Section title="Altitude">
          <Row
            label="Barometric"
            value={fmtNumber(flight.altitude_baro_ft, " ft")}
          />
          <Row
            label="Geometric"
            value={fmtNumber(flight.altitude_geo_ft, " ft")}
          />
        </Section>

        <Section title="Motion">
          <Row
            label="Ground speed"
            value={fmtNumber(flight.ground_speed_kt, " kt")}
          />
          <Row
            label="Vertical rate"
            value={
              flight.vertical_rate_fpm == null
                ? null
                : `${verticalRateArrow(flight.vertical_rate_fpm)} ${Math.round(
                    flight.vertical_rate_fpm,
                  )} fpm`
            }
          />
          <Row label="Heading" value={fmtNumber(flight.heading_deg, "°")} />
        </Section>

        <Section title="Status">
          <Row label="On ground" value={flight.on_ground ? "yes" : "no"} />
          <Row label="Squawk" value={squawkValue(flight)} />
        </Section>

        <Section title="Position">
          <Row
            label="Lat / Lon"
            value={`${flight.lat.toFixed(4)}, ${flight.lon.toFixed(4)}`}
          />
          <Row
            label="Quality"
            value={positionQualityLabel(flight.position_quality)}
          />
        </Section>

        <Section title="Provenance">
          <Row
            label="Sources"
            value={flight.sources.length ? flight.sources.join(", ") : null}
          />
          <Row
            label="Last seen"
            value={`${secondsAgo(flight.last_seen_utc)} s ago`}
          />
        </Section>
      </div>
    </DrawerShell>
  );
}

function squawkValue(flight: FlightState): React.ReactNode {
  if (!flight.squawk) return null;
  const emergency = decodeSquawkEmergency(flight.squawk);
  if (emergency) {
    return (
      <span className="text-red-500">
        {flight.squawk} ({emergency})
      </span>
    );
  }
  return flight.squawk;
}

function HeaderBadges({ flight }: { flight: FlightState }) {
  return (
    <div className="flex gap-1">
      {flight.military && (
        <span className="rounded bg-red-900/70 px-1.5 py-0.5 text-xs text-red-100">
          military
        </span>
      )}
      {flight.stale && (
        <span className="rounded bg-amber-900/70 px-1.5 py-0.5 text-xs text-amber-100">
          stale
        </span>
      )}
    </div>
  );
}

function DrawerShell({
  title,
  badges,
  onClose,
  children,
}: {
  title: string;
  badges?: React.ReactNode;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <aside
      className="absolute top-0 right-0 z-20 flex h-full w-80 max-w-[85vw] flex-col gap-3 overflow-y-auto border-l border-border bg-bg/95 p-4 shadow-lg"
      aria-label="flight detail"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-col gap-1">
          <span className="font-mono text-lg text-text-h">{title}</span>
          {badges}
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="close flight detail"
          className="rounded px-2 py-1 text-text hover:text-text-h"
        >
          ✕
        </button>
      </div>
      {children}
    </aside>
  );
}
