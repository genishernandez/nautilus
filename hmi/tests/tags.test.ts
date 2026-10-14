// Packing a screen's tag list into the handful of patterns one subscription
// accepts.
//
// The property that matters is the SUPERSET one, and it is the only kind of
// bug here that does not announce itself: a pattern set that has quietly
// dropped a tag renders a live instrument as "—" forever, on a screen that
// otherwise looks healthy. Every spec below either checks that property
// directly or checks a rule that exists to protect it, and the fleet fixture
// at the bottom runs it over a synthetic fleet the size of a real central
// host's `/system` subscription (217 top-level tags) — the scale that forced
// the cap problem in the first place.
import { describe, it, expect } from './harness.js';
import {
	MAX_TAG_PATTERNS,
	NO_TAGS,
	mergeTagPatterns,
	packTagPatterns,
	tagInPatterns,
	tagPatternMatches
} from '../src/lib/tags.js';

describe('tagPatternMatches — path.Match semantics', () => {
	it('an exact name matches only itself', () => {
		expect(tagPatternMatches('RTU9_ZONE15_FIT_001', 'RTU9_ZONE15_FIT_001')).toBe(true);
		expect(tagPatternMatches('RTU9_ZONE15_FIT_001', 'RTU9_ZONE15_FIT_002')).toBe(false);
	});

	it('? is exactly one character', () => {
		expect(tagPatternMatches('RTU9_ZONE15_FIT_00?', 'RTU9_ZONE15_FIT_001')).toBe(true);
		expect(tagPatternMatches('RTU9_ZONE15_FIT_00?', 'RTU9_ZONE15_FIT_0012')).toBe(false);
		expect(tagPatternMatches('RTU9_ZONE15_FIT_00?', 'RTU9_ZONE15_FIT_00')).toBe(false);
	});

	it('* spans the rest of a dotted name — a tag path has no separator in it', () => {
		expect(tagPatternMatches('RTU32_*', 'RTU32_BST9_PMP_09A')).toBe(true);
		expect(tagPatternMatches('*', 'anything.at.all')).toBe(true);
		expect(tagPatternMatches('Tank*', 'Tank101.Level')).toBe(true);
	});

	it('a dot is an ordinary character, not a wildcard', () => {
		expect(tagPatternMatches('AB.C', 'ABxC')).toBe(false);
		expect(tagPatternMatches('AB.C', 'AB.C')).toBe(true);
	});

	it('an empty pattern list is the unfiltered stream', () => {
		expect(tagInPatterns([], 'RTU9_ZONE15_FIT_001')).toBe(true);
		expect(tagInPatterns(['RTU9_*'], 'RTU32_BST9_PMP_09A')).toBe(false);
		expect(tagInPatterns(['RTU9_*', 'RTU32_*'], 'RTU32_BST9_PMP_09A')).toBe(true);
	});

	it('NO_TAGS matches nothing a controller would ever publish', () => {
		for (const n of ['RTU9_ZONE15_FIT_001', 'PLT1_TNK6_FIT_001', 'ClockRate']) {
			expect(tagPatternMatches(NO_TAGS, n)).toBe(false);
		}
	});
});

describe('mergeTagPatterns — every merge widens', () => {
	it('same length, differing positions become ?', () => {
		expect(mergeTagPatterns('RTU9_ZONE15_FIT_001', 'RTU9_ZONE15_FIT_002')).toBe(
			'RTU9_ZONE15_FIT_00?'
		);
	});

	it('different lengths fall back to the common literal prefix plus *', () => {
		expect(mergeTagPatterns('RTU32_BST9_PMP_09A', 'RTU32_TNK12_LIT_001')).toBe('RTU32_*');
		expect(mergeTagPatterns('RTU32_BST9', 'RTU32_BST9_FIT_001')).toBe('RTU32_BST9*');
	});

	it('a wildcard in either operand ends the literal prefix', () => {
		// The trap this rule exists for: `abc?` and `abcd` are the same
		// length, but a per-character merge would produce `abc?`, which does
		// NOT match everything `abc*` matches.
		expect(mergeTagPatterns('abc*', 'abcd')).toBe('abc*');
		expect(mergeTagPatterns('ab?d', 'abcde')).toBe('ab*');
	});

	it('merging a pattern with itself changes nothing', () => {
		expect(mergeTagPatterns('RTU9_*', 'RTU9_*')).toBe('RTU9_*');
	});
});

describe('packTagPatterns', () => {
	it('returns fewer names than the cap verbatim — exact names, exact frame', () => {
		const names = ['B', 'A', 'C'];
		expect(packTagPatterns(names, { max: 10 })).toEqual(['A', 'B', 'C']);
	});

	it('dedupes and drops empties', () => {
		expect(packTagPatterns(['A', 'A', '', 'B'], { max: 10 })).toEqual(['A', 'B']);
	});

	it('an empty list means NO tags, not every tag', () => {
		// `tags: []` is the unfiltered stream to RealtimeOptions, so a caller
		// with nothing to draw must not be handed one.
		expect(packTagPatterns([], { max: 10 })).toEqual([NO_TAGS]);
	});

	it('packs to at most `max` patterns', () => {
		const names = Array.from({ length: 200 }, (_, i) => `RTU${i}_WEL${i}_FIT_001`);
		expect(packTagPatterns(names, { max: 5 }).length <= 5).toBe(true);
		expect(packTagPatterns(names).length <= MAX_TAG_PATTERNS).toBe(true);
	});

	it('prefers a ? merge over a * merge — it drags in far less plant', () => {
		// Two families of four. A per-character merge collapses each family
		// into one `?` pattern and never has to truncate anything.
		const names = [
			'RTU9_ZONE15_FIT_001',
			'RTU9_ZONE15_FIT_002',
			'RTU9_ZONE15_FIT_003',
			'RTU9_ZONE15_FIT_004',
			'RTU32_BST9_PMP_09A',
			'RTU32_BST9_PMP_09B',
			'RTU32_BST9_PMP_09C',
			'RTU32_BST9_PMP_09D'
		];
		expect(packTagPatterns(names, { max: 2 })).toEqual([
			'RTU32_BST9_PMP_09?',
			'RTU9_ZONE15_FIT_00?'
		]);
	});
});

// A SYNTHETIC fleet, shaped like a real central host's `/system`
// subscription: about as many top-level names (217) against the same
// 40-pattern cap, in the same kind of structure — a few regular families of
// one RTU per site with a couple of instruments each, area numbering that
// does not line up with the RTU numbering, split sites with lettered RTUs,
// a plant with its own prefix, and a handful of one-off names of their own
// lengths. None of it comes from a real plant. It is here because the
// property under test only breaks at scale — with a handful of names nothing
// is ever merged.
function syntheticFleet(): string[] {
	const names: string[] = [];
	const pad = (n: number, w = 3) => String(n).padStart(w, '0');
	let rtu = 1;
	// 40 zone sites: a flow meter and a pump each.
	for (let i = 0; i < 40; i++) {
		const site = `RTU${rtu++}`;
		const zone = 3 + ((i * 7) % 41);
		names.push(`${site}_ZONE${zone}_FIT_001`, `${site}_ZONE${zone}_PMP_${pad(zone)}`);
	}
	// 14 booster stations: two to four lettered pumps and a station meter.
	for (let i = 0; i < 14; i++) {
		const site = `RTU${rtu++}`;
		const b = i + 1;
		for (const letter of 'ABCD'.slice(0, 2 + (i % 3))) {
			names.push(`${site}_BST${b}_PMP_${pad(b, 2)}${letter}`);
		}
		names.push(`${site}_BST${b}_FIT_001`);
	}
	// 16 storage tanks, every third with a second level transmitter.
	for (let i = 0; i < 16; i++) {
		const site = `RTU${rtu++}`;
		names.push(`${site}_TNK${i + 1}_LIT_001`);
		if (i % 3 === 0) names.push(`${site}_TNK${i + 1}_LIT_002`);
	}
	// 12 pressure-reducing vaults, upstream and downstream pressure.
	for (let i = 0; i < 12; i++) {
		const site = `RTU${rtu++}`;
		names.push(`${site}_PRV${i + 1}_PIT_001`, `${site}_PRV${i + 1}_PIT_002`);
	}
	// 6 transfer points split across two lettered RTUs.
	for (let i = 0; i < 6; i++) {
		const n = rtu++;
		names.push(`RTU${n}A_XFR${i + 2}_FIT_001`, `RTU${n}B_XFR${i + 2}_GLV_001`);
	}
	// The plant, on its own prefix and its own naming.
	for (let i = 1; i <= 3; i++) names.push(`PLT1_RAW_FIT_${pad(i)}`);
	names.push('PLT1_RAW_AIT_001_TURB');
	for (let i = 1; i <= 8; i++) names.push(`PLT1_FLT${i}_LIT_001`);
	names.push('PLT1_CLW_AIT_001_CL2', 'PLT1_CLW_AIT_002_PH', 'PLT1_CLW_LIT_001');
	for (let i = 1; i <= 4; i++) names.push(`PLT1_HLP_PMP_0${i}`);
	// One-offs that belong to no family.
	names.push('SysHeartbeat', 'ScadaMode', 'CommsWatchdog', 'RTU99_SPARE_DI_01', 'FleetAlarmCount');
	return names;
}
const SYNTHETIC_FLEET = syntheticFleet();

describe('packTagPatterns — a fleet-scale /system subscription', () => {
	it('is the size of the subscription it stands in for', () => {
		expect(SYNTHETIC_FLEET.length).toBe(217);
		expect(new Set(SYNTHETIC_FLEET).size).toBe(217);
	});

	it('fits the controller cap', () => {
		const pats = packTagPatterns(SYNTHETIC_FLEET);
		expect(pats.length <= MAX_TAG_PATTERNS).toBe(true);
	});

	it('still matches every tag the screen draws', () => {
		// THE property. A dropped tag is a dead-looking instrument, and
		// nothing else in the stack would notice.
		const pats = packTagPatterns(SYNTHETIC_FLEET);
		const missed = SYNTHETIC_FLEET.filter((n) => !tagInPatterns(pats, n));
		expect(missed).toEqual([]);
	});

	it('holds the property at every cap, down to one pattern', () => {
		for (const max of [1, 2, 5, 10, 20, 40, 80]) {
			const pats = packTagPatterns(SYNTHETIC_FLEET, { max });
			expect(pats.length <= max).toBe(true);
			expect(SYNTHETIC_FLEET.filter((n) => !tagInPatterns(pats, n))).toEqual([]);
		}
	});

	it('does not reach for * while a ? merge is available', () => {
		// At the real cap the greedy loop can still collapse whole families
		// character by character, so most of the set stays narrow. This is
		// what separates a 200 kB subscription from a 500 kB one; if it ever
		// regresses, the packer got the cost function wrong.
		const pats = packTagPatterns(SYNTHETIC_FLEET);
		const wide = pats.filter((p) => p.includes('*'));
		expect(wide.length <= 4).toBe(true);
	});
});
