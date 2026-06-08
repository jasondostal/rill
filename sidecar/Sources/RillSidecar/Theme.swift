// Theme.swift — Rill's OKLCH theme engine, ported from frontend/src/lib/theme.js.
// Every color derives from a handful of knobs. Same knobs, same math as the web
// app, so a theme set in either place renders identically. Knobs are Codable to
// round-trip through rill's /api/settings (key: appearance.theme).

import SwiftUI

// MARK: - OKLCH → sRGB

/// Build a SwiftUI Color from oklch(L C H / a). L in 0…1, C ≥ 0, H in degrees.
func oklch(_ l: Double, _ c: Double, _ h: Double, _ a: Double = 1) -> Color {
	let L = max(0, min(1, l))
	let C = max(0, c)
	let H = ((h.truncatingRemainder(dividingBy: 360)) + 360).truncatingRemainder(dividingBy: 360)
	let hr = H * .pi / 180
	let aLab = C * cos(hr)
	let bLab = C * sin(hr)

	// OKLab → linear sRGB (Ottosson).
	let l_ = L + 0.3963377774 * aLab + 0.2158037573 * bLab
	let m_ = L - 0.1055613458 * aLab - 0.0638541728 * bLab
	let s_ = L - 0.0894841775 * aLab - 1.2914855480 * bLab
	let lc = l_ * l_ * l_, mc = m_ * m_ * m_, sc = s_ * s_ * s_
	let r =  4.0767416621 * lc - 3.3077115913 * mc + 0.2309699292 * sc
	let g = -1.2684380046 * lc + 2.6097574011 * mc - 0.3413193965 * sc
	let b = -0.0041960863 * lc - 0.7034186147 * mc + 1.7076147010 * sc

	func gamma(_ x: Double) -> Double {
		let v = x <= 0.0031308 ? 12.92 * x : 1.055 * pow(x, 1 / 2.4) - 0.055
		return max(0, min(1, v))
	}
	return Color(.sRGB, red: gamma(r), green: gamma(g), blue: gamma(b), opacity: a)
}

// MARK: - Knobs

enum ThemeMode: String, Codable { case dark, light }

struct ThemeKnobs: Codable, Equatable {
	var mode: ThemeMode = .dark
	var name: String = "Electric"
	var bgH: Double = 265
	var bgL: Double = 0.175
	var bgC: Double = 0.018
	var accentH: Double = 255
	var accentL: Double = 0.70
	var accentC: Double = 0.19
	var catC: Double = 0.16
	var catL: Double? = nil
	var hueShift: Double = 0

	init(mode: ThemeMode = .dark, name: String = "Electric", bgH: Double = 265,
	     bgL: Double = 0.175, bgC: Double = 0.018, accentH: Double = 255,
	     accentL: Double = 0.70, accentC: Double = 0.19, catC: Double = 0.16,
	     catL: Double? = nil, hueShift: Double = 0) {
		self.mode = mode; self.name = name; self.bgH = bgH; self.bgL = bgL; self.bgC = bgC
		self.accentH = accentH; self.accentL = accentL; self.accentC = accentC
		self.catC = catC; self.catL = catL; self.hueShift = hueShift
	}

	// Tolerant decode: the web app writes the same knobs WITHOUT `name`, and may
	// omit fields. Missing keys fall back to defaults so cross-app sync is robust.
	init(from decoder: Decoder) throws {
		let c = try decoder.container(keyedBy: CodingKeys.self)
		let d = ThemeKnobs()
		mode = (try? c.decodeIfPresent(ThemeMode.self, forKey: .mode)) ?? d.mode
		name = (try? c.decodeIfPresent(String.self, forKey: .name)) ?? d.name
		bgH = try c.decodeIfPresent(Double.self, forKey: .bgH) ?? d.bgH
		bgL = try c.decodeIfPresent(Double.self, forKey: .bgL) ?? d.bgL
		bgC = try c.decodeIfPresent(Double.self, forKey: .bgC) ?? d.bgC
		accentH = try c.decodeIfPresent(Double.self, forKey: .accentH) ?? d.accentH
		accentL = try c.decodeIfPresent(Double.self, forKey: .accentL) ?? d.accentL
		accentC = try c.decodeIfPresent(Double.self, forKey: .accentC) ?? d.accentC
		catC = try c.decodeIfPresent(Double.self, forKey: .catC) ?? d.catC
		catL = try c.decodeIfPresent(Double.self, forKey: .catL)
		hueShift = try c.decodeIfPresent(Double.self, forKey: .hueShift) ?? d.hueShift
	}
}

// MARK: - Categorical hues (match backend ValidKinds / ValidEntityTypes order)

let KIND_HUES: [String: Double] = [
	"decision": 85, "preference": 330, "insight": 195, "procedure": 255,
	"fact": 150, "identity": 300, "rule": 30, "idea": 110,
]
let ENTITY_HUES: [String: Double] = [
	"person": 25, "project": 255, "tool": 150, "organization": 300,
	"place": 85, "preference": 330, "concept": 195,
]
let KINDS = ["decision", "preference", "insight", "procedure", "fact", "identity", "rule", "idea"]
let ENTITY_TYPES = ["person", "project", "tool", "organization", "place", "preference", "concept"]
let ENTITY_SIGIL: [String: String] = ["person": "@", "project": "#"]

// MARK: - Curated presets

extension ThemeKnobs {
	static let presets: [ThemeKnobs] = [
		ThemeKnobs(mode: .dark, name: "Electric",   bgH: 265, bgL: 0.175, bgC: 0.018, accentH: 255, accentL: 0.70, accentC: 0.19,  catC: 0.16),
		ThemeKnobs(mode: .dark, name: "Deep Water", bgH: 220, bgL: 0.175, bgC: 0.024, accentH: 196, accentL: 0.74, accentC: 0.135, catC: 0.155),
		ThemeKnobs(mode: .dark, name: "Aurora",     bgH: 165, bgL: 0.175, bgC: 0.020, accentH: 158, accentL: 0.78, accentC: 0.16,  catC: 0.17,  hueShift: 24),
		ThemeKnobs(mode: .dark, name: "Ember",      bgH: 45,  bgL: 0.175, bgC: 0.020, accentH: 48,  accentL: 0.72, accentC: 0.165, catC: 0.165, hueShift: -18),
		ThemeKnobs(mode: .dark, name: "Cairn",      bgH: 0,   bgL: 0.145, bgC: 0.0,   accentH: 304, accentL: 0.65, accentC: 0.22,  catC: 0.20, catL: 0.62),
		ThemeKnobs(mode: .dark, name: "Violet",     bgH: 292, bgL: 0.175, bgC: 0.022, accentH: 300, accentL: 0.66, accentC: 0.205, catC: 0.165),
		ThemeKnobs(mode: .dark, name: "Mono",       bgH: 255, bgL: 0.175, bgC: 0.010, accentH: 255, accentL: 0.74, accentC: 0.045, catC: 0.052),
		ThemeKnobs(mode: .light, name: "Daylight",  bgH: 255, bgL: 0.965, bgC: 0.012, accentH: 255, accentL: 0.55, accentC: 0.18,  catC: 0.16),
	]
}

// MARK: - Resolved tokens

/// The full set of derived colors for the current knobs. Views read these.
struct Tokens {
	let bg, surface, surface2, surface3, border, borderStrong: Color
	let text, textDim, textFaint: Color
	let accent, accentHi, accentFg, accentBg, accentLine: Color
	let destructive, destructiveBg, warning, warningBg, success, muted: Color
	let isLight: Bool
	private let kindHues: [String: Double]
	private let entityHues: [String: Double]
	private let catL: Double
	private let catC: Double

	init(_ p: ThemeKnobs) {
		let light = p.mode == .light
		isLight = light
		let bgH = p.bgH, bgC = p.bgC
		if light {
			bg            = oklch(p.bgL, bgC * 0.6, bgH)
			surface       = oklch(min(1, p.bgL + 0.03), bgC * 0.5, bgH)
			surface2      = oklch(min(1, p.bgL + 0.05), bgC * 0.5, bgH)
			surface3      = oklch(p.bgL - 0.04, bgC * 0.7, bgH)
			border        = oklch(p.bgL - 0.10, bgC * 0.8, bgH)
			borderStrong  = oklch(p.bgL - 0.22, bgC, bgH)
			text          = oklch(0.26, 0.02, bgH)
			textDim       = oklch(0.44, 0.018, bgH)
			textFaint     = oklch(0.58, 0.016, bgH)
		} else {
			bg            = oklch(p.bgL, bgC, bgH)
			surface       = oklch(p.bgL + 0.035, bgC, bgH)
			surface2      = oklch(p.bgL + 0.065, bgC, bgH)
			surface3      = oklch(p.bgL + 0.105, bgC * 1.1, bgH)
			border        = oklch(p.bgL + 0.095, bgC * 0.9, bgH)
			borderStrong  = oklch(p.bgL + 0.185, bgC, bgH)
			text          = oklch(0.965, 0.007, bgH)
			textDim       = oklch(0.745, 0.012, bgH)
			textFaint     = oklch(0.555, 0.016, bgH)
		}
		accent     = oklch(p.accentL, p.accentC, p.accentH)
		accentHi   = oklch(min(0.92, p.accentL + 0.12), p.accentC * 0.9, p.accentH)
		accentFg   = oklch(0.985, 0.012, p.accentH)
		accentBg   = oklch(p.accentL, p.accentC, p.accentH, light ? 0.12 : 0.16)
		accentLine = oklch(p.accentL, p.accentC, p.accentH, 0.35)
		destructive   = oklch(light ? 0.55 : 0.66, 0.20, 32)
		destructiveBg = oklch(0.66, 0.20, 32, 0.16)
		warning       = oklch(light ? 0.62 : 0.80, 0.155, 80)
		warningBg     = oklch(0.80, 0.155, 80, 0.16)
		success       = oklch(light ? 0.55 : 0.74, 0.15, 150)
		muted         = oklch(light ? 0.62 : 0.60, 0.012, bgH)

		catL = p.catL ?? (light ? 0.55 : 0.745)
		catC = p.catC
		kindHues = KIND_HUES.mapValues { $0 + p.hueShift }
		entityHues = ENTITY_HUES.mapValues { $0 + p.hueShift }
	}

	func kind(_ id: String) -> Color { oklch(catL, catC, kindHues[id] ?? 0) }
	func kindBg(_ id: String) -> Color { oklch(catL, catC, kindHues[id] ?? 0, isLight ? 0.13 : 0.16) }
	func kindSoft(_ id: String) -> Color { oklch(catL, catC, kindHues[id] ?? 0, 0.30) }
	func entity(_ id: String) -> Color { oklch(catL, catC, entityHues[id] ?? 0) }
	func entityBg(_ id: String) -> Color { oklch(catL, catC, entityHues[id] ?? 0, isLight ? 0.13 : 0.16) }
	func entityLine(_ id: String) -> Color { oklch(catL, catC, entityHues[id] ?? 0, 0.40) }
}

// MARK: - Manager

@MainActor
final class ThemeManager: ObservableObject {
	@Published var knobs: ThemeKnobs { didSet { tokens = Tokens(knobs) } }
	@Published private(set) var tokens: Tokens

	init(_ knobs: ThemeKnobs = .presets[0]) {
		self.knobs = knobs
		self.tokens = Tokens(knobs)
	}

	var activePresetName: String? {
		ThemeKnobs.presets.first { p in
			p.bgH == knobs.bgH && p.accentH == knobs.accentH && p.mode == knobs.mode &&
			abs(p.catC - knobs.catC) < 0.001 && p.hueShift == knobs.hueShift
		}?.name
	}

	func apply(preset: ThemeKnobs) { knobs = preset }
}
