// Icon.swift — render the real Lucide "droplets" mark from its SVG path data.
// No raster assets, no SVG tooling on the box: we parse the path strings once
// and reuse them for both the menu-bar template image and the in-app logo, so
// the two are pixel-identical (the brief calls this mark out specifically).

import SwiftUI
import AppKit

// The two stroked sub-paths of Lucide "droplets" (viewBox 0 0 24 24), lifted
// verbatim from the design bundle (rill-kit.jsx).
enum Lucide {
	static let dropletsPaths: [String] = [
		"M7 16.3c2.2 0 4-1.83 4-4.05 0-1.16-.57-2.26-1.71-3.19S7.29 6.75 7 5.3c-.29 1.45-1.14 2.84-2.29 3.76S3 11.1 3 12.25c0 2.22 1.8 4.05 4 4.05z",
		"M12.56 6.6A10.97 10.97 0 0 0 14 3.02c.5 2.5 2 4.9 4 6.5s3 3.5 3 5.5a6.98 6.98 0 0 1-11.91 4.97"
	]
	static let viewBox: CGFloat = 24
}

// MARK: - SwiftUI shape

/// The droplets mark as a strokeable SwiftUI shape, scaled to fit `rect`.
struct DropletsShape: Shape {
	func path(in rect: CGRect) -> Path {
		let s = min(rect.width, rect.height) / Lucide.viewBox
		let tx = rect.minX + (rect.width - Lucide.viewBox * s) / 2
		let ty = rect.minY + (rect.height - Lucide.viewBox * s) / 2
		var t = CGAffineTransform(translationX: tx, y: ty).scaledBy(x: s, y: s)
		let combined = CGMutablePath()
		for d in Lucide.dropletsPaths {
			if let p = SVGPath.parse(d) { combined.addPath(p, transform: t) }
		}
		return Path(combined)
	}
}

// MARK: - Menu-bar template image

enum DropletIcon {
	/// A monochrome template NSImage for the status bar. `isTemplate` lets macOS
	/// tint it for light/dark menu bars automatically.
	static func menuBarImage(point: CGFloat = 18, lineWidth: CGFloat = 1.7) -> NSImage {
		let size = NSSize(width: point, height: point)
		let img = NSImage(size: size)
		img.lockFocus()
		defer { img.unlockFocus() }
		guard let ctx = NSGraphicsContext.current?.cgContext else { return img }

		// Fit 24x24 viewBox into the image with a little inset so strokes don't clip.
		let inset = lineWidth + 0.5
		let avail = point - inset * 2
		let s = avail / Lucide.viewBox
		// AppKit is y-up; SVG is y-down. Flip y then scale.
		ctx.translateBy(x: inset, y: point - inset)
		ctx.scaleBy(x: s, y: -s)

		ctx.setStrokeColor(NSColor.black.cgColor) // template: color is ignored
		ctx.setLineWidth(lineWidth / s)
		ctx.setLineCap(.round)
		ctx.setLineJoin(.round)
		for d in Lucide.dropletsPaths {
			if let p = SVGPath.parse(d) {
				ctx.addPath(p)
				ctx.strokePath()
			}
		}
		img.isTemplate = true
		return img
	}
}

// MARK: - Minimal SVG path → CGPath parser

/// Parses the subset of SVG path syntax used by Lucide icons:
/// M/m L/l H/h V/v C/c S/s Q/q T/t A/a Z/z (absolute + relative).
enum SVGPath {
	static func parse(_ d: String) -> CGPath? {
		let path = CGMutablePath()
		var tokens = Tokenizer(d)
		var cur = CGPoint.zero
		var start = CGPoint.zero
		var lastCubicCtrl: CGPoint? = nil
		var lastQuadCtrl: CGPoint? = nil
		var cmd: Character = " "

		func num() -> CGFloat? { tokens.number() }
		func pt(rel: Bool) -> CGPoint? {
			guard let x = num(), let y = num() else { return nil }
			return rel ? CGPoint(x: cur.x + x, y: cur.y + y) : CGPoint(x: x, y: y)
		}

		while true {
			// Read an explicit command, or repeat the previous one ONLY if more
			// coordinates follow (SVG implicit-repeat). 'z'/'Z' consume nothing, so
			// they must never repeat — otherwise end-of-input spins forever.
			let c: Character
			if let next = tokens.command() {
				c = next
			} else if cmd != " ", cmd.lowercased() != "z", tokens.peekIsNumber() {
				c = cmd
			} else {
				break
			}
			cmd = c
			let rel = c.isLowercase
			switch Character(c.lowercased()) {
			case "m":
				guard let p = pt(rel: rel) else { return path }
				cur = p; start = p
				path.move(to: cur)
				lastCubicCtrl = nil; lastQuadCtrl = nil
				cmd = rel ? "l" : "L" // subsequent implicit pairs are lineto
			case "l":
				guard let p = pt(rel: rel) else { return path }
				cur = p; path.addLine(to: cur)
				lastCubicCtrl = nil; lastQuadCtrl = nil
			case "h":
				guard let x = num() else { return path }
				cur = CGPoint(x: rel ? cur.x + x : x, y: cur.y); path.addLine(to: cur)
				lastCubicCtrl = nil; lastQuadCtrl = nil
			case "v":
				guard let y = num() else { return path }
				cur = CGPoint(x: cur.x, y: rel ? cur.y + y : y); path.addLine(to: cur)
				lastCubicCtrl = nil; lastQuadCtrl = nil
			case "c":
				guard let c1 = pt(rel: rel), let c2 = pt(rel: rel), let p = pt(rel: rel) else { return path }
				path.addCurve(to: p, control1: c1, control2: c2)
				lastCubicCtrl = c2; cur = p; lastQuadCtrl = nil
			case "s":
				let c1 = lastCubicCtrl.map { CGPoint(x: 2 * cur.x - $0.x, y: 2 * cur.y - $0.y) } ?? cur
				guard let c2 = pt(rel: rel), let p = pt(rel: rel) else { return path }
				path.addCurve(to: p, control1: c1, control2: c2)
				lastCubicCtrl = c2; cur = p; lastQuadCtrl = nil
			case "q":
				guard let c1 = pt(rel: rel), let p = pt(rel: rel) else { return path }
				path.addQuadCurve(to: p, control: c1)
				lastQuadCtrl = c1; cur = p; lastCubicCtrl = nil
			case "t":
				let c1 = lastQuadCtrl.map { CGPoint(x: 2 * cur.x - $0.x, y: 2 * cur.y - $0.y) } ?? cur
				guard let p = pt(rel: rel) else { return path }
				path.addQuadCurve(to: p, control: c1)
				lastQuadCtrl = c1; cur = p; lastCubicCtrl = nil
			case "a":
				guard let rx = num(), let ry = num(), let rot = num(),
				      let large = tokens.flag(), let sweep = tokens.flag(),
				      let p = pt(rel: rel) else { return path }
				addArc(to: path, from: cur, to: p, rx: rx, ry: ry,
				       xRotDeg: rot, largeArc: large, sweep: sweep)
				cur = p; lastCubicCtrl = nil; lastQuadCtrl = nil
			case "z":
				path.closeSubpath(); cur = start
				lastCubicCtrl = nil; lastQuadCtrl = nil
			default:
				return path
			}
		}
		return path
	}

	// Endpoint-parameterization elliptical arc → cubic bezier segments.
	private static func addArc(to path: CGMutablePath, from p0: CGPoint, to p1: CGPoint,
	                           rx rxIn: CGFloat, ry ryIn: CGFloat, xRotDeg: CGFloat,
	                           largeArc: Bool, sweep: Bool) {
		var rx = abs(rxIn), ry = abs(ryIn)
		if rx == 0 || ry == 0 { path.addLine(to: p1); return }
		let phi = xRotDeg * .pi / 180
		let cosP = cos(phi), sinP = sin(phi)
		let dx = (p0.x - p1.x) / 2, dy = (p0.y - p1.y) / 2
		let x1p = cosP * dx + sinP * dy
		let y1p = -sinP * dx + cosP * dy
		// Correct out-of-range radii.
		let lambda = (x1p * x1p) / (rx * rx) + (y1p * y1p) / (ry * ry)
		if lambda > 1 { let s = sqrt(lambda); rx *= s; ry *= s }
		var denom = rx * rx * y1p * y1p + ry * ry * x1p * x1p
		var num = rx * rx * ry * ry - rx * rx * y1p * y1p - ry * ry * x1p * x1p
		if num < 0 { num = 0 }
		var coef = sqrt(num / max(denom, .leastNonzeroMagnitude))
		if largeArc == sweep { coef = -coef }
		let cxp = coef * rx * y1p / ry
		let cyp = -coef * ry * x1p / rx
		let cx = cosP * cxp - sinP * cyp + (p0.x + p1.x) / 2
		let cy = sinP * cxp + cosP * cyp + (p0.y + p1.y) / 2

		func angle(_ ux: CGFloat, _ uy: CGFloat, _ vx: CGFloat, _ vy: CGFloat) -> CGFloat {
			let dot = ux * vx + uy * vy
			let len = sqrt((ux * ux + uy * uy) * (vx * vx + vy * vy))
			var a = acos(max(-1, min(1, dot / len)))
			if ux * vy - uy * vx < 0 { a = -a }
			return a
		}
		let theta1 = angle(1, 0, (x1p - cxp) / rx, (y1p - cyp) / ry)
		var dTheta = angle((x1p - cxp) / rx, (y1p - cyp) / ry,
		                   (-x1p - cxp) / rx, (-y1p - cyp) / ry)
		if !sweep && dTheta > 0 { dTheta -= 2 * .pi }
		if sweep && dTheta < 0 { dTheta += 2 * .pi }
		_ = denom; _ = num // silence unused in some compilers

		let segments = max(1, Int(ceil(abs(dTheta) / (.pi / 2))))
		let delta = dTheta / CGFloat(segments)
		let t = (4.0 / 3.0) * tan(delta / 4)
		var theta = theta1
		for _ in 0..<segments {
			let cosT1 = cos(theta), sinT1 = sin(theta)
			let theta2 = theta + delta
			let cosT2 = cos(theta2), sinT2 = sin(theta2)
			// Points/derivatives in the unrotated unit circle, then map to ellipse.
			func map(_ ex: CGFloat, _ ey: CGFloat) -> CGPoint {
				let x = rx * ex, y = ry * ey
				return CGPoint(x: cosP * x - sinP * y + cx, y: sinP * x + cosP * y + cy)
			}
			let p2 = map(cosT2, sinT2)
			let c1 = map(cosT1 - t * sinT1, sinT1 + t * cosT1)
			let c2 = map(cosT2 + t * sinT2, sinT2 - t * cosT2)
			path.addCurve(to: p2, control1: c1, control2: c2)
			theta = theta2
		}
	}
}

// Streaming tokenizer for SVG path data.
private struct Tokenizer {
	private let chars: [Character]
	private var i = 0
	init(_ s: String) { chars = Array(s) }

	private mutating func skipSep() {
		while i < chars.count, chars[i] == " " || chars[i] == "," || chars[i] == "\n" || chars[i] == "\t" || chars[i] == "\r" {
			i += 1
		}
	}

	mutating func command() -> Character? {
		skipSep()
		guard i < chars.count else { return nil }
		let c = chars[i]
		if c.isLetter {
			i += 1
			return c
		}
		return nil
	}

	/// True if the next non-separator token starts a number (digit, sign, or dot).
	mutating func peekIsNumber() -> Bool {
		skipSep()
		guard i < chars.count else { return false }
		let c = chars[i]
		return c.isNumber || c == "+" || c == "-" || c == "."
	}

	mutating func number() -> CGFloat? {
		skipSep()
		guard i < chars.count else { return nil }
		var s = ""
		if chars[i] == "+" || chars[i] == "-" { s.append(chars[i]); i += 1 }
		var seenDot = false, seenExp = false
		while i < chars.count {
			let c = chars[i]
			if c.isNumber { s.append(c); i += 1 }
			else if c == "." && !seenDot && !seenExp { seenDot = true; s.append(c); i += 1 }
			else if (c == "e" || c == "E") && !seenExp { seenExp = true; s.append(c); i += 1
				if i < chars.count, chars[i] == "+" || chars[i] == "-" { s.append(chars[i]); i += 1 } }
			else { break }
		}
		return Double(s).map { CGFloat($0) }
	}

	// Arc flags are single 0/1 digits and may be packed without separators.
	mutating func flag() -> Bool? {
		skipSep()
		guard i < chars.count else { return nil }
		let c = chars[i]
		if c == "0" { i += 1; return false }
		if c == "1" { i += 1; return true }
		return nil
	}
}
