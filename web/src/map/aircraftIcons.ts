// Maps a FlightState's classified IconClass bucket to its SVG and provides
// the heading→angle conversion the IconLayer uses. Kept free of deck.gl
// imports so the pure helpers can be unit-tested directly.

import commercialJet from "../assets/commercial_jet.svg";
import widebody from "../assets/widebody.svg";
import businessJet from "../assets/business_jet.svg";
import cargo from "../assets/cargo.svg";
import turboprop from "../assets/turboprop.svg";
import utilityHelicopter from "../assets/utility_helicopter.svg";
import attackHelicopter from "../assets/attack_helicopter.svg";
import militaryTransport from "../assets/military_transport.svg";
import militaryDrone from "../assets/military_drone.svg";
import tanker from "../assets/tanker.svg";
import awacs from "../assets/awacs.svg";
import reconnaissance from "../assets/reconnaissance.svg";
import maritimePatrol from "../assets/maritime_patrol.svg";
import stealth from "../assets/stealth.svg";

// Default for unclassified / OpenSky-sourced aircraft (decided in the
// feature spec). Must be a key of ICON_URLS.
export const DEFAULT_ICON_CLASS = "commercial_jet";

// className → SVG asset URL. Keys mirror the classifier buckets in
// internal/aircrafttype and the SVG file names in src/assets.
export const ICON_URLS: Record<string, string> = {
  commercial_jet: commercialJet,
  widebody,
  business_jet: businessJet,
  cargo,
  turboprop,
  utility_helicopter: utilityHelicopter,
  attack_helicopter: attackHelicopter,
  military_transport: militaryTransport,
  military_drone: militaryDrone,
  tanker,
  awacs,
  reconnaissance,
  maritime_patrol: maritimePatrol,
  stealth,
};

// Native pixel size of the source SVGs (viewBox 0 0 512 512).
const ICON_SIZE = 512;

export interface IconDef {
  id: string;
  url: string;
  width: number;
  height: number;
  // mask:true lets the IconLayer tint each silhouette via getColor, so the
  // existing fresh-blue / stale-grey / replay-amber semantics carry over.
  mask: true;
  anchorX: number;
  anchorY: number;
}

// resolveIconClass returns a valid icon-bucket key for an aircraft's
// icon_class, falling back to the default for null/unknown values (incl.
// OpenSky-sourced aircraft, which carry no type).
export function resolveIconClass(iconClass: string | null | undefined): string {
  if (iconClass && iconClass in ICON_URLS) return iconClass;
  return DEFAULT_ICON_CLASS;
}

// iconDefFor returns the deck.gl icon definition for an aircraft's class.
export function iconDefFor(iconClass: string | null | undefined): IconDef {
  const cls = resolveIconClass(iconClass);
  return {
    id: cls,
    url: ICON_URLS[cls],
    width: ICON_SIZE,
    height: ICON_SIZE,
    mask: true,
    anchorX: ICON_SIZE / 2,
    anchorY: ICON_SIZE / 2,
  };
}

// headingToIconAngle converts an aviation heading (degrees clockwise from
// north) to deck.gl IconLayer's getAngle convention (degrees
// counter-clockwise, 0 = icon as authored, which points north). A null
// heading renders pointing north.
export function headingToIconAngle(heading: number | null | undefined): number {
  // `!heading` covers null/undefined/NaN and 0 itself — the latter avoids
  // returning -0, which is distinct from 0 under Object.is.
  if (!heading) return 0;
  return -heading;
}
