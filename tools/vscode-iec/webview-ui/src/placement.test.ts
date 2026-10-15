/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { BESIDE_GAP, bands, besideSpot, planPlacement, snapshot, type EdgeLike, type PlacedLike } from './placement.ts';

const n = (id: string, x: number, y: number, extra: Partial<PlacedLike> = {}): PlacedLike => ({
	id,
	kind: id.startsWith('v:') ? 'input' : id.startsWith('c:') ? 'coil' : 'block',
	label: id.replace(/^[a-z]+:(w\.|c\.)?/, ''),
	x,
	y,
	w: 60,
	h: 40,
	pinned: false,
	...extra
});

// P-101's network before the insert: two compares feeding a coil each.
const net: PlacedLike[] = [n('v:Level', 24, 40), n('b:w.LE', 120, 20), n('b:w.GE', 120, 80), n('c:Run', 240, 50)];
const netEdges: EdgeLike[] = [
	{ from: 'v:Level', to: 'b:w.LE' },
	{ from: 'v:Level', to: 'b:w.GE' },
	{ from: 'b:w.LE', to: 'c:Run' }
];

test('bands: connected components, one key per network', () => {
	const b = bands([...net, n('v:X', 0, 0)], netEdges);
	assert.equal(b.get('b:w.LE'), b.get('b:w.GE'));
	assert.notEqual(b.get('v:X'), b.get('b:w.LE'));
});

test('besideSpot: one column gap right of the right-most selected node, tops aligned', () => {
	assert.equal(besideSpot([]), undefined);
	assert.deepEqual(besideSpot([{ x: 100, y: 30, w: 60 }, { x: 10, y: 90, w: 40 }]), { x: 160 + BESIDE_GAP, y: 30 });
});

test('planPlacement: a new block lands at the asked spot, its chips keeping their arrangement', () => {
	const before = snapshot(net, netEdges);
	// Auto-layout put `both = AND(a, b)` in a new band at the bottom.
	const after = [...net, n('v:a', 24, 300), n('v:b', 24, 350), n('b:w.both', 120, 320)];
	const edges = [...netEdges, { from: 'v:a', to: 'b:w.both' }, { from: 'v:b', to: 'b:w.both' }];
	const entries = planPlacement(before, after, edges, { x: 400, y: 40 });
	assert.deepEqual(
		entries.sort((p, q) => p.node.localeCompare(q.node)),
		[
			{ node: 'b:w.both', x: 496, y: 60 },
			{ node: 'v:a', x: 400, y: 40 },
			{ node: 'v:b', x: 400, y: 90 }
		]
	);
});

test('planPlacement: no spot asked and nothing arranged → nothing pinned (pure auto-layout stays pure)', () => {
	const before = snapshot(net, netEdges);
	// Joining the network reorders it (LE/GE swap rows) — allowed: nobody
	// arranged it.
	const after = [n('v:Level', 24, 40), n('b:w.LE', 120, 80), n('b:w.GE', 120, 20), n('c:Run', 240, 50), n('b:w.both', 200, 140)];
	const edges = [...netEdges, { from: 'b:w.LE', to: 'b:w.both' }];
	assert.deepEqual(planPlacement(before, after, edges), []);
});

test('planPlacement: an ARRANGED network keeps every element where it was; the new block still lands at the spot', () => {
	// The user dragged c:Run: the network is arranged.
	const arranged = net.map((m) => (m.id === 'c:Run' ? { ...m, x: 300, pinned: true } : m));
	const before = snapshot(arranged, netEdges);
	// The insert joins the network; auto-layout swaps LE and GE.
	const after = [
		n('v:Level', 24, 40),
		n('b:w.LE', 120, 80),
		n('b:w.GE', 120, 20),
		{ ...arranged[3] },
		n('b:w.both', 200, 140)
	];
	const edges = [...netEdges, { from: 'b:w.LE', to: 'b:w.both' }, { from: 'b:w.GE', to: 'b:w.both' }];
	const entries = planPlacement(before, after, edges, { x: 420, y: 200 });
	const at = Object.fromEntries(entries.map((e) => [e.node, [e.x, e.y]]));
	assert.deepEqual(at['b:w.LE'], [120, 20], 'LE back on its row');
	assert.deepEqual(at['b:w.GE'], [120, 80], 'GE back on its row');
	assert.deepEqual(at['b:w.both'], [420, 200], 'the new block at the spot');
	assert.equal(at['v:Level'], undefined, 'an element that did not move is not pinned');
	assert.equal(at['c:Run'], undefined, 'an already-pinned element is left alone');
});

test('planPlacement: an arranged network elsewhere that shifted down is held; an unarranged one may move', () => {
	const other = [n('v:Q', 24, 400, { pinned: true }), n('c:Q2', 120, 400)];
	const loose = [n('v:R', 24, 500), n('c:R2', 120, 500)];
	const edges = [...netEdges, { from: 'v:Q', to: 'c:Q2' }, { from: 'v:R', to: 'c:R2' }];
	const before = snapshot([...net, ...other, ...loose], edges);
	const after = [...net, other[0], { ...other[1], y: 460 }, { ...loose[0], y: 560 }, { ...loose[1], y: 560 }, n('c:New', 200, 380)];
	const entries = planPlacement(before, after, edges);
	assert.deepEqual(entries, [{ node: 'c:Q2', x: 120, y: 400 }]);
});

test('planPlacement: a shifted ordinal id (k:N with another value) is new, never held at the old one\'s spot', () => {
	const k = (id: string, label: string, x: number, y: number): PlacedLike => ({ ...n(id, x, y), kind: 'input', label });
	const before = snapshot([k('k:1', '5', 24, 40, ), n('c:A', 120, 40, { pinned: true })], [{ from: 'k:1', to: 'c:A' }]);
	// The new statement's constant took k:1; the old one is now k:2.
	const after = [
		{ ...k('k:1', '10', 24, 200) },
		k('k:2', '5', 24, 60),
		n('c:A', 120, 40, { pinned: true }),
		n('c:B', 120, 200)
	];
	const edges = [{ from: 'k:2', to: 'c:A' }, { from: 'k:1', to: 'c:B' }];
	const entries = planPlacement(before, after, edges, { x: 300, y: 300 });
	const at = Object.fromEntries(entries.map((e) => [e.node, [e.x, e.y]]));
	assert.deepEqual(at['k:1'], [300, 300], 'the new constant goes with its statement');
	assert.deepEqual(at['c:B'], [396, 300]);
	assert.equal(at['k:2'], undefined, 'not mistaken for the old k:1');
});

test('planPlacement: comments and ghosts are never pinned by it', () => {
	const before = snapshot(net, netEdges);
	const after = [...net, n('cm:0', 24, 0, { kind: 'comment' }), n('g:in.X', 0, 0, { ghost: true })];
	assert.deepEqual(planPlacement(before, after, netEdges, { x: 1, y: 1 }), []);
});

test('planPlacement: a spot that is taken moves down until the new block is clear of everything', () => {
	const before = snapshot(net, netEdges);
	const after = [...net, n('b:w.both', 200, 300)];
	// Asked for right on top of GE (120,80 60×40).
	const [e] = planPlacement(before, after, netEdges, { x: 130, y: 70 });
	assert.equal(e.node, 'b:w.both');
	assert.equal(e.x, 130, 'same column');
	assert.ok(e.y >= 80 + 40 + 10, `below GE with clearance, got y=${e.y}`);
	assert.equal((e.y - 70) % 24, 0, 'in whole steps');
});

test('planPlacement: joining an ARRANGED network with no spot asked: pinned at its auto place, clear of the held elements', () => {
	const arranged = net.map((m) => (m.id === 'c:Run' ? { ...m, pinned: true } : m));
	const before = snapshot(arranged, netEdges);
	// Auto-layout gave `both` the column the coil had, and pushed the coil;
	// the coil is pinned, so it stays — `both` must not land on it.
	const after = [...arranged, n('b:w.both', 240, 50)];
	const edges = [...netEdges, { from: 'b:w.LE', to: 'b:w.both' }];
	const entries = planPlacement(before, after, edges);
	const both = entries.find((e) => e.node === 'b:w.both')!;
	assert.ok(both, 'the newcomer is pinned');
	assert.equal(both.x, 240);
	assert.ok(both.y >= 50 + 40 + 10, `below the pinned coil, got ${both.y}`);
});

test('planPlacement: open-pin chips are told apart by what they feed, not their look-alike ids', () => {
	// lic's two `_` chips were v:_ and v:_#2; `either = OR(_, _)` went in
	// AHEAD of lic, so its chips took those ids and lic's became v:_#3/#4.
	const chip = (id: string, x: number, y: number): PlacedLike => ({ ...n(id, x, y), kind: 'input', label: '_' });
	const beforeNodes = [chip('v:_', 24, 40), chip('v:_#2', 24, 80), n('f:lic', 120, 40)];
	const beforeEdges = [
		{ from: 'v:_', to: 'f:lic', toPin: 'PV' },
		{ from: 'v:_#2', to: 'f:lic', toPin: 'SP' }
	];
	const before = snapshot(beforeNodes, beforeEdges);
	const after = [chip('v:_', 24, 300), chip('v:_#2', 24, 340), n('b:w.either', 120, 310), chip('v:_#3', 24, 40), chip('v:_#4', 24, 80), n('f:lic', 120, 40)];
	const edges = [
		{ from: 'v:_', to: 'b:w.either', toPin: 'IN1' },
		{ from: 'v:_#2', to: 'b:w.either', toPin: 'IN2' },
		{ from: 'v:_#3', to: 'f:lic', toPin: 'PV' },
		{ from: 'v:_#4', to: 'f:lic', toPin: 'SP' }
	];
	const at = Object.fromEntries(planPlacement(before, after, edges, { x: 500, y: 500 }).map((e) => [e.node, [e.x, e.y]]));
	assert.deepEqual(at['v:_'], [500, 500], "either's first chip goes with it");
	assert.deepEqual(at['v:_#2'], [500, 540]);
	assert.deepEqual(at['b:w.either'], [596, 510]);
	assert.equal(at['v:_#3'], undefined, "lic's chips stay");
});
