// Where a palette insert lands, and what it may move — pure (App.svelte
// feeds it the layout before and after; #78).
//
// The FBD layout model: positions are auto-layout from topology, except
// nodes the user dragged, which are pinned in the file's (* @layout *)
// block. Two things made "+ add" surprising:
//
// 1. The new statement landed wherever auto-layout put it — a new band at
//    the bottom of the diagram (often off-screen), or wherever its
//    network's columns had room.
// 2. Joining a network re-ran that network's auto-layout: the new block
//    took part in the barycenter ordering, and existing blocks swapped
//    rows (in P-101, LE and GE).
//
// So after the insert's model arrives, ONE setLayout op pins:
// - every element of an ARRANGED network (a band with at least one pinned
//   node — the user has laid it out by hand) that the insert would move,
//   back where it was. A network nobody ever arranged stays fully
//   auto-laid-out: it re-flows around the new element, which is what
//   auto-layout is for, and nothing gets pinned to keep it still.
// - the new elements, as a group (their auto-layout arrangement kept),
//   with the group's top-left at the place the user asked for: the
//   canvas point they double-clicked, or just right of the element they
//   had selected. Asked for nothing, auto-layout places them, as before —
//   unless they joined an arranged network, whose elements no longer
//   make room for them: then they are pinned where auto-layout put them.
//   Either way the group never lands ON another element: it moves down
//   until it is clear.

export type PlacedLike = {
	id: string;
	kind: string;
	label: string;
	x: number;
	y: number;
	w: number;
	h: number;
	ghost?: boolean;
	/** Pinned in the @layout block (the model carried x/y for it). */
	pinned: boolean;
};
export type EdgeLike = { from: string; to: string; toPin?: string };
export type Entry = { node: string; x: number; y: number };

/** What the editor knew just before the insert: every node as rendered,
 * and which network band (connected component) each sat in. */
export type Snapshot = {
	nodes: Map<string, { x: number; y: number; label: string; kind: string; pinned: boolean; feeds: string[] }>;
	band: Map<string, string>;
};

/** The pins each node's output feeds, as `to|toPin`. */
function feedsOf(edges: EdgeLike[]): Map<string, string[]> {
	const m = new Map<string, string[]>();
	for (const e of edges) (m.get(e.from) ?? m.set(e.from, []).get(e.from)!).push(`${e.to}|${e.toPin ?? ''}`);
	return m;
}

/** Gap between a selected element and a block placed beside it — the
 * auto-layout's own column gap. */
export const BESIDE_GAP = 72;
/** Clearance the new group keeps from other elements, and the step it
 * moves down by to find it. */
const CLEAR = 10;
const STEP = 24;

type Box = { x: number; y: number; w: number; h: number };
const hits = (a: Box, b: Box) =>
	a.x < b.x + b.w + CLEAR && b.x < a.x + a.w + CLEAR && a.y < b.y + b.h + CLEAR && b.y < a.y + a.h + CLEAR;

/** The first top-left at or below `at` where a `size` box touches none of
 * `obstacles` (searching down a column, as a person would make room). */
export function clearOf(at: { x: number; y: number }, size: { w: number; h: number }, obstacles: Box[]): { x: number; y: number } {
	for (let i = 0; i < 400; i++) {
		const box = { x: at.x, y: at.y + i * STEP, ...size };
		if (!obstacles.some((o) => hits(box, o))) return { x: at.x, y: box.y };
	}
	return at;
}

/** Connected components (the editor's network bands), as id → band key. */
export function bands(nodes: { id: string }[], edges: EdgeLike[]): Map<string, string> {
	const parent = new Map<string, string>(nodes.map((n) => [n.id, n.id]));
	const find = (x: string): string => {
		let r = x;
		while (parent.get(r) !== r) r = parent.get(r)!;
		return r;
	};
	for (const e of edges) {
		if (!parent.has(e.from) || !parent.has(e.to)) continue;
		parent.set(find(e.from), find(e.to));
	}
	return new Map(nodes.map((n) => [n.id, find(n.id)]));
}

export function snapshot(placed: PlacedLike[], edges: EdgeLike[]): Snapshot {
	const feeds = feedsOf(edges);
	return {
		nodes: new Map(placed.map((n) => [n.id, { x: n.x, y: n.y, label: n.label, kind: n.kind, pinned: n.pinned, feeds: feeds.get(n.id) ?? [] }])),
		band: bands(placed, edges)
	};
}

/** The spot beside a selected node: one column gap to its right, tops
 * aligned. With several selected, beside the right-most. */
export function besideSpot(sel: { x: number; y: number; w: number }[]): { x: number; y: number } | undefined {
	if (!sel.length) return undefined;
	const r = sel.reduce((a, b) => (b.x + b.w > a.x + a.w ? b : a));
	return { x: Math.round(r.x + r.w + BESIDE_GAP), y: Math.round(r.y) };
}

// Comments carry ordinal ids (cm:N) and never band; ghosts are layout-only.
const placeable = (n: PlacedLike) => n.kind !== 'comment' && !n.ghost;
// A statement's own element (named by the statement, so its id is stable):
// a wire's block, an FB instance, a coil.
const statementRoot = (id: string) => /^(b:w\.|f:|c:)/.test(id);

/**
 * The setLayout entries that make an insert land where it was asked and
 * leave arranged networks where they were. `before`: the snapshot taken
 * as the insert was posted; `after`: the auto-layout of the model that
 * came back (pins applied); `spot`: where the user asked for it, if they
 * did. Empty: nothing to pin (post nothing).
 */
export function planPlacement(before: Snapshot, after: PlacedLike[], afterEdges: EdgeLike[], spot?: { x: number; y: number }): Entry[] {
	// The same id with another label/kind is another element — a constant
	// (k:N) or a repeated chip (v:X#N, an open pin's v:_#N) whose ordinal
	// shifted when the statement went in ahead of it — so it counts as new,
	// and is never "kept" at the old one's position. Chips look alike (every
	// open pin is `_`), so a chip is also the same one only if it still
	// feeds every pin it fed (a shared tag chip may gain a consumer).
	const afterFeeds = feedsOf(afterEdges);
	const same = (n: PlacedLike) => {
		const b = before.nodes.get(n.id);
		if (!b || b.label !== n.label || b.kind !== n.kind) return false;
		if (n.kind !== 'input') return true;
		const now = new Set(afterFeeds.get(n.id) ?? []);
		return b.feeds.every((f) => now.has(f));
	};
	const fresh = after.filter((n) => placeable(n) && !same(n));
	const entries: Entry[] = [];
	// Arranged bands: the bands (before the insert) holding a pinned node.
	const arranged = new Set<string>();
	for (const [id, b] of before.nodes) if (b.pinned) arranged.add(before.band.get(id)!);
	// Where every existing element ends up: held where it was (arranged
	// bands), pinned, or auto.
	const final = new Map<string, Box>();
	for (const n of after) {
		if (!same(n)) continue;
		final.set(n.id, { x: n.x, y: n.y, w: n.w, h: n.h });
		if (!placeable(n) || n.pinned) continue;
		const was = before.nodes.get(n.id)!;
		if (!arranged.has(before.band.get(n.id)!)) continue;
		if (Math.abs(was.x - n.x) < 0.5 && Math.abs(was.y - n.y) < 0.5) continue;
		entries.push({ node: n.id, x: Math.round(was.x), y: Math.round(was.y) });
		final.set(n.id, { x: was.x, y: was.y, w: n.w, h: n.h });
	}
	if (!fresh.length) return entries;
	// The group is the new statement's own elements plus every new element
	// in their band(s) — its chips and constants. A new id in some other
	// band (an ordinal that shifted) is not part of it.
	const band = bands(after, afterEdges);
	const roots = fresh.filter((n) => statementRoot(n.id));
	const rootBands = new Set(roots.map((n) => band.get(n.id)));
	const group = (roots.length ? fresh.filter((n) => rootBands.has(band.get(n.id))) : fresh).filter((n) => !n.pinned);
	if (!group.length) return entries;
	// Joined an arranged network: its elements hold still, so they no
	// longer make room — the group is placed (and pinned) like an asked-for
	// one, at its auto position.
	const groupIds = new Set(group.map((n) => n.id));
	const joinsArranged = after.some(
		(n) => same(n) && !groupIds.has(n.id) && rootBands.has(band.get(n.id)) && arranged.has(before.band.get(n.id)!)
	);
	if (!spot && !joinsArranged) return entries;
	const minX = Math.min(...group.map((n) => n.x));
	const minY = Math.min(...group.map((n) => n.y));
	const size = {
		w: Math.max(...group.map((n) => n.x + n.w)) - minX,
		h: Math.max(...group.map((n) => n.y + n.h)) - minY
	};
	const others = after.filter((n) => !groupIds.has(n.id) && !n.ghost).map((n) => final.get(n.id) ?? n);
	const at = clearOf(spot ?? { x: minX, y: minY }, size, others);
	for (const n of group) entries.push({ node: n.id, x: Math.round(at.x + n.x - minX), y: Math.round(at.y + n.y - minY) });
	return entries;
}
