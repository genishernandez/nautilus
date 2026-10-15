// The FBD canvas's opening frame, pure (FitController applies it).
//
// "Fit everything" is the wrong frame for a big program: at the zoom that
// fits a lift station's whole FBD, pins are ~4 px and pin names ~5 px
// (gesture finding 19, #79) — nothing can be read or wired. So the editor
// opens fitted only while that keeps the diagram readable; past that it
// opens at the readable floor on the diagram's top-left (the first
// network's header) and the user zooms out or pans from there. The fit
// command (Ctrl+0, the corner fit button) still fits everything.

export type Rect = { x: number; y: number; width: number; height: number };
export type Viewport = { x: number; y: number; zoom: number };

/** The lowest zoom the editor OPENS at: block titles and chips (12 px)
 * draw at ≥ 10.8 px, pin names (9 px) at ≥ 8.1 px, pin handles (7 px) at
 * ≥ 6.3 px. Below it the diagram is a picture, not an editor. */
export const READABLE_ZOOM = 0.9;

/** xyflow's fitView arithmetic (getViewportForBounds, @xyflow/system
 * 0.0.x, numeric padding): the zoom that fits `bounds` inside the pane
 * less `padding` (a fraction), clamped to [minZoom, maxZoom], centred.
 * FitController passes xyflow's own function; this is the same rule for
 * the unit tests. */
export function fitViewport(bounds: Rect, pane: { width: number; height: number }, padding = 0.1, minZoom = 0.15, maxZoom = 2): Viewport {
	const pad = (v: number) => Math.floor((v - v / (1 + padding)) * 0.5) * 2;
	const xZoom = (pane.width - pad(pane.width)) / bounds.width;
	const yZoom = (pane.height - pad(pane.height)) / bounds.height;
	const zoom = Math.min(maxZoom, Math.max(minZoom, Math.min(xZoom, yZoom)));
	const cx = bounds.x + bounds.width / 2;
	const cy = bounds.y + bounds.height / 2;
	return { x: pane.width / 2 - cx * zoom, y: pane.height / 2 - cy * zoom, zoom };
}

export type FitFn = (bounds: Rect, width: number, height: number, minZoom: number, maxZoom: number, padding: number) => Viewport;

/** The frame the editor opens on: the fit when it is readable (zoom ≥
 * `floor`), else `floor` with the diagram's top-left `margin` px in from
 * the pane's top-left corner. `fits` says which. */
export function openViewport(
	bounds: Rect,
	pane: { width: number; height: number },
	opts: { padding?: number; floor?: number; minZoom?: number; maxZoom?: number; margin?: number; fit?: FitFn } = {}
): Viewport & { fits: boolean } {
	const { padding = 0.1, floor = READABLE_ZOOM, minZoom = 0.15, maxZoom = 2, margin = 16 } = opts;
	if (!(bounds.width > 0 && bounds.height > 0 && pane.width > 0 && pane.height > 0)) {
		return { x: 0, y: 0, zoom: 1, fits: true };
	}
	const fit = opts.fit
		? opts.fit(bounds, pane.width, pane.height, minZoom, maxZoom, padding)
		: fitViewport(bounds, pane, padding, minZoom, maxZoom);
	if (fit.zoom >= floor) return { ...fit, fits: true };
	return { x: margin - bounds.x * floor, y: margin - bounds.y * floor, zoom: floor, fits: false };
}

/** The pan that brings `target` (canvas coordinates) into the pane at the
 * current zoom, or null when it is already fully in view. Used after a
 * structural edit: the camera stays put unless the new element landed
 * off-screen, and then only pans (never re-zooms). */
export function revealViewport(vp: Viewport, target: Rect, pane: { width: number; height: number }, margin = 40): Viewport | null {
	const left = target.x * vp.zoom + vp.x;
	const top = target.y * vp.zoom + vp.y;
	const right = left + target.width * vp.zoom;
	const bottom = top + target.height * vp.zoom;
	if (left >= 0 && top >= 0 && right <= pane.width && bottom <= pane.height) return null;
	// Centre it when it fits; otherwise put its top-left in from the corner.
	const w = target.width * vp.zoom;
	const h = target.height * vp.zoom;
	const x = w + 2 * margin <= pane.width ? pane.width / 2 - (target.x + target.width / 2) * vp.zoom : margin - target.x * vp.zoom;
	const y = h + 2 * margin <= pane.height ? pane.height / 2 - (target.y + target.height / 2) * vp.zoom : margin - target.y * vp.zoom;
	return { x, y, zoom: vp.zoom };
}
