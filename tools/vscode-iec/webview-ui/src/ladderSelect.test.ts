/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { afterWrapOr, deleteOrder, inSel, orOp, selPaths, seriesKey, toggleSel, type LdSel } from './ladderSelect.ts';
import { layoutRung } from './ladderLayout.ts';
import type { Ann } from './ladder.ts';

test('toggleSel: Ctrl-click grows a selection within one series, shrinks it on a second click', () => {
	let sel: LdSel = { rung: 'r', path: [0] };
	sel = toggleSel(sel, 'r', [1]);
	sel = toggleSel(sel, 'r', [2]);
	assert.deepEqual(selPaths(sel), [[2], [1], [0]]);
	assert.ok(inSel(sel, 'r', [0]) && inSel(sel, 'r', [1]) && inSel(sel, 'r', [2]));
	sel = toggleSel(sel, 'r', [1]);
	assert.ok(!inSel(sel, 'r', [1]));
	assert.equal(selPaths(sel).length, 2);
	// Down to nothing: no selection.
	sel = toggleSel(toggleSel(sel, 'r', [0]), 'r', [2]);
	assert.equal(sel, null);
});

test('toggleSel: another series, another rung or a coil starts afresh', () => {
	const sel: LdSel = { rung: 'r', path: [0], more: [[1]] };
	assert.deepEqual(toggleSel(sel, 'r', [2, 0, 0]), { rung: 'r', path: [2, 0, 0] });
	assert.deepEqual(toggleSel(sel, 'q', [1]), { rung: 'q', path: [1] });
	assert.deepEqual(toggleSel(sel, 'r', undefined, 0), { rung: 'r', coil: 0 });
	assert.deepEqual(toggleSel({ rung: 'r', whole: true }, 'r', [1]), { rung: 'r', path: [1] });
	// Within one leg of a branch the rule is the same.
	const leg = toggleSel({ rung: 'r', path: [2, 1, 0] }, 'r', [2, 1, 1]);
	assert.deepEqual(selPaths(leg), [[2, 1, 1], [2, 1, 0]]);
});

test('orOp: N selected elements → ONE wrapOr in series order; a branch → addLeg; one element → wrapBranch', () => {
	const kinds: Record<string, string> = { '0': 'contact', '1': 'contact', '2': 'contact', '3': 'branch' };
	const kindAt = (p: number[]) => kinds[p.join('.')];
	assert.deepEqual(orOp({ rung: 'r', path: [2], more: [[0], [1]] }, kindAt), {
		type: 'wrapOr',
		rung: 'r',
		paths: [[0], [1], [2]]
	});
	assert.deepEqual(orOp({ rung: 'r', path: [3] }, kindAt), { type: 'addLeg', rung: 'r', path: [3] });
	assert.deepEqual(orOp({ rung: 'r', path: [1] }, kindAt), { type: 'wrapBranch', rung: 'r', path: [1] });
	assert.equal(orOp({ rung: 'r', coil: 0 }, kindAt), null);
	assert.equal(orOp({ rung: 'r', whole: true }, kindAt), null);
	assert.equal(orOp(null, kindAt), null);
});

test('afterWrapOr: the new branch (at the first selected index) becomes the selection', () => {
	assert.deepEqual(afterWrapOr({ rung: 'r', path: [3], more: [[1], [2]] }), { rung: 'r', path: [1] });
	assert.deepEqual(afterWrapOr({ rung: 'r', path: [0, 1, 2], more: [[0, 1, 4]] }), { rung: 'r', path: [0, 1, 2] });
});

test('deleteOrder: highest index first, so no delete renumbers a later one', () => {
	assert.deepEqual(deleteOrder([[0], [2], [1]]), [[2], [1], [0]]);
});

const c = (ref: string): Ann => ({ el: { kind: 'contact', ref } });
const branch = (...legs: Ann[][]): Ann => ({
	el: { kind: 'branch', legs: legs.map((l) => l.map((a) => a.el)) },
	legs
});

test('layoutRung: a branch reports its own box — every leg node inside it, its path, one centerline per leg', () => {
	const br = branch([c('A'), c('B')], [c('C')], [c('D')]);
	const lay = layoutRung([c('X'), br], [], 800);
	assert.equal(lay.branches.length, 1);
	const b = lay.branches[0];
	assert.deepEqual(b.path, [1]);
	assert.equal(b.legYs.length, 3);
	const legNodes = lay.nodes.filter((n) => n.path && n.path.length > 1);
	assert.equal(legNodes.length, 4);
	for (const n of legNodes) {
		assert.ok(n.x >= b.x && n.x + n.w <= b.x + b.w, `${n.ann.el.ref} inside the branch horizontally`);
		assert.ok(n.y >= b.y && n.y + n.h <= b.y + b.h, `${n.ann.el.ref} inside the branch vertically`);
		assert.equal(seriesKey(n.path).startsWith('1.'), true, 'grouped under the branch');
	}
	// X sits outside the branch box.
	const x = lay.nodes.find((n) => n.ann.el.ref === 'X')!;
	assert.ok(x.x + x.w <= b.x, 'X is left of the branch');
	assert.equal(seriesKey(x.path), '');
});

test('layoutRung: nested branches each get a box, the inner one inside the outer', () => {
	const inner = branch([c('A')], [c('B')]);
	const outer = branch([inner], [c('C')]);
	const lay = layoutRung([outer], [], 800);
	assert.equal(lay.branches.length, 2);
	const o = lay.branches.find((b) => b.path.length === 1)!;
	const i = lay.branches.find((b) => b.path.length === 3)!;
	assert.deepEqual(i.path, [0, 0, 0]);
	assert.equal(seriesKey(i.path), '0.0');
	assert.ok(i.x >= o.x && i.x + i.w <= o.x + o.w && i.y >= o.y && i.y + i.h <= o.y + o.h);
});
