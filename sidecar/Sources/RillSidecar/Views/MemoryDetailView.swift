// MemoryDetailView.swift — a roomy, resizable detail window for one memory.
// Opened from the popover ("open" on an expanded row). The popover is for quick
// capture/browse; this is where you actually read and edit a long memory.

import SwiftUI
import AppKit

struct MemoryDetailView: View {
	@EnvironmentObject var theme: ThemeManager
	@EnvironmentObject var state: AppState
	let memoryID: String
	let fallback: Memory

	@State private var editing = false
	@State private var editText = ""

	private var memory: Memory { state.memory(byID: memoryID) ?? fallback }

	var body: some View {
		let t = theme.tokens
		VStack(alignment: .leading, spacing: 0) {
			header
			Divider().overlay(t.border)
			ScrollView {
				VStack(alignment: .leading, spacing: 14) {
					if editing { editor } else { readBody }
				}
				.padding(16)
			}
		}
		.frame(minWidth: 380, minHeight: 320)
		.background(t.bg)
		.environment(\.colorScheme, t.isLight ? .light : .dark)
		.onAppear { state.loadDetailIfNeeded(memoryID) }
	}

	private var header: some View {
		let t = theme.tokens
		let m = memory
		return HStack(spacing: 9) {
			KindDot(kind: m.kind, size: 9)
			VStack(alignment: .leading, spacing: 2) {
				Mono(metaLine(m), size: 11, color: t.textDim)
				if let c = m.createdAt { Mono(String(c.prefix(19)).replacingOccurrences(of: "T", with: "  "), size: 9.5, color: t.textFaint) }
			}
			Spacer()
			IconButton(system: m.pinned ? "star.fill" : "star") { Task { await state.togglePin(m) } }
			IconButton(system: editing ? "xmark" : "pencil") { toggleEdit(m) }
			IconButton(system: "trash") { state.forget(m); closeWindow() }
		}
		.padding(.horizontal, 16).padding(.vertical, 11)
		.background(t.surface.opacity(0.4))
	}

	private var readBody: some View {
		let t = theme.tokens
		let m = memory
		return VStack(alignment: .leading, spacing: 14) {
			Text(m.summary).font(.system(size: 16, weight: .medium)).foregroundColor(t.text)
				.textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
			if let d = m.details, !d.isEmpty, d != m.summary {
				Text(d).font(.system(size: 13.5)).foregroundColor(t.textDim)
					.textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
					.lineSpacing(2)
			}
			if let ents = state.detailEntities[m.id], !ents.isEmpty {
				VStack(alignment: .leading, spacing: 6) {
					Mono("ENTITIES", size: 9, weight: .semibold, color: t.textFaint)
					FlowWrap(ents) { EntityChip(entity: $0) }
				}
			}
		}
	}

	private var editor: some View {
		let t = theme.tokens
		return VStack(alignment: .leading, spacing: 8) {
			Mono("EDIT", size: 9, weight: .semibold, color: t.textFaint)
			TextEditor(text: $editText)
				.font(.system(size: 13.5)).foregroundColor(t.text)
				.scrollContentBackground(.hidden)
				.frame(minHeight: 160)
				.padding(8)
				.background(RoundedRectangle(cornerRadius: 7).fill(t.surface))
				.overlay(RoundedRectangle(cornerRadius: 7).stroke(t.border, lineWidth: 1))
			HStack {
				Spacer()
				Button("cancel") { editing = false }.buttonStyle(.plain)
					.font(.system(size: 12)).foregroundColor(t.textDim)
				Button {
					let full = editText.trimmingCharacters(in: .whitespacesAndNewlines)
					let summary = full.count > 600 ? String(full.prefix(597)) + "…" : full
					Task { await state.saveEdit(memoryID, summary: summary, details: full); editing = false }
				} label: {
					Text("save").font(.system(size: 12, weight: .semibold)).foregroundColor(t.accentFg)
						.padding(.horizontal, 12).padding(.vertical, 5)
						.background(RoundedRectangle(cornerRadius: 6).fill(t.accent))
				}.buttonStyle(.plain)
			}
		}
	}

	private func toggleEdit(_ m: Memory) {
		if !editing { editText = m.details ?? m.summary }
		editing.toggle()
	}
	private func metaLine(_ m: Memory) -> String {
		var p = [m.kind]; if !m.author.isEmpty { p.append(m.author) }
		if let pr = m.project, !pr.isEmpty { p.append(pr) }
		return p.joined(separator: "  ·  ")
	}
	private func closeWindow() { DetailWindowManager.shared.close() }
}

// A simple wrapping (multi-line) flow layout for chips.
struct FlowWrap<Item: Identifiable, Content: View>: View {
	let items: [Item]
	let content: (Item) -> Content
	init(_ items: [Item], @ViewBuilder content: @escaping (Item) -> Content) {
		self.items = items; self.content = content
	}
	var body: some View {
		// Coarse wrap: chunk into rows of 3. Good enough for entity chips in a
		// resizable window without a custom Layout.
		let rows = stride(from: 0, to: items.count, by: 3).map { Array(items[$0..<min($0 + 3, items.count)]) }
		VStack(alignment: .leading, spacing: 5) {
			ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
				HStack(spacing: 5) { ForEach(row) { content($0) }; Spacer(minLength: 0) }
			}
		}
	}
}

// MARK: - Window manager

@MainActor
final class DetailWindowManager {
	static let shared = DetailWindowManager()
	private var window: NSWindow?
	weak var state: AppState?
	weak var theme: ThemeManager?

	func configure(state: AppState, theme: ThemeManager) {
		self.state = state; self.theme = theme
	}

	func open(_ memory: Memory) {
		guard let state, let theme else { return }
		let root = MemoryDetailView(memoryID: memory.id, fallback: memory)
			.environmentObject(state)
			.environmentObject(theme)
		let hosting = NSHostingController(rootView: root)
		if let w = window {
			w.contentViewController = hosting
		} else {
			let w = NSWindow(
				contentRect: NSRect(x: 0, y: 0, width: 480, height: 560),
				styleMask: [.titled, .closable, .resizable, .miniaturizable],
				backing: .buffered, defer: false)
			w.title = "Memory"
			w.titlebarAppearsTransparent = true
			w.isReleasedWhenClosed = false
			w.contentViewController = hosting
			w.center()
			window = w
		}
		window?.makeKeyAndOrderFront(nil)
		NSApp.activate(ignoringOtherApps: true)
	}

	func close() { window?.close() }
}
