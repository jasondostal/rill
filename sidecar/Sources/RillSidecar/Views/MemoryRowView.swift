// MemoryRowView.swift — one memory in the list: kind dot · summary · meta, with
// relative time that swaps to pin/forget on hover. Clicking the row opens the
// resizable detail window (full details + entity chips + inline edit).

import SwiftUI

struct MemoryRowView: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	let memory: Memory
	@State private var hovering = false

	var body: some View {
		let t = theme.tokens
		let fresh = state.freshID == memory.id
		HStack(alignment: .top, spacing: 9) {
			KindDot(kind: memory.kind).padding(.top, 4)
			VStack(alignment: .leading, spacing: 4) {
				Text(memory.summary)
					.font(.system(size: 13)).foregroundColor(t.text)
					.lineLimit(2).fixedSize(horizontal: false, vertical: true)
				HStack(spacing: 5) {
					if memory.pinned { Image(systemName: "star.fill").font(.system(size: 8)).foregroundColor(t.accent) }
					Mono(metaLine, size: 10.5, color: t.textFaint)
				}
			}
			Spacer(minLength: 4)
			Group {
				if hovering {
					HStack(spacing: 4) {
						IconButton(system: memory.pinned ? "star.fill" : "star") { Task { await state.togglePin(memory) } }
						IconButton(system: "trash") { state.forget(memory) }
					}
				} else {
					Mono(relativeTime(memory.createdAt), size: 10.5, color: t.textFaint)
				}
			}
			.frame(minWidth: 40, alignment: .trailing)
		}
		.padding(.horizontal, 11).padding(.vertical, 7)
		.background(RoundedRectangle(cornerRadius: 7).fill(fresh ? t.kindBg(memory.kind) : (hovering ? t.surface2 : Color.clear)))
		.overlay(RoundedRectangle(cornerRadius: 7).stroke(hovering ? t.borderStrong : Color.clear, lineWidth: 1))
		.contentShape(Rectangle())
		.onTapGesture { DetailWindowManager.shared.open(memory) }
		.animation(.easeOut(duration: 0.12), value: hovering)
		.animation(.easeOut(duration: 0.4), value: fresh)
		.onHover { hovering = $0 }
	}

	private var metaLine: String {
		var parts = [memory.kind]
		if !memory.author.isEmpty { parts.append(memory.author) }
		if let p = memory.project, !p.isEmpty { parts.append(p) }
		return parts.joined(separator: " · ")
	}
}
