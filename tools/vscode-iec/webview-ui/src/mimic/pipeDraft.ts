// Pure logic for the pipe-drawing draft's completion (Enter / double-click
// finish) — split out of EditorCanvas.svelte so it's unit-testable without a
// Svelte/DOM harness (none exists for EditorCanvas — see mimicOps.test.ts's
// note) and so the anchor-aware point-count floor has exactly ONE copy in
// the webview, instead of a second hand-rolled check drifting from it.
import type { MimicPipeAnchor } from '@joyautomation/nautilus-hmi';

/** How many INTERIOR [x, y] points a pipe needs given which ends are
 * anchored. Mirror of src/mimicOps.ts's minInteriorPoints (the extension
 * host reducer is the authority — it validates the wire op; this copy lets
 * the webview evaluate the SAME floor locally, without a round trip, for
 * the draw-draft completion check below and EditorCanvas's vertex-delete
 * cascade). A plain pipe still needs 2 (start + end); each anchored end
 * supplies its own endpoint instead, so the floor drops by one per anchored
 * end (both anchored -> 0: just the two ports, nothing between them). */
export function minInteriorPoints(fromSet: boolean, toSet: boolean): number {
	return Math.max(0, 2 - (fromSet ? 1 : 0) - (toSet ? 1 : 0));
}

/** A cursor point snapped onto a named equipment port, WITH its resolved
 * canvas position — see EditorCanvas.svelte's portSnapNamed(). The position
 * isn't needed below (only used by the caller, to know WHERE to draw the
 * hover feedback) — accepting the fuller shape here just avoids the caller
 * stripping it down to {equip, port} before calling in. */
export type NamedPort = { equip: string; port: string; x: number; y: number };

export type DraftFinish = {
	points: [number, number][];
	from?: MimicPipeAnchor;
	to?: MimicPipeAnchor;
};

const floorMet = (pts: [number, number][], from: MimicPipeAnchor | null, to: MimicPipeAnchor | null): boolean =>
	pts.length - (from ? 1 : 0) - (to ? 1 : 0) >= minInteriorPoints(!!from, !!to);

/** Resolve a pipe-drawing draft into an addPipe payload, or null when
 * there isn't enough to complete a pipe yet.
 *
 * `draft` is every point placed by an explicit click so far; `draftFrom`/
 * `draftLastSnap` are the port anchors (if any) the first/most-recent click
 * landed on. `cursor` is the live rubber-band end and `hoverPort` the port,
 * if any, it is currently resting on.
 *
 * The live rubber-band segment is treated as an IMPLICIT final click —
 * mirroring what an actual click there would produce — in EITHER of two
 * cases:
 *
 *   1. The cursor is resting on a PORT the draft hasn't already ended on
 *      (`hoverPort` set, `draftLastSnap` null). The draw preview has been
 *      showing that connecting segment the whole time; dropping it on finish
 *      is the "Enter completes the pipe but not TO the port" bug — the pipe
 *      lands short of the port the user was clearly aiming at. This holds
 *      even when the clicked draft ALREADY meets the floor (e.g. a bent
 *      pipe: click port A, click a corner, then Enter over port B), which is
 *      exactly the case the old `!floorMet`-only guard silently dropped.
 *
 *   2. The clicked draft alone doesn't yet meet the anchor-aware floor
 *      (minInteriorPoints) — the rubber-band point is needed to form a pipe
 *      at all (e.g. one click plus Enter over its far end).
 *
 * A cursor merely DRIFTING over empty canvas on an already-complete draft is
 * still ignored (case-1 needs a port, case-2 needs an unmet floor), so an
 * ordinary multi-click floating pipe finishes exactly as clicked. */
export function resolveDraftFinish(
	draft: [number, number][],
	draftFrom: MimicPipeAnchor | null,
	draftLastSnap: MimicPipeAnchor | null,
	cursor: [number, number] | null,
	hoverPort: NamedPort | null
): DraftFinish | null {
	let pts = draft;
	let to = draftLastSnap;

	if (draft.length >= 1 && cursor) {
		const last = draft[draft.length - 1];
		const cursorIsNewPoint = cursor[0] !== last[0] || cursor[1] !== last[1];
		// Aiming at a port the draft hasn't already terminated on (case 1) —
		// or the draft can't stand on its own yet (case 2).
		const aimingAtNewPort = !!hoverPort && !draftLastSnap;
		if (cursorIsNewPoint && (aimingAtNewPort || !floorMet(pts, draftFrom, to))) {
			pts = [...draft, cursor];
			to = hoverPort ? { equip: hoverPort.equip, port: hoverPort.port } : null;
		}
	}

	if (!floorMet(pts, draftFrom, to)) return null;

	const interior = pts.slice(draftFrom ? 1 : 0, to ? -1 : undefined);
	return {
		points: interior,
		...(draftFrom ? { from: draftFrom } : {}),
		...(to ? { to } : {})
	};
}

/** The candidate nearest to `p` within `radius` (Math.hypot), so two ports
 * closer together than the snap radius don't hand the hit to whichever one
 * comes first in file order. */
export function nearestPort(p: [number, number], ports: NamedPort[], radius: number): NamedPort | null {
	let best: NamedPort | null = null;
	let bestD = radius;
	for (const port of ports) {
		const d = Math.hypot(port.x - p[0], port.y - p[1]);
		if (d <= bestD) {
			best = port;
			bestD = d;
		}
	}
	return best;
}
