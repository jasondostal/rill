// Components.swift — shared primitives: logo, kind dot/chip, entity chip, mono text.

import SwiftUI

// Monospace label (JetBrains-Mono-in-spirit; system mono keeps the binary tiny).
struct Mono: View {
	let text: String
	var size: CGFloat = 10.5
	var weight: Font.Weight = .medium
	var color: Color = .primary
	init(_ text: String, size: CGFloat = 10.5, weight: Font.Weight = .medium, color: Color = .primary) {
		self.text = text; self.size = size; self.weight = weight; self.color = color
	}
	var body: some View {
		Text(text)
			.font(.system(size: size, weight: weight, design: .monospaced))
			.foregroundColor(color)
	}
}

// The Lucide droplets mark + "rill" wordmark, used in the header.
struct Logo: View {
	@EnvironmentObject var theme: ThemeManager
	var body: some View {
		HStack(spacing: 6) {
			DropletsShape()
				.stroke(theme.tokens.accent, style: StrokeStyle(lineWidth: 1.9, lineCap: .round, lineJoin: .round))
				.frame(width: 16, height: 16)
			Text("rill")
				.font(.system(size: 14, weight: .bold))
				.foregroundColor(theme.tokens.text)
		}
	}
}

struct KindDot: View {
	@EnvironmentObject var theme: ThemeManager
	let kind: String
	var size: CGFloat = 7
	var body: some View {
		Circle()
			.fill(theme.tokens.kind(kind))
			.frame(width: size, height: size)
			.overlay(Circle().stroke(theme.tokens.kindSoft(kind), lineWidth: 2).blur(radius: 0.5))
	}
}

struct KindChip: View {
	@EnvironmentObject var theme: ThemeManager
	let kind: String
	let selected: Bool
	let action: () -> Void
	var body: some View {
		let t = theme.tokens
		Button(action: action) {
			HStack(spacing: 5) {
				Circle().fill(t.kind(kind)).frame(width: 6, height: 6)
				Text(kind).font(.system(size: 11.5, weight: .medium, design: .monospaced))
			}
			.padding(.horizontal, 9).padding(.vertical, 4)
			.foregroundColor(selected ? t.kind(kind) : t.textDim)
			.background(RoundedRectangle(cornerRadius: 999).fill(selected ? t.kindBg(kind) : t.surface2))
			.overlay(RoundedRectangle(cornerRadius: 999).stroke(selected ? t.kind(kind) : t.border, lineWidth: 1))
		}
		.buttonStyle(.plain)
	}
}

struct EntityChip: View {
	@EnvironmentObject var theme: ThemeManager
	let entity: EntityLite
	var onRemove: (() -> Void)? = nil
	var body: some View {
		let t = theme.tokens
		let sigil = ENTITY_SIGIL[entity.type] ?? ""
		HStack(spacing: 3) {
			Circle().fill(t.entity(entity.type)).frame(width: 5, height: 5)
			Text("\(sigil)\(entity.name)")
				.font(.system(size: 10.5, weight: .medium, design: .monospaced))
			if let onRemove {
				Button(action: onRemove) { Image(systemName: "xmark").font(.system(size: 7, weight: .bold)) }
					.buttonStyle(.plain)
			}
		}
		.padding(.horizontal, 6).padding(.vertical, 2.5)
		.foregroundColor(t.entity(entity.type))
		.background(RoundedRectangle(cornerRadius: 999).fill(t.entityBg(entity.type)))
		.overlay(RoundedRectangle(cornerRadius: 999).stroke(t.entityLine(entity.type), lineWidth: 1))
	}
}

// A statement edge: subject --verb--> object, as colored entity chips.
struct TripleRow: View {
	@EnvironmentObject var theme: ThemeManager
	let edge: EdgeDecl
	var onRemove: (() -> Void)? = nil
	var body: some View {
		let t = theme.tokens
		HStack(spacing: 5) {
			endChip(edge.subject, edge.subjectType)
			Image(systemName: "arrow.right").font(.system(size: 8, weight: .bold)).foregroundColor(t.textFaint)
			Text(edge.predicate.replacingOccurrences(of: "_", with: " "))
				.font(.system(size: 10, weight: .medium, design: .monospaced)).italic()
				.foregroundColor(t.accent)
			Image(systemName: "arrow.right").font(.system(size: 8, weight: .bold)).foregroundColor(t.textFaint)
			endChip(edge.object, edge.objectType)
			if let onRemove {
				Spacer(minLength: 2)
				Button(action: onRemove) { Image(systemName: "xmark").font(.system(size: 7, weight: .bold)).foregroundColor(t.textFaint) }
					.buttonStyle(.plain)
			}
		}
		.padding(.horizontal, 7).padding(.vertical, 4)
		.background(RoundedRectangle(cornerRadius: 6).fill(t.accentBg))
		.overlay(RoundedRectangle(cornerRadius: 6).stroke(t.accentLine, lineWidth: 1))
	}
	private func endChip(_ name: String, _ type: String) -> some View {
		let t = theme.tokens
		return HStack(spacing: 3) {
			Circle().fill(t.entity(type)).frame(width: 5, height: 5)
			Text("\(ENTITY_SIGIL[type] ?? "")\(name)")
				.font(.system(size: 10, weight: .medium, design: .monospaced))
				.foregroundColor(t.entity(type)).lineLimit(1)
		}
	}
}

// A small header icon button (search, settings, back).
struct IconButton: View {
	@EnvironmentObject var theme: ThemeManager
	let system: String
	var active: Bool = false
	let action: () -> Void
	var body: some View {
		let t = theme.tokens
		Button(action: action) {
			Image(systemName: system)
				.font(.system(size: 12, weight: .medium))
				.frame(width: 22, height: 22)
				.foregroundColor(active ? t.accent : t.textDim)
				.background(RoundedRectangle(cornerRadius: 6).fill(active ? t.accentBg : .clear))
		}
		.buttonStyle(.plain)
	}
}
