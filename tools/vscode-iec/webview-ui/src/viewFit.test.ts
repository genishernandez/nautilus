/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { READABLE_ZOOM, fitViewport, openViewport, revealViewport } from './viewFit.ts';

const pane = { width: 1000, height: 700 };

test('READABLE_ZOOM keeps 12 px titles at ≥ 10.5 px and 7 px pin handles at ≥ 6 px', () => {
	assert.ok(12 * READABLE_ZOOM >= 10.5);
	assert.ok(7 * READABLE_ZOOM >= 6);
});

test('openViewport: a diagram that fits readably opens fitted (the old behaviour)', () => {
	const b = { x: 10, y: 10, width: 600, height: 400 };
	const v = openViewport(b, pane);
	assert.equal(v.fits, true);
	assert.deepEqual({ x: v.x, y: v.y, zoom: v.zoom }, fitViewport(b, pane));
	assert.ok(v.zoom >= READABLE_ZOOM);
});

test('openViewport: a big program opens at the readable floor on its top-left, not shrunk to ~0.5', () => {
	// ex01's lift station: ~2000 × 1600 canvas units.
	const b = { x: 10, y: 4, width: 2000, height: 1600 };
	assert.ok(fitViewport(b, pane).zoom < 0.5, 'the plain fit would be unreadable');
	const v = openViewport(b, pane);
	assert.equal(v.fits, false);
	assert.equal(v.zoom, READABLE_ZOOM);
	// The diagram's top-left sits 16 px in from the pane's corner.
	assert.equal(b.x * v.zoom + v.x, 16);
	assert.equal(b.y * v.zoom + v.y, 16);
});

test('openViewport: the floor is a parameter; an injected fit function is used as-is', () => {
	const b = { x: 0, y: 0, width: 2000, height: 1600 };
	assert.equal(openViewport(b, pane, { floor: 0.3 }).fits, true);
	const v = openViewport(b, pane, { fit: () => ({ x: 1, y: 2, zoom: 1.5 }) });
	assert.deepEqual(v, { x: 1, y: 2, zoom: 1.5, fits: true });
});

test('openViewport: an empty or unmeasured diagram gets the identity view', () => {
	assert.deepEqual(openViewport({ x: 0, y: 0, width: 0, height: 0 }, pane), { x: 0, y: 0, zoom: 1, fits: true });
});

test('revealViewport: in view → no pan; off-screen → pan (same zoom) to centre it', () => {
	const vp = { x: 0, y: 0, zoom: 1 };
	assert.equal(revealViewport(vp, { x: 100, y: 100, width: 80, height: 40 }, pane), null);
	const v = revealViewport(vp, { x: 100, y: 1500, width: 80, height: 40 }, pane)!;
	assert.equal(v.zoom, 1);
	const top = 1500 * v.zoom + v.y;
	assert.ok(top >= 0 && top + 40 <= pane.height, 'now inside the pane');
});
