// Ladder selection rules, pure (no DOM): which elements may be selected
// together, and what the OR gesture posts for a selection. LadderView
// owns the state; these decide it, so the rules are unit-tested.
//
// A multi-selection is a set of elements of ONE series of one rung (the
// rung root, or one leg of a branch): the only set that ORs into one
// branch — `A B C` with all three selected → `[ A | B | C ]`.

export type LdSel = {
	rung: string;
	/** The primary element (the last one clicked). */
	path?: number[];
	coil?: number;
	/** The rung itself (its name clicked). */
	whole?: boolean;
	/** Further elements of the primary's series (Ctrl/Shift-click). */
	more?: number[][];
} | null;

const same = (a: number[] | undefined, b: number[] | undefined) =>
	!!a && !!b && a.length === b.length && a.every((v, i) => v === b[i]);

/** The series an element sits in: its path minus its own index. */
export const seriesOf = (path: number[]) => path.slice(0, -1);

/** Every element path the selection holds, primary first. */
export function selPaths(sel: LdSel): number[][] {
	if (!sel || sel.whole || sel.coil !== undefined || !sel.path) return [];
	return [sel.path, ...(sel.more ?? [])];
}

/** Is the element at (rung, path) / coil part of the selection? */
export function inSel(sel: LdSel, rung: string, path?: number[], coil?: number): boolean {
	if (!sel || sel.whole || sel.rung !== rung) return false;
	if (coil !== undefined || sel.coil !== undefined) return sel.coil === coil && coil !== undefined;
	return selPaths(sel).some((p) => same(p, path));
}

/** A Ctrl/Shift-click on an element: toggles it in the selection when it
 * shares the selection's series; anything else (another rung, another
 * series, a coil, a whole-rung selection) starts a fresh selection. */
export function toggleSel(sel: LdSel, rung: string, path?: number[], coil?: number): LdSel {
	const fresh: LdSel = coil !== undefined ? { rung, coil } : { rung, path };
	if (coil !== undefined || !path || !sel || sel.whole || sel.coil !== undefined || !sel.path || sel.rung !== rung) return fresh;
	if (!same(seriesOf(sel.path), seriesOf(path))) return fresh;
	const all = selPaths(sel);
	const has = all.some((p) => same(p, path));
	const next = has ? all.filter((p) => !same(p, path)) : [...all, path];
	if (!next.length) return null;
	// The clicked element becomes primary when added; on removal the most
	// recent remaining one does.
	const primary = has ? next[next.length - 1] : path;
	return { rung, path: primary, more: next.filter((p) => !same(p, primary)) };
}

/** The OR gesture (the `O` key, the toolbar's OR button) for a selection:
 * - two or more elements → `wrapOr`: one branch, one leg per element;
 * - a branch → `addLeg`: one more OR leg (a `_` placeholder contact);
 * - one other element → `wrapBranch`: a 2-leg OR around it.
 * `kindAt` says what kind of element a path holds. Null: nothing to OR. */
export function orOp(sel: LdSel, kindAt: (path: number[]) => string | undefined): Record<string, unknown> | null {
	const paths = selPaths(sel);
	if (!sel || !paths.length) return null;
	if (paths.length >= 2) {
		const sorted = [...paths].sort((a, b) => a[a.length - 1] - b[b.length - 1]);
		return { type: 'wrapOr', rung: sel.rung, paths: sorted };
	}
	if (kindAt(paths[0]) === 'branch') return { type: 'addLeg', rung: sel.rung, path: paths[0] };
	return { type: 'wrapBranch', rung: sel.rung, path: paths[0] };
}

/** Where the selection lands after `wrapOr`: the new branch sits at the
 * first selected element's index, so selecting it there lets a second
 * `O` add a leg straight away. */
export function afterWrapOr(sel: LdSel): LdSel {
	const paths = selPaths(sel);
	if (!sel || paths.length < 2) return sel;
	const first = Math.min(...paths.map((p) => p[p.length - 1]));
	return { rung: sel.rung, path: [...seriesOf(paths[0]), first] };
}

/** Deletes for a multi-selection, highest index first: each op resolves
 * against the text the previous one left, and removing a later element
 * never renumbers an earlier one. */
export function deleteOrder(paths: number[][]): number[][] {
	return [...paths].sort((a, b) => b[b.length - 1] - a[a.length - 1]);
}

/** The series a node or branch is drawn in, as a key ('' = the rung root,
 * '1.0' = leg 0 of the branch at index 1). The view nests each branch's
 * legs inside the branch's own <g>, by this key. */
export const seriesKey = (path: number[] | undefined) => (path ? seriesOf(path).join('.') : '');
