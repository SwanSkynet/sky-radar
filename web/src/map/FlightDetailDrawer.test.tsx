import { describe, it, expect, beforeEach } from "vitest";
import { render, screen, fireEvent, within, act } from "@testing-library/react";
import { FlightDetailDrawer } from "./FlightDetailDrawer";
import { useFlightStore } from "../store/useFlightStore";
import type { FlightState } from "../api/flights";

function flight(overrides: Partial<FlightState> = {}): FlightState {
  return {
    icao24: "abc123",
    callsign: "UAL123",
    registration: "N12345",
    lat: 34.05,
    lon: -118.24,
    altitude_baro_ft: 35000,
    altitude_geo_ft: 35200,
    ground_speed_kt: 420,
    vertical_rate_fpm: 1200,
    heading_deg: 90,
    on_ground: false,
    squawk: "1200",
    sources: ["adsb.lol"],
    position_quality: "adsb",
    last_seen_utc: new Date().toISOString(),
    stale: false,
    aircraft_type: "A320",
    emitter_category: "A3",
    military: false,
    icon_class: "commercial_jet",
    ...overrides,
  };
}

describe("FlightDetailDrawer", () => {
  beforeEach(() => {
    useFlightStore.setState({
      flights: {},
      connectionStatus: "open",
      lastUpdated: null,
      error: null,
      selectedIcao24: null,
    });
  });

  it("renders nothing when no aircraft is selected", () => {
    const { container } = render(<FlightDetailDrawer />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the selected aircraft's fields", () => {
    useFlightStore.setState({
      flights: { abc123: flight() },
      selectedIcao24: "abc123",
    });
    render(<FlightDetailDrawer />);

    const drawer = screen.getByLabelText("flight detail");
    expect(within(drawer).getByText("UAL123")).toBeInTheDocument();
    expect(within(drawer).getByText("N12345")).toBeInTheDocument();
    expect(within(drawer).getByText("A320 · commercial jet")).toBeInTheDocument();
    expect(within(drawer).getByText("35000 ft")).toBeInTheDocument();
  });

  it("reflects a live update for the selected aircraft without reselecting", () => {
    useFlightStore.setState({
      flights: { abc123: flight({ ground_speed_kt: 420 }) },
      selectedIcao24: "abc123",
    });
    render(<FlightDetailDrawer />);
    expect(screen.getByText("420 kt")).toBeInTheDocument();

    // Simulate a new WS message upserting the same aircraft.
    act(() => {
      useFlightStore.getState().upsertFlight(flight({ ground_speed_kt: 480 }));
    });

    expect(screen.getByText("480 kt")).toBeInTheDocument();
    expect(screen.queryByText("420 kt")).not.toBeInTheDocument();
  });

  it("decodes an emergency squawk", () => {
    useFlightStore.setState({
      flights: { abc123: flight({ squawk: "7700" }) },
      selectedIcao24: "abc123",
    });
    render(<FlightDetailDrawer />);
    expect(screen.getByText(/7700 \(general emergency\)/)).toBeInTheDocument();
  });

  it("shows a military badge when flagged", () => {
    useFlightStore.setState({
      flights: { abc123: flight({ military: true }) },
      selectedIcao24: "abc123",
    });
    render(<FlightDetailDrawer />);
    expect(screen.getByText("military")).toBeInTheDocument();
  });

  it("clears the selection on close-button click and on Esc", () => {
    useFlightStore.setState({
      flights: { abc123: flight() },
      selectedIcao24: "abc123",
    });
    const { rerender } = render(<FlightDetailDrawer />);

    fireEvent.click(screen.getByLabelText("close flight detail"));
    expect(useFlightStore.getState().selectedIcao24).toBeNull();

    // Reselect and clear via Esc.
    useFlightStore.setState({ selectedIcao24: "abc123" });
    rerender(<FlightDetailDrawer />);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(useFlightStore.getState().selectedIcao24).toBeNull();
  });
});
